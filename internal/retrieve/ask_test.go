package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func tmpStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func writeRec(t *testing.T, st *store.Store, rec claim.Record) {
	t.Helper()
	if rec.ProjectKey == "" {
		rec.ProjectKey = "acme/api"
	}
	if rec.Harness == "" {
		rec.Harness = "grok"
	}
	if rec.SessionID == "" {
		rec.SessionID = "eval"
	}
	if rec.Source == "" {
		rec.Source = "import"
	}
	if rec.Status == "" {
		rec.Status = "active"
	}
	if rec.Paths == nil {
		rec.Paths = []string{}
	}
	if _, err := st.WriteClaim(rec); err != nil {
		t.Fatal(err)
	}
}

func seed(t *testing.T) *store.Store {
	t.Helper()
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JFAIL", Type: "failed",
		Text:      "Redis token bucket failed in staging; connection pool exhausted.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JJOSE", Type: "decision",
		Text:      "Use jose, not jsonwebtoken, for Edge.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-20T11:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JRATE", Type: "decision",
		Text:      "Rate limiter lives in src/middleware/auth.ts as an in-process token bucket.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-22T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JAUTHZ", Type: "constraint",
		Text:      "Never log Authorization headers.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-06-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JBILL", Type: "state",
		Text:      "Working on billing invoices export, not auth.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-10T00:00:00Z",
	})
	return st
}

func askAt(t *testing.T, st *store.Store, req Request) Response {
	t.Helper()
	e := Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	}}
	out, err := e.Ask(req)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func textsOf(out Response) string {
	var b strings.Builder
	for _, h := range out.Context {
		b.WriteString(h.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestAskDropsFixtureSessions(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "FIXJOSE", Type: "decision", SessionID: "grok-auth", Source: "import",
		Text:      "We decided to use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "REALSKILL", Type: "decision", SessionID: "01a003db-f4a6-7f43-a694-082428bbff32", Source: "remember",
		Text:      "Setup writes the lossless skill into every harness native dir.",
		CreatedAt: "2026-08-17T04:54:39Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what did we decide about jose and the skill",
		Goal:     "install the skill",
	})
	got := textsOf(out)
	if strings.Contains(got, "jose") {
		t.Fatalf("fixture won ask: %+v", out)
	}
	if !strings.Contains(got, "skill") {
		t.Fatalf("real decision missing: %+v", out)
	}
}

func TestAskDropsOonSkillState(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "JOSE", Type: "decision",
		Text:  "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-07-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "SKILLSTATE", Type: "state",
		Text:  "This is still not a guarantee — a model can ignore a skill — but it is no longer one sentence on a tool next to grep.",
		Paths: []string{".claude/skills/lossless/SKILL.md"}, CreatedAt: "2026-08-17T04:41:01Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "why not jsonwebtoken",
		Goal:     "add inspect visibility",
		Paths:    []string{"src/middleware/auth.ts", "internal/inspect/inspect.go"},
	})
	if textsOf(out) != "" && strings.Contains(textsOf(out), "ignore a skill") {
		t.Fatalf("skill-state packed: %+v", out.Context)
	}
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("jose missed: %+v", out.Context)
	}
}

func TestRateLimitReturnsFailedAndWarning(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what do we know about rate limiting on auth?",
		Goal:     "add rate limiting",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "Redis token bucket failed") {
		t.Fatalf("missing redis failed: %+v", out)
	}
	if !hasWarn(out, "failed") {
		t.Fatalf("expected failed warning: %+v", out.Warnings)
	}
	if out.Tokens > 1200 {
		t.Fatalf("tokens=%d", out.Tokens)
	}
}

func TestWhyNotJSONWebTokenReturnsJose(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "why not jsonwebtoken",
		Goal:     "pick a jwt library",
	})
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("missing jose: %+v", out)
	}
}

func TestPortableProjectKeys(t *testing.T) {
	st := seed(t)
	a := askAt(t, st, Request{Project: "Acme/API", Question: "jose"})
	b := askAt(t, st, Request{Project: "acme__api", Question: "jose"})
	if len(a.Context) == 0 {
		t.Fatal("empty context")
	}
	if ids(a) != ids(b) {
		t.Fatalf("ids differ:\n%s\n%s", ids(a), ids(b))
	}
}

func TestColdAskPrefersFailedAndDecision(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{Project: "acme/api"})
	if len(out.Context) == 0 {
		t.Fatal("empty")
	}
	top := out.Context[0].Type
	if top != "failed" && top != "decision" {
		t.Fatalf("top type=%s want failed|decision; %+v", top, out.Context)
	}
	if len(out.Warnings) != 0 {
		t.Fatalf("cold ask should not warn: %v", out.Warnings)
	}
}

