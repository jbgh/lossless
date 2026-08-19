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
	if extractNoise(claim.Record{Type: "decision", Text: "slice-loop is autonomous."}) {
		t.Fatal("slice-loop remember is not residue")
	}
	if retrieveNoise("Redis token bucket failed in staging.") {
		t.Fatal("Redis failed lock")
	}
	if !retrieveNoise("The next product is: the model sees it before it retries the failed work.") {
		t.Fatal("roadmap next-product")
	}
	if !retrieveNoise("You can switch between them and never lose memory.") {
		t.Fatal("slogan residue")
	}
	if !retrieveNoise("Intended gap: Shipped channel is still 0.1.5: leftover.") {
		t.Fatal("intended-gap residue")
	}
	if !retrieveNoise("Same failure twice pauses as no-progress.") {
		t.Fatal("same-failure residue")
	}
	if retrieveNoise("Same failure twice: Redis token bucket still 429 in staging.") {
		t.Fatal("same-failure job-1 lock")
	}
	if !retrieveNoise("ProcessState is in SkipProse as planned; Working on … next is no longer a required kept state.") {
		t.Fatal("process-state recap")
	}
	if retrieveNoise("Tests failed to connect after we raised the pool.") {
		t.Fatal("pathless Tests failed to lock")
	}
	if !retrieveNoise("The SQLITE_BUSY failure looks flaky; I'll rerun the full eval suite to confirm.") {
		t.Fatal("i'll rerun residue")
	}
	if !retrieveNoise("0.1.7 extract-clean tree: slogans, I'll-run, intended-gap, same-failure-twice, process-state gated.") {
		t.Fatal("hyphenated operator labels")
	}
	if !retrieveNoise("Tests failed on the live residue as expected.") {
		t.Fatal("live residue recap")
	}
	if !retrieveNoise("Slogans, I'll-run, intended-gap, right-next-step, same-failure-twice, and the test-pass recap are gone.") {
		t.Fatal("slogans recap")
	}
	if !retrieveNoise("tree: productCopy slogans not bare never; space-form Same failure twice Redis still extracts; 0.1.") {
		t.Fatal("19:09 recap")
	}
	if !retrieveNoise("e_test.go lock the recap row, not a pathful named-lock failed (contrast Redis token bucket).") {
		t.Fatal("19:15 recap")
	}
	if !retrieveNoise("Redis faileds, stick-with decisions, space-form “same failure twice” job-1, and pathless `Tests failed to` still store and pack.") {
		t.Fatal("19:03 recap")
	}
	if !retrieveNoise("Shipping the current tree would lock fail-close skips and recap-as-failed.") {
		t.Fatal("19:22 recap")
	}
	if !retrieveNoise("They found real control-flow holes: budget headroom too small, a failed semver check aborting the whole batch, and a “shipped N” summary when the run just ran out of slots.") {
		t.Fatal("17:43 recap")
	}
	if !retrieveNoise("One remaining active failed looks recap-like.") {
		t.Fatal("19:57 recap")
	}
	if !retrieveNoise("Live recent 8 are slice-loop / 0.1.5 decisions and version state; `recent_noise=0`; 17:43 is not a packed failed.") {
		t.Fatal("20:10 recap")
	}
	if !retrieveNoise("Inspect recent on the live store still includes the recap failed “Live recent 8 are slice-loop…”, which the uncommitted 0.1.7 gates already skip (`a packed failed` / `inspect recent 8`).") {
		t.Fatal("20:13 recap")
	}
	if retrieveNoise("Named locks in catchup.go stay on the session JSONL.") {
		t.Fatal("named-lock keep")
	}
	if retrieveNoise("File locks are tested in concurrent_test.go.") {
		t.Fatal("file-lock keep")
	}
	if retrieveNoise("concurrent_test.go File locks failed to acquire.") {
		t.Fatal("concurrent_test.go-first keep")
	}
	if retrieveNoise("They found Redis token bucket failed in src/middleware/auth.ts staging.") {
		t.Fatal("they-found Redis keep")
	}
	if retrieveNoise("They found Redis token bucket failed in this session in src/middleware/auth.ts.") {
		t.Fatal("they-found Redis in this session keep")
	}
	if !retrieveNoise("A They-found + Redis/path failed still stores and packs.") {
		t.Fatal("still stores and packs meta")
	}
	if !retrieveNoise(`{"diff_stat":"13 files changed, 322 insertions(+), 6 deletions(-)","ok":true,"summary":"Gate SkipProse now treats hyphenated I'll-run."}`) {
		t.Fatal("eval json dump")
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
