package write

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/store"
)

func tapeUnder(t *testing.T, st *store.Store, dirKey, session, text string) {
	t.Helper()
	dir := filepath.Join(st.Root, "raw", dirKey, "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tape := `{"type":"user","content":"` + text + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, session+".jsonl"), []byte(tape), 0o600); err != nil {
		t.Fatal(err)
	}
}

// merge-project moves a key's records and sessions and leaves its tapes
// in the old folder. Reading those tapes again must not write the old
// key back.
func TestBackfillAfterMergeProjectStaysMerged(t *testing.T) {
	st := tmpStore(t)
	tapeUnder(t, st, "acme__api", "sess-1", "lets use the smaller model for all subagents from now on")
	onRecord(t, st, "sess-1")
	if res, err := Backfill(st, "", false); err != nil || res.Added != 1 {
		t.Fatalf("backfill: %+v %v", res, err)
	}
	if _, err := st.MoveProject("acme/api", "acme/core", false); err != nil {
		t.Fatal(err)
	}
	if dry, err := Backfill(st, "", true); err != nil || dry.Added != 0 {
		t.Fatalf("dry run after the merge: %+v %v", dry, err)
	}
	if res, err := Backfill(st, "", false); err != nil || res.Added != 0 {
		t.Fatalf("backfill after the merge: %+v %v", res, err)
	}
	if got := saidOf(t, st, "acme/api"); len(got) != 0 {
		t.Fatalf("the old key is back: %q", got)
	}
	if got := saidOf(t, st, "acme/core"); len(got) != 1 {
		t.Fatalf("merged key: %q", got)
	}
	// --project follows the session, not the folder the tape sits in.
	if res, _ := Backfill(st, "acme/core", true); res.Tapes != 1 {
		t.Fatalf("merged key read %d tapes", res.Tapes)
	}
	if res, _ := Backfill(st, "acme/api", true); res.Tapes != 0 {
		t.Fatalf("old key read %d tapes", res.Tapes)
	}
}

// A turn this version keeps and the writing version did not is filed
// under the session's project, wherever the tape sits.
func TestBackfillFilesNewTurnsUnderTheSessionsProject(t *testing.T) {
	st := tmpStore(t)
	tapeUnder(t, st, "acme__api", "sess-1", "lets use the smaller model for all subagents from now on")
	if err := st.UpsertSession(store.Session{JSONL: "/src/sess-1.jsonl", SessionID: "sess-1", Harness: "grok", Project: "acme/core"}); err != nil {
		t.Fatal(err)
	}
	if res, err := Backfill(st, "", false); err != nil || res.Added != 1 {
		t.Fatalf("backfill: %+v %v", res, err)
	}
	if got := saidOf(t, st, "acme/api"); len(got) != 0 {
		t.Fatalf("filed under the tape folder: %q", got)
	}
	if got := saidOf(t, st, "acme/core"); len(got) != 1 {
		t.Fatalf("session's project: %q", got)
	}
}

// A session whose project changed mid-way keeps the turns stored under
// its first key. They are known; they are not stored twice.
func TestBackfillKnowsATurnStoredUnderTheTapesKey(t *testing.T) {
	st := tmpStore(t)
	tapeUnder(t, st, "acme__api", "sess-1", "lets use the smaller model for all subagents from now on")
	onRecord(t, st, "sess-1")
	if res, err := Backfill(st, "", false); err != nil || res.Added != 1 {
		t.Fatalf("backfill: %+v %v", res, err)
	}
	if err := st.UpsertSession(store.Session{JSONL: "/src/sess-1.jsonl", SessionID: "sess-1", Harness: "grok", Project: "acme/core"}); err != nil {
		t.Fatal(err)
	}
	if res, err := Backfill(st, "", false); err != nil || res.Added != 0 {
		t.Fatalf("stored twice: %+v %v", res, err)
	}
	if got := saidOf(t, st, "acme/core"); len(got) != 0 {
		t.Fatalf("copied to the new key: %q", got)
	}
}

// One session id can have tapes in two projects that both still hold a
// row for it. Each tape stays under its own folder's key.
func TestBackfillKeepsATapeUnderItsFolderWhileTheSessionHasARowThere(t *testing.T) {
	st := tmpStore(t)
	tapeUnder(t, st, "acme__api", "sess-1", "lets use the smaller model for all subagents from now on")
	tapeUnder(t, st, "acme__web", "sess-1", "always keep the sidebar collapsed on the settings page please")
	for _, s := range []store.Session{
		{JSONL: "/api/sess-1.jsonl", SessionID: "sess-1", Harness: "grok", Project: "acme/api"},
		{JSONL: "/web/sess-1.jsonl", SessionID: "sess-1", Harness: "grok", Project: "acme/web"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	if res, err := Backfill(st, "", false); err != nil || res.Added != 2 {
		t.Fatalf("backfill: %+v %v", res, err)
	}
	if got := saidOf(t, st, "acme/api"); len(got) != 1 {
		t.Fatalf("acme/api: %q", got)
	}
	if got := saidOf(t, st, "acme/web"); len(got) != 1 {
		t.Fatalf("acme/web: %q", got)
	}
	if res, _ := Backfill(st, "acme/api", true); res.Tapes != 1 {
		t.Fatalf("--project acme/api read %d tapes", res.Tapes)
	}
}