func TestStaleVerifyPrefixNotPersisted(t *testing.T) {
	st := tmpStore(t)
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(filepath.Join(ws, "src/middleware"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(ws, "src/middleware/auth.ts")
	if err := os.WriteFile(file, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRec(t, st, claim.Record{
		ID: "01JLIM", Type: "decision",
		Text:      "Keep the in-process limiter in src/middleware/auth.ts",
		Paths:     []string{"src/middleware/auth.ts"},
		PathMtime: map[string]int64{"src/middleware/auth.ts": 1},
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	first := askAt(t, st, Request{
		Project:       "acme/api",
		WorkspaceRoot: ws,
		Goal:          "change auth limiter",
		Paths:         []string{"src/middleware/auth.ts"},
	})
	if len(first.Context) == 0 || !strings.HasPrefix(first.Context[0].Text, "[verify]") {
		t.Fatalf("expected [verify] prefix: %+v", first)
	}
	got, ok := st.Get("01JLIM")
	if !ok || strings.HasPrefix(got.Text, "[verify]") {
		t.Fatal("stale must not persist")
	}
	cold := askAt(t, st, Request{Project: "acme/api"})
	if !strings.Contains(textsOf(cold), "in-process limiter") {
		t.Fatalf("cold lost claim: %+v", cold)
	}
}

func TestEmptyStore(t *testing.T) {
	st := tmpStore(t)
	out := askAt(t, st, Request{Project: "acme/api", Question: "anything"})
	if len(out.Context) != 0 || len(out.Warnings) != 0 {
		t.Fatalf("want empty: %+v", out)
	}
}

func TestMissingProject(t *testing.T) {
	st := tmpStore(t)
	_, err := Ask(st, Request{Question: "hi"})
	if err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("err=%v", err)
	}
}

func TestDiversityPacksOneRestatement(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JA", Type: "decision",
		Text:      "Use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-07-20T11:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JB", Type: "decision",
		Text:      "Use jose not jsonwebtoken for Edge runtime.",
		CreatedAt: "2026-07-21T11:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Question: "why not jsonwebtoken"})
	n := 0
	for _, h := range out.Context {
		if strings.Contains(h.Text, "jose") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("diversity: packed %d jose hits: %+v", n, out.Context)
	}
}

func TestFailedEvictedIntoCap(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JFAIL2", Type: "failed",
		Text:      "Redis token bucket failed in staging; connection pool exhausted.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	for i := 0; i < 8; i++ {
		writeRec(t, st, claim.Record{
			ID:        "01JDEC" + string(rune('A'+i)),
			Type:      "decision",
			Text:      "Decided the auth middleware export invoices rate limiting helper " + strings.Repeat("token ", 4) + string(rune('A'+i)),
			Paths:     []string{"src/middleware/auth.ts"},
			CreatedAt: "2026-08-12T00:00:00Z",
		})
	}
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what do we know about rate limiting on auth?",
		Goal:     "add rate limiting",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "Redis token bucket failed") {
		t.Fatalf("failed dropped at cap: %+v", out)
	}
	if !hasWarn(out, "failed") {
		t.Fatalf("expected failed warning: %v", out.Warnings)
	}
	if len(out.Context) > 5 {
		t.Fatalf("cap 5, got %d", len(out.Context))
	}
}

func TestSupersededNotReturned(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JOLD", Type: "decision",
		Text:      "Use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-07-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JNEW", Type: "decision",
		Text:      "Use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Question: "why not jsonwebtoken"})
	for _, h := range out.Context {
		if h.ID == "01JOLD" || h.Status == "superseded" {
			t.Fatalf("superseded leaked: %+v", h)
		}
	}
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatal("missing active jose")
	}
}

func TestLimitTokensStillReturnsOne(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project:     "acme/api",
		Question:    "jose",
		LimitTokens: 80,
	})
	if len(out.Context) < 1 {
		t.Fatal("expected at least one hit")
	}
	if out.Tokens > 80 {
		// warnings can add a few tokens; hits themselves must have fit
		hitTok := 0
		for _, h := range out.Context {
			hitTok += estimateTokens(mustJSON(h))
		}
		if hitTok > 80 {
			t.Fatalf("hit tokens=%d total=%d", hitTok, out.Tokens)
		}
	}
}

func TestGoalWithoutPathsStillHitsRedisFailed(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal:    "add rate limiting",
	})
	if !strings.Contains(textsOf(out), "Redis token bucket failed") {
		t.Fatalf("expected redis failed via type candidate: %+v", out)
	}
}

func TestBillingAskDoesNotRankRedisFirst(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "export invoices",
		Goal:     "export invoices",
		Paths:    []string{"src/billing/export.ts"},
	})
	if len(out.Context) == 0 {
		t.Fatal("empty")
	}
	if strings.Contains(out.Context[0].Text, "Redis") {
		t.Fatalf("redis failed ranked #1 on billing ask: %+v", out.Context)
	}
}

func TestAuthorizationConstraint(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "Authorization headers",
	})
	if !strings.Contains(textsOf(out), "Never log Authorization") {
		t.Fatalf("missing constraint: %+v", out)
	}
}

func TestPathOnlyColdAsk(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project: "acme/api",
		Paths:   []string{"src/middleware/auth.ts"},
	})
	if strings.Contains(textsOf(out), "billing invoices") {
		t.Fatalf("billing leaked into auth cold path ask: %+v", out)
	}
	if !strings.Contains(textsOf(out), "Redis") && !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("expected auth records: %+v", out)
	}
}

func ids(out Response) string {
	var b strings.Builder
	for _, h := range out.Context {
		b.WriteString(h.ID)
		b.WriteByte(',')
	}
	return b.String()
}

func hasWarn(out Response, needle string) bool {
	for _, w := range out.Warnings {
		if strings.Contains(w, needle) {
			return true
		}
	}
	return false
}
