package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/gate"
	"lossless/internal/harness"
	"lossless/internal/store"
	"lossless/internal/write"
)

func sessionClaims(t *testing.T, st *store.Store, project, sid string) []claim.Record {
	t.Helper()
	recs, err := st.ListActive(project)
	if err != nil {
		t.Fatal(err)
	}
	var out []claim.Record
	for _, r := range recs {
		if r.SessionID == sid {
			out = append(out, r)
		}
	}
	return out
}

const catchUpLine = `{"type":"assistant","content":"Use catchup, not sidecar, in internal/write/catchup.go."}` + "\n"

func TestAskCatchUpOmitsSidUsesStoredWorkspace(t *testing.T) {
	st := tmpStore(t)
	ws := filepath.Join(t.TempDir(), "acme-api")
	current := writeJSONL(t, filepath.Join(ws, "s-current"), "tape.jsonl", catchUpLine)
	behindBody := `{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}` + "\n"
	behind := writeJSONL(t, filepath.Join(ws, "s-behind"), "tape.jsonl", behindBody)
	for _, s := range []store.Session{
		{JSONL: current, SessionID: "s-current", Harness: "grok", Workspace: ws, Project: "acme/api"},
		{JSONL: behind, SessionID: "s-behind", Harness: "grok", Workspace: ws + "/", Project: "acme/api"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: current, Project: "acme/api", WorkspaceRoot: ws,
		Harness: "grok", SessionID: "s-current", Source: "turn",
	}); err != nil {
		t.Fatal(err)
	}
	beforeCurrent := len(sessionClaims(t, st, "acme/api", "s-current"))
	if beforeCurrent == 0 {
		t.Fatal("pre-ingest current")
	}
	out := askAt(t, st, Request{
		Project:       "acme/api",
		WorkspaceRoot: ws,
		Question:      "what limiter",
		Goal:          "keep catch-up local",
		Paths:         []string{"src/middleware/auth.ts", "internal/write/catchup.go"},
	})
	if n := len(sessionClaims(t, st, "acme/api", "s-current")); n != beforeCurrent {
		t.Fatalf("re-extracted current: %d -> %d", beforeCurrent, n)
	}
	got := sessionClaims(t, st, "acme/api", "s-behind")
	if len(got) == 0 {
		t.Fatalf("behind session not ingested: %+v", out)
	}
	if len(sessionClaims(t, st, "acme/api", "chat_history")) != 0 {
		t.Fatal("named session chat_history")
	}
	if !strings.Contains(textsOf(out)+got[0].Text, "limiter") && !strings.Contains(textsOf(out), "sidecar") {
		t.Fatalf("pack missed ingested work: %+v claims=%+v", out, got)
	}
}

func TestAskCatchUpLocatesUnknownSid(t *testing.T) {
	st := tmpStore(t)
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	ws := "/ws/acme-api"
	sid := "s-locate-1"
	jsonl := writeJSONL(t, filepath.Join(home, "sessions", harness.EncodeCWD(ws), sid), "chat_history.jsonl", catchUpLine)
	out := askAt(t, st, Request{
		Project:       "acme/api",
		WorkspaceRoot: ws,
		SessionID:     sid,
		Question:      "how does catch-up work",
		Goal:          "keep catch-up local",
		Paths:         []string{"internal/write/catchup.go"},
	})
	if _, err := os.Stat(jsonl); err != nil {
		t.Fatal(err)
	}
	got := sessionClaims(t, st, "acme/api", sid)
	if len(got) == 0 {
		t.Fatalf("did not locate+ingest: %+v", out)
	}
	if len(sessionClaims(t, st, "acme/api", "chat_history")) != 0 {
		t.Fatal("named session chat_history")
	}
	if !strings.Contains(textsOf(out)+got[0].Text, "sidecar") {
		t.Fatalf("pack missed: %+v claims=%+v", out, got)
	}
}

