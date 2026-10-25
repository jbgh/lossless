package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// An ask carrying only the harness-stamped session id keeps its project
// identity from the session row the watcher indexed.
func TestAskFillsProjectFromSession(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "DECJOSE", Type: "decision", SessionID: "grok-auth", Source: "import",
		Text:      "We decided to use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	if err := st.UpsertSession(store.Session{
		JSONL: "/tmp/omp/chat.jsonl", SessionID: "omp-audit", Harness: "omp",
		Workspace: "/Users/x/repo", Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := Ask(st, Request{
		SessionID: "omp-audit",
		Question:  "what did we decide about jose",
	})
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}
	if out.Project != "acme/api" {
		t.Fatalf("project not filled from session: %+v", out)
	}
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("jose decision missing: %+v", out)
	}
}

// Without a session row the ask still fails on missing project identity;
// the fill never invents one.
func TestAskUnknownSessionStillRefused(t *testing.T) {
	st := tmpStore(t)
	_, err := Ask(st, Request{
		SessionID: "no-such-session",
		Question:  "what did we decide",
	})
	if err == nil || !strings.Contains(err.Error(), "project or workspace_root required") {
		t.Fatalf("expected the project error, got %v", err)
	}
}

// A session-only ask gets the workspace too, so the workspace steps run.
func TestFillFromSessionSetsWorkspace(t *testing.T) {
	st := tmpStore(t)
	if err := st.UpsertSession(store.Session{
		JSONL: "/tmp/omp/chat.jsonl", SessionID: "omp-audit", Harness: "omp",
		Workspace: "/Users/x/repo", Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	got := Engine{Store: st}.fillFromSession(Request{SessionID: "omp-audit"})
	if got.Project != "acme/api" || got.WorkspaceRoot != "/Users/x/repo" {
		t.Fatalf("fill: project=%q workspace=%q", got.Project, got.WorkspaceRoot)
	}
	// Caller fields win, and neither is mixed in from the session.
	got = Engine{Store: st}.fillFromSession(Request{SessionID: "omp-audit", WorkspaceRoot: "/Users/x/other"})
	if got.Project != "" || got.WorkspaceRoot != "/Users/x/other" {
		t.Fatalf("caller workspace: project=%q workspace=%q", got.Project, got.WorkspaceRoot)
	}
}

// Rows of one session id that disagree fill nothing they disagree on.
func TestFillFromSessionAmbiguous(t *testing.T) {
	st := tmpStore(t)
	for _, s := range []store.Session{
		{JSONL: "/tmp/a.jsonl", SessionID: "sid-1", Harness: "claude", Workspace: "/Users/x/a", Project: "acme/a"},
		{JSONL: "/tmp/b.jsonl", SessionID: "sid-1", Harness: "claude", Workspace: "/Users/x/b", Project: "acme/b"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	got := Engine{Store: st}.fillFromSession(Request{SessionID: "sid-1"})
	if got.Project != "" || got.WorkspaceRoot != "" {
		t.Fatalf("ambiguous session filled: project=%q workspace=%q", got.Project, got.WorkspaceRoot)
	}
}
