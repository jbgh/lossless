package watch

import (
	"testing"

	"lossless/internal/store"
)

// 2026-09-18: a tick took 38 seconds, all of it in idleSeal. Discover lists
// every OpenCode session in the harness database (1,087 here) without a
// project, and idleSeal resolved each one by running git, every tick: a
// thousand git processes a second-long tick, nonstop, since v0.1.0. A
// session the store already knows reuses its stored project, and a
// workspace that still has to be resolved is resolved once per TTL.
func TestIdleSealDoesNotShellOutForKnownSessions(t *testing.T) {
	calls := 0
	old := resolveProject
	resolveProject = func(ws string) string { calls++; return "acme/api" }
	t.Cleanup(func() { resolveProject = old; resetProjectCache() })
	resetProjectCache()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	known := []store.Session{{JSONL: "/x/spool/virtual-ses_1.jsonl", SessionID: "ses_1", Harness: "opencode", Workspace: "/work/api", Project: "acme/api"}}
	idx := projectsBySession(known)

	tg := Target{Harness: "opencode", SessionID: "ses_1", Workspace: "/work/api"}
	for i := 0; i < 5; i++ {
		idleSeal(st, tg, Options{}, idx)
	}
	if calls != 0 {
		t.Fatalf("a known session must reuse its stored project, git ran %d times", calls)
	}

	unknown := Target{Harness: "opencode", SessionID: "ses_new", Workspace: "/work/other"}
	for i := 0; i < 5; i++ {
		idleSeal(st, unknown, Options{}, idx)
	}
	if calls != 1 {
		t.Fatalf("an unknown workspace resolves once per TTL, git ran %d times", calls)
	}
	if got := sealProject(Target{Project: "given/key", Workspace: "/work/api"}, idx); got != "given/key" {
		t.Fatalf("an explicit project wins: %q", got)
	}
}
