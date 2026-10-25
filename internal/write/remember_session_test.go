package write

import (
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// A remember that carries only the harness-stamped session id picks up
// harness, project, and workspace from the session row the watcher
// indexed, so it books under omp instead of "other" and needs no
// workspace_root from the model.
func TestRememberFillsFromSession(t *testing.T) {
	st := tmpStore(t)
	if err := st.UpsertSession(store.Session{
		JSONL: "/tmp/omp/chat.jsonl", SessionID: "omp-audit", Harness: "omp",
		Workspace: "/Users/x/repo", Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := Remember(st, claim.Record{
		Type: "decision", Text: "Keep the gate read-only in audits.",
		SessionID: "omp-audit",
	})
	if err != nil {
		t.Fatalf("remember failed: %v", err)
	}
	got, ok := st.Get(out.IDs[0])
	if !ok {
		t.Fatal("record missing")
	}
	if got.Harness != "omp" || got.ProjectKey != "acme/api" || got.WorkspaceRoot != "/Users/x/repo" {
		t.Fatalf("session fill wrong: harness=%q project=%q workspace=%q",
			got.Harness, got.ProjectKey, got.WorkspaceRoot)
	}
}

// Caller-sent identity wins over the session row: a remember that names
// its own project and harness keeps them even though the session says
// otherwise.
func TestRememberCallerFieldsWin(t *testing.T) {
	st := tmpStore(t)
	if err := st.UpsertSession(store.Session{
		JSONL: "/tmp/omp/chat.jsonl", SessionID: "omp-audit", Harness: "omp",
		Workspace: "/Users/x/repo", Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := Remember(st, claim.Record{
		Type: "decision", Text: "Cross-project note kept as typed.",
		ProjectKey: "other/proj", Harness: "claude",
		SessionID: "omp-audit",
	})
	if err != nil {
		t.Fatalf("remember failed: %v", err)
	}
	got, ok := st.Get(out.IDs[0])
	if !ok {
		t.Fatal("record missing")
	}
	if got.ProjectKey != "other/proj" || got.Harness != "claude" {
		t.Fatalf("caller fields overwritten: project=%q harness=%q",
			got.ProjectKey, got.Harness)
	}
}

// A session id the watcher never saw fills nothing: the remember still
// refuses for missing project identity instead of inventing one.
func TestRememberUnknownSessionStillRefused(t *testing.T) {
	st := tmpStore(t)
	_, err := Remember(st, claim.Record{
		Type: "decision", Text: "No project, no session row.",
		SessionID: "no-such-session",
	})
	if err == nil || !strings.Contains(err.Error(), "project or workspace_root required") {
		t.Fatalf("expected the project error, got %v", err)
	}
}

// A caller-sent workspace wins over the session row: the record books to
// that workspace's project, not to the session's.
func TestRememberCallerWorkspaceWins(t *testing.T) {
	st := tmpStore(t)
	if err := st.UpsertSession(store.Session{
		JSONL: "/tmp/omp/chat.jsonl", SessionID: "omp-audit", Harness: "omp",
		Workspace: "/Users/x/repoA", Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir() // no git remote: the key comes from the path, not acme/api
	out, err := Remember(st, claim.Record{
		Type: "decision", Text: "Booked where the caller said.",
		WorkspaceRoot: ws, SessionID: "omp-audit",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(out.IDs[0])
	if got.ProjectKey == "acme/api" || got.WorkspaceRoot != ws {
		t.Fatalf("session overrode caller workspace: project=%q workspace=%q", got.ProjectKey, got.WorkspaceRoot)
	}
	if got.Harness != "omp" {
		t.Fatalf("harness still fills from the session: %q", got.Harness)
	}
}

// A caller-sent project does not pick up another repo's workspace.
func TestRememberCallerProjectGetsNoForeignWorkspace(t *testing.T) {
	st := tmpStore(t)
	if err := st.UpsertSession(store.Session{
		JSONL: "/tmp/omp/chat.jsonl", SessionID: "omp-audit", Harness: "omp",
		Workspace: "/Users/x/repo", Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := Remember(st, claim.Record{
		Type: "decision", Text: "Project only.", ProjectKey: "other/proj", SessionID: "omp-audit",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(out.IDs[0])
	if got.ProjectKey != "other/proj" || got.WorkspaceRoot != "" {
		t.Fatalf("project=%q workspace=%q", got.ProjectKey, got.WorkspaceRoot)
	}
}

// One session id indexed under two projects fills no project: picking a
// row would be a guess.
func TestRememberAmbiguousSessionFillsNoProject(t *testing.T) {
	st := tmpStore(t)
	for _, s := range []store.Session{
		{JSONL: "/tmp/a.jsonl", SessionID: "sid-1", Harness: "claude", Workspace: "/Users/x/a", Project: "acme/a"},
		{JSONL: "/tmp/b.jsonl", SessionID: "sid-1", Harness: "claude", Workspace: "/Users/x/b", Project: "acme/b"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Remember(st, claim.Record{Type: "decision", Text: "Which repo?", SessionID: "sid-1"})
	if err == nil || !strings.Contains(err.Error(), "project or workspace_root required") {
		t.Fatalf("ambiguous session must not fill a project, got %v", err)
	}
}

// A sentinel session id never locates a session row, the way ask treats it.
func TestRememberSentinelSessionIDIgnored(t *testing.T) {
	st := tmpStore(t)
	if err := st.UpsertSession(store.Session{
		JSONL: "/tmp/d.jsonl", SessionID: "default", Harness: "omp",
		Workspace: "/Users/x/repo", Project: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := Remember(st, claim.Record{Type: "decision", Text: "No fill.", SessionID: " default "})
	if err == nil || !strings.Contains(err.Error(), "project or workspace_root required") {
		t.Fatalf("sentinel id filled identity: %v", err)
	}
}
