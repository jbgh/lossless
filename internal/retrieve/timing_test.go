package retrieve

import (
	"path/filepath"
	"testing"

	"lossless/internal/store"
)

// A timed ask reports the catch-up it ran and the project lookup.
func TestAskTimedSplitsCatchUpAndProject(t *testing.T) {
	st := tmpStore(t)
	ws := filepath.Join(t.TempDir(), "acme-api")
	tape := writeJSONL(t, filepath.Join(ws, "s-behind"), "tape.jsonl",
		`{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}`+"\n")
	if err := st.UpsertSession(store.Session{JSONL: tape, SessionID: "s-behind", Harness: "grok", Workspace: ws, Project: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	_, tm, err := AskTimed(st, Request{WorkspaceRoot: ws, Project: "acme/api", Goal: "rate limiting"})
	if err != nil {
		t.Fatal(err)
	}
	if tm.CatchUps != 1 || tm.CatchUp <= 0 {
		t.Fatalf("catch-up not timed: %+v", tm)
	}

	// Nothing behind now, and the project comes from the workspace.
	_, tm, err = AskTimed(st, Request{WorkspaceRoot: ws, Goal: "rate limiting"})
	if err != nil {
		t.Fatal(err)
	}
	if tm.CatchUps != 0 || tm.Project <= 0 {
		t.Fatalf("second ask: %+v", tm)
	}
}
