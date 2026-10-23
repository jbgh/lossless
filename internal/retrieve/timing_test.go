package retrieve

import (
	"os"
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

	// A behind file whose new bytes are not a full line yet copies
	// nothing, so it is not counted as a session caught up.
	if err := os.WriteFile(tape, append(mustRead(t, tape), []byte(`{"type":"assistant","content":"half`)...), 0o644); err != nil {
		t.Fatal(err)
	}
	_, tm, err = AskTimed(st, Request{WorkspaceRoot: ws, Project: "acme/api", Goal: "rate limiting"})
	if err != nil {
		t.Fatal(err)
	}
	if tm.CatchUps != 0 {
		t.Fatalf("a catch-up that copied nothing was counted: %+v", tm)
	}
}

// A project lookup made inside the ask-time catch-up counts as project
// time, not catch-up time, so a slow git shows up as git.
func TestAskTimedBillsProjectLookupInsideCatchUp(t *testing.T) {
	st := tmpStore(t)
	ws := t.TempDir()
	tm := &AskTiming{}
	(Engine{Store: st}).maybeCatchUp(Request{WorkspaceRoot: ws, timing: tm})
	if tm.Project <= 0 {
		t.Fatalf("project lookup inside catch-up not timed as project: %+v", tm)
	}
	if tm.CatchUp < 0 {
		t.Fatalf("negative catch-up: %+v", tm)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
