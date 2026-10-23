package store

import (
	"path/filepath"
	"testing"
)

// The watcher keeps the session list between ticks and reloads it when
// SessionsGen moves, so the counter must move on every real change and
// stay put when a catch-up re-upserts the row it already has.
func TestSessionsGenMovesOnlyOnChange(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sess := Session{JSONL: "/x/a.jsonl", SessionID: "a", Harness: "claude", Workspace: "/ws", Project: "o/r"}

	g0 := st.SessionsGen()
	if err := st.UpsertSession(sess); err != nil {
		t.Fatal(err)
	}
	g1 := st.SessionsGen()
	if g1 == g0 {
		t.Fatal("insert did not move the generation")
	}
	if err := st.UpsertSession(sess); err != nil {
		t.Fatal(err)
	}
	if st.SessionsGen() != g1 {
		t.Fatal("re-upserting an unchanged row moved the generation")
	}
	sess.Workspace = "/ws2"
	if err := st.UpsertSession(sess); err != nil {
		t.Fatal(err)
	}
	g2 := st.SessionsGen()
	if g2 == g1 {
		t.Fatal("a changed row did not move the generation")
	}
	got, ok := st.SessionByJSONL(sess.JSONL)
	if !ok || got.Workspace != "/ws2" {
		t.Fatalf("row not updated: %+v %v", got, ok)
	}
	if err := st.DeleteSession(sess.JSONL); err != nil {
		t.Fatal(err)
	}
	g3 := st.SessionsGen()
	if g3 == g2 {
		t.Fatal("delete did not move the generation")
	}
	if err := st.DeleteSession(sess.JSONL); err != nil {
		t.Fatal(err)
	}
	if st.SessionsGen() != g3 {
		t.Fatal("deleting a missing row moved the generation")
	}
}

// A project key is stored normalized, so re-upserting the raw form of the
// same key is no change.
func TestUpsertSessionNormalizedProjectIsNoChange(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sess := Session{JSONL: "/x/b.jsonl", SessionID: "b", Harness: "pi", Workspace: "/ws", Project: "Owner/Repo"}
	if err := st.UpsertSession(sess); err != nil {
		t.Fatal(err)
	}
	g := st.SessionsGen()
	if err := st.UpsertSession(sess); err != nil {
		t.Fatal(err)
	}
	if st.SessionsGen() != g {
		t.Fatal("same project in raw form moved the generation")
	}
}
