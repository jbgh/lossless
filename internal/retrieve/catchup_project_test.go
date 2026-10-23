package retrieve

import (
	"path/filepath"
	"testing"

	"lossless/internal/projectkey"
	"lossless/internal/store"
)

// The omitted-sid catch-up reads only this project's sessions (through the
// project index, not a full table scan). A behind session of another
// project is left for its own asks and the watcher.
func TestAskCatchUpStoredWorkspaceStaysInProject(t *testing.T) {
	st := tmpStore(t)
	ws := filepath.Join(t.TempDir(), "acme-api")
	mine := writeJSONL(t, filepath.Join(ws, "s-mine"), "tape.jsonl",
		`{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}`+"\n")
	theirs := writeJSONL(t, filepath.Join(t.TempDir(), "s-theirs"), "tape.jsonl",
		`{"type":"assistant","content":"Use pgbouncer, not direct pools, in db/pool.go."}`+"\n")
	for _, s := range []store.Session{
		{JSONL: mine, SessionID: "s-mine", Harness: "grok", Workspace: ws, Project: "acme/api"},
		{JSONL: theirs, SessionID: "s-theirs", Harness: "grok", Workspace: "/elsewhere", Project: "other/db"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	askAt(t, st, Request{Project: "acme/api", WorkspaceRoot: ws, Goal: "rate limiting", Paths: []string{"src/middleware/auth.ts"}})
	if len(sessionClaims(t, st, "acme/api", "s-mine")) == 0 {
		t.Fatal("this project's behind session was not caught up")
	}
	if st.Cursor(theirs) != 0 || len(sessionClaims(t, st, "other/db", "s-theirs")) != 0 {
		t.Fatal("another project's session was caught up by this ask")
	}
}

// An ask that sends only a workspace (no git origin, so a path-hash key)
// still finds the sessions stored under that key.
func TestAskCatchUpStoredWorkspaceFromWorkspaceOnly(t *testing.T) {
	st := tmpStore(t)
	ws := filepath.Join(t.TempDir(), "plain")
	tape := writeJSONL(t, filepath.Join(ws, "s-plain"), "tape.jsonl",
		`{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}`+"\n")
	key := projectkey.FromWorkspace(ws)
	if err := st.UpsertSession(store.Session{JSONL: tape, SessionID: "s-plain", Harness: "grok", Workspace: ws, Project: key}); err != nil {
		t.Fatal(err)
	}
	askAt(t, st, Request{WorkspaceRoot: ws, Goal: "rate limiting", Paths: []string{"src/middleware/auth.ts"}})
	if len(sessionClaims(t, st, key, "s-plain")) == 0 {
		t.Fatalf("workspace-only ask did not catch up the session stored under %s", key)
	}
}