func TestAskCatchUpEOFNoDoubleExtract(t *testing.T) {
	st := tmpStore(t)
	jsonl := writeJSONL(t, t.TempDir(), "tape.jsonl", catchUpLine)
	if err := st.UpsertSession(store.Session{
		JSONL: jsonl, SessionID: "s-eof", Harness: "grok",
		Workspace: filepath.Dir(jsonl), Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	req := Request{
		Project: "acme/api", SessionID: "s-eof",
		Question: "how does catch-up work", Goal: "keep catch-up local",
		Paths: []string{"internal/write/catchup.go"},
	}
	_ = askAt(t, st, req)
	n := len(sessionClaims(t, st, "acme/api", "s-eof"))
	if n == 0 {
		t.Fatal("first ask ingested nothing")
	}
	_ = askAt(t, st, req)
	if got := len(sessionClaims(t, st, "acme/api", "s-eof")); got != n {
		t.Fatalf("double extract %d -> %d", n, got)
	}
}

func TestAskCatchUpWaitsForCompleteLine(t *testing.T) {
	st := tmpStore(t)
	jsonl := writeJSONL(t, t.TempDir(), "tape.jsonl", `{"type":"user","content":"incomplete`)
	if err := st.UpsertSession(store.Session{
		JSONL: jsonl, SessionID: "s-partial", Harness: "grok",
		Workspace: filepath.Dir(jsonl), Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	_ = askAt(t, st, Request{
		Project: "acme/api", SessionID: "s-partial",
		Question: "anything", Goal: "catch-up",
	})
	if st.Cursor(jsonl) != 0 {
		t.Fatalf("cursor advanced on incomplete line: %d", st.Cursor(jsonl))
	}
	if len(sessionClaims(t, st, "acme/api", "s-partial")) != 0 {
		t.Fatal("extracted incomplete line")
	}
}

func TestAskCatchUpUnknownSidWithoutWorkspaceIsNoop(t *testing.T) {
	st := tmpStore(t)
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	_ = askAt(t, st, Request{
		Project:   "acme/api",
		SessionID: "s-no-ws",
		Question:  "how does catch-up work",
		Goal:      "keep catch-up local",
	})
	if len(sessionClaims(t, st, "acme/api", "chat_history")) != 0 {
		t.Fatal("invented chat_history")
	}
	if len(sessionClaims(t, st, "acme/api", "s-no-ws")) != 0 {
		t.Fatal("ingested without workspace")
	}
	sess, err := st.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sess) != 0 {
		t.Fatalf("sessions: %+v", sess)
	}
}

func TestAskCatchUpDoesNotCrossWorktree(t *testing.T) {
	st := tmpStore(t)
	root := t.TempDir()
	wsA := filepath.Join(root, "tree-a")
	wsB := filepath.Join(root, "tree-b")
	a := writeJSONL(t, wsA, "a.jsonl", `{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}`+"\n")
	b := writeJSONL(t, wsB, "b.jsonl", `{"type":"assistant","content":"Stripe invoice webhook failed in src/billing/export.ts."}`+"\n")
	for _, s := range []store.Session{
		{JSONL: a, SessionID: "s-tree-a", Harness: "grok", Workspace: wsA, Project: "acme/api"},
		{JSONL: b, SessionID: "s-tree-b", Harness: "grok", Workspace: wsB, Project: "acme/api"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	_ = askAt(t, st, Request{
		Project:       "acme/api",
		WorkspaceRoot: wsA,
		Question:      "what limiter",
		Goal:          "keep limiter in-process",
		Paths:         []string{"src/middleware/auth.ts"},
	})
	if len(sessionClaims(t, st, "acme/api", "s-tree-a")) == 0 {
		t.Fatal("did not ingest this worktree")
	}
	if len(sessionClaims(t, st, "acme/api", "s-tree-b")) != 0 {
		t.Fatal("ingested the other worktree")
	}
}

func TestAskCatchUpProjectOnlyTouchesThatProject(t *testing.T) {
	st := tmpStore(t)
	dir := t.TempDir()
	api := writeJSONL(t, dir, "api.jsonl", `{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}`+"\n")
	shop := writeJSONL(t, dir, "shop.jsonl", `{"type":"assistant","content":"Stripe invoice webhook failed in src/billing/export.ts."}`+"\n")
	for _, s := range []store.Session{
		{JSONL: api, SessionID: "s-api", Harness: "grok", Workspace: "", Project: "acme/api"},
		{JSONL: shop, SessionID: "s-shop", Harness: "grok", Workspace: "", Project: "other/shop"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	_ = askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what limiter",
		Goal:     "keep limiter in-process",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if len(sessionClaims(t, st, "acme/api", "s-api")) == 0 {
		t.Fatal("project-only ask missed acme")
	}
	if len(sessionClaims(t, st, "other/shop", "s-shop")) != 0 {
		t.Fatal("project-only ask ingested other/shop")
	}
}

func TestLocateExactSkipsCodexNewestCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	root := filepath.Join(home, "sessions", "2026", "08", "18")
	wantSID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	decoySID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	meta := func(sid string) string {
		return `{"type":"session_meta","payload":{"id":"` + sid + `","cwd":"/ws/acme-api"}}` + "\n"
	}
	wantBody := meta(wantSID) + `{"type":"assistant","content":"Use catchup, not sidecar, in internal/write/catchup.go."}` + "\n"
	decoyBody := meta(decoySID) + `{"type":"assistant","content":"Stripe invoice webhook failed in src/billing/export.ts."}` + "\n"
	want := writeJSONL(t, root, "rollout-2026-08-18T00-00-00-"+wantSID+".jsonl", wantBody)
	decoy := writeJSONL(t, root, "rollout-2026-08-18T12-00-00-"+decoySID+".jsonl", decoyBody)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(decoy, later, later); err != nil {
		t.Fatal(err)
	}
	jsonl, hid, harn := locateExact("/ws/acme-api", wantSID)
	if harn != "codex" || hid != wantSID || jsonl != want {
		t.Fatalf("locateExact=%q hid=%q harn=%q want %s", jsonl, hid, harn, want)
	}
	// Missing sid + cwd is how LocateCodex stamps the newest file with the asked id.
	trap := harness.LocateCodex("", "missing-sid", "/ws/acme-api")
	if trap.JSONL != decoy {
		t.Fatalf("setup: expected cwd fallback to decoy, got %q", trap.JSONL)
	}
	miss, missID, _ := locateExact("/ws/acme-api", "missing-sid")
	if miss != "" || missID != "" {
		t.Fatalf("locateExact fell back to newest cwd: %q %q", miss, missID)
	}
}

func TestAskCatchUpResetsOnShrink(t *testing.T) {
	st := tmpStore(t)
	jsonl := writeJSONL(t, t.TempDir(), "tape.jsonl", catchUpLine+catchUpLine)
	if err := st.UpsertSession(store.Session{
		JSONL: jsonl, SessionID: "s-shrink", Harness: "grok",
		Workspace: filepath.Dir(jsonl), Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	_ = askAt(t, st, Request{
		Project: "acme/api", SessionID: "s-shrink",
		Question: "how does catch-up work", Goal: "keep catch-up local",
		Paths: []string{"internal/write/catchup.go"},
	})
	if st.Cursor(jsonl) == 0 {
		t.Fatal("first ingest")
	}
	small := `{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}` + "\n"
	if err := os.WriteFile(jsonl, []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}
	out := askAt(t, st, Request{
		Project: "acme/api", SessionID: "s-shrink",
		Question: "what limiter", Goal: "keep limiter in-process",
		Paths: []string{"src/middleware/auth.ts"},
	})
	got := sessionClaims(t, st, "acme/api", "s-shrink")
	var blob string
	for _, r := range got {
		blob += r.Text
	}
	if !strings.Contains(blob+textsOf(out), "limiter") {
		t.Fatalf("shrink did not re-ingest: cursor=%d claims=%+v pack=%+v", st.Cursor(jsonl), got, out)
	}
}

func TestPlanningIWillAskNotLetMeAsk(t *testing.T) {
	if !gate.Planning("I will ask lossless first.") {
		t.Fatal("i will ask")
	}
	if gate.Planning("Let me ask lossless what's already decided.") {
		t.Fatal("let me ask is not planning")
	}
}

func TestExtractNoiseKeepsPointOneThreeRemember(t *testing.T) {
	if retrieveNoise("0.1.3 / 0.3 extract-clean: gate failed-work-first. Real pathful faileds stay.") {
		t.Fatal("0.1.3 remember is not residue")
	}
	if !retrieveNoise("The next product is: the model sees it before it retries the failed work.") {
		t.Fatal("roadmap next-product")
	}
}

func retrieveNoise(text string) bool {
	return extractNoise(claim.Record{Type: "failed", Text: text})
}

func TestAskDropsReadmeFailedFirstResidue(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "FAILFIRST", Type: "failed", SessionID: "s-live",
		Text:      "Failed work first, then what already shipped.",
		CreatedAt: "2026-08-18T17:41:09Z",
	})
	writeRec(t, st, claim.Record{
		ID: "REALFAIL", Type: "failed", SessionID: "s-live",
		Text:      "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-08-17T18:51:54Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what failed on auth",
		Goal:     "fix the limiter",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	got := textsOf(out)
	if strings.Contains(got, "Failed work first") {
		t.Fatalf("readme residue packed: %+v", out)
	}
	if !strings.Contains(got, "Redis") {
		t.Fatalf("real failed missed: %+v", out)
	}
}

func TestAskDropsStoredIllAskPlanning(t *testing.T) {
	if !gate.Planning("I'll ask lossless what's already decided, then look through the repo.") {
		t.Fatal("gate")
	}
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "ASKPLAN", Type: "decision", SessionID: "s-live",
		Text:      "I'll ask lossless what's already decided, then look through the repo.",
		CreatedAt: "2026-08-18T05:21:30Z",
	})
	writeRec(t, st, claim.Record{
		ID: "REALCATCH", Type: "decision", SessionID: "s-live",
		Text:      "Use catchup, not sidecar, in internal/write/catchup.go.",
		Paths:     []string{"internal/write/catchup.go"},
		CreatedAt: "2026-08-18T17:13:58Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "how does catch-up work",
		Goal:     "keep catch-up local",
		Paths:    []string{"internal/write/catchup.go"},
	})
	got := textsOf(out)
	if strings.Contains(got, "I'll ask lossless") {
		t.Fatalf("planning packed: %+v", out)
	}
	if !strings.Contains(got, "sidecar") {
		t.Fatalf("real decision missed: %+v", out)
	}
}
