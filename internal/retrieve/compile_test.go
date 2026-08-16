package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
)

func writeJSONL(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRichDoesNotCompile(t *testing.T) {
	st := tmpStore(t)
	e := Engine{Store: st, LocateSession: func(string, string) string {
		t.Fatal("should not locate when rich")
		return ""
	}}
	q, err := normalize(Request{Project: "acme/api", Paths: []string{"src/billing/export.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	out := e.maybeCompile(Request{Project: "acme/api", Paths: []string{"src/billing/export.ts"}}, q)
	if out.PathKeys[0] != "src/billing/export.ts" && !containsStr(out.PathKeys, "export.ts") {
		t.Fatalf("%+v", out)
	}
}

func TestCompileFillsThinAskFromTail(t *testing.T) {
	st := tmpStore(t)
	p := writeJSONL(t, t.TempDir(), "s.jsonl",
		"{\"role\":\"user\",\"content\":\"add rate limiting to src/middleware/auth.ts after the redis failure\"}\n")
	e := Engine{Store: st, LocateSession: func(string, string) string { return p }}
	q, err := normalize(Request{Project: "acme/api"})
	if err != nil || !q.Head {
		t.Fatalf("%+v %v", q, err)
	}
	out := e.maybeCompile(Request{Project: "acme/api"}, q)
	if out.Head {
		t.Fatalf("expected session-conditioned: %+v", out)
	}
	if !containsStr(out.PathKeys, "src/middleware/auth.ts") && !containsStr(out.PathKeys, "auth.ts") {
		t.Fatalf("missing path: %+v", out)
	}
	if !containsStr(out.LookupTokens, "rate") && !containsStr(out.QuestionTokens, "rate") {
		t.Fatalf("missing rate tokens: %+v", out)
	}
	if !containsStr(out.LookupTokens, "failure") && !containsStr(out.LookupTokens, "failed") {
		t.Fatalf("missing failed tokens: %+v", out)
	}
}

func TestCompileNoopOnMissingSession(t *testing.T) {
	st := tmpStore(t)
	e := Engine{Store: st, Home: t.TempDir()}
	q, _ := normalize(Request{Project: "acme/api"})
	out := e.maybeCompile(Request{Project: "acme/api"}, q)
	if !out.Head {
		t.Fatalf("%+v", out)
	}
}

func TestNewestJSONLPrefersLatest(t *testing.T) {
	dir := t.TempDir()
	old := writeJSONL(t, dir, "old.jsonl", "{\"role\":\"user\",\"content\":\"old\"}\n")
	_ = os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	newp := writeJSONL(t, dir, "new.jsonl", "{\"role\":\"user\",\"content\":\"new\"}\n")
	got := newestJSONL(dir)
	if got != newp {
		t.Fatalf("got %s want %s", got, newp)
	}
	if newestJSONL(filepath.Join(dir, "missing")) != "" {
		t.Fatal("missing dir")
	}
}

func TestThinAskCompilesAndPacksRedisFailed(t *testing.T) {
	st := seed(t)
	p := writeJSONL(t, t.TempDir(), "s.jsonl",
		"{\"role\":\"assistant\",\"content\":\"looking at src/middleware/auth.ts\"}\n{\"role\":\"user\",\"content\":\"add rate limiting, the redis pool failed\"}\n")
	e := Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	}, LocateSession: func(string, string) string { return p }}
	out, err := e.Ask(Request{Project: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textsOf(out), "Redis token bucket failed") {
		t.Fatalf("C1: %+v", out)
	}
}

func TestRichAskIgnoresAuthTail(t *testing.T) {
	st := seed(t)
	p := writeJSONL(t, t.TempDir(), "s.jsonl",
		"{\"role\":\"user\",\"content\":\"fix src/middleware/auth.ts redis rate limiting\"}\n")
	e := Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	}, LocateSession: func(string, string) string { return p }}
	out, err := e.Ask(Request{
		Project:  "acme/api",
		Question: "export invoices",
		Goal:     "export invoices",
		Paths:    []string{"src/billing/export.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Context) == 0 {
		t.Fatal("empty")
	}
	if strings.Contains(out.Context[0].Text, "Redis") {
		t.Fatalf("C2 redis #1: %+v", out.Context)
	}
}

func containsStr(xs []string, needle string) bool {
	for _, x := range xs {
		if x == needle {
			return true
		}
	}
	return false
}

func TestLocateUsesStoreRaw(t *testing.T) {
	st := tmpStore(t)
	dir := filepath.Join(st.Root, "raw", "acme__api", "2026-08")
	writeJSONL(t, dir, "sess.jsonl", "{\"role\":\"user\",\"content\":\"touch src/middleware/auth.ts\"}\n")
	e := Engine{Store: st}
	q, _ := normalize(Request{Project: "acme/api"})
	out := e.maybeCompile(Request{Project: "acme/api"}, q)
	if !containsStr(out.PathKeys, "src/middleware/auth.ts") && !containsStr(out.PathKeys, "auth.ts") {
		t.Fatalf("%+v", out)
	}
}

func TestReadTailSkipsEmptyPathAndBadFile(t *testing.T) {
	if readTail("") != nil {
		t.Fatal("empty")
	}
	if readTail(filepath.Join(t.TempDir(), "nope.jsonl")) != nil {
		t.Fatal("missing")
	}
}

func TestCompileRecentClaimPaths(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "RP", Type: "decision", Text: "Keep limiter in src/middleware/auth.ts",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-01T00:00:00Z",
	})
	p := writeJSONL(t, t.TempDir(), "s.jsonl", "{\"role\":\"user\",\"content\":\"keep going\"}\n")
	e := Engine{Store: st, LocateSession: func(string, string) string { return p }}
	q, _ := normalize(Request{Project: "acme/api"})
	out := e.maybeCompile(Request{Project: "acme/api"}, q)
	if !containsStr(out.PathKeys, "src/middleware/auth.ts") && !containsStr(out.PathKeys, "auth.ts") {
		t.Fatalf("recent paths: %+v", out)
	}
}
