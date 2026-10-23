package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lossless/internal/store"
)

// countListSessions swaps the session-table read for a counting wrapper.
func countListSessions(t *testing.T) *int {
	t.Helper()
	n := new(int)
	real := listSessions
	listSessions = func(st *store.Store) ([]store.Session, error) { *n++; return real(st) }
	t.Cleanup(func() { listSessions = real })
	return n
}

func knownHas(w *Watcher, jsonl string) bool {
	for _, s := range w.known {
		if s.JSONL == jsonl {
			return true
		}
	}
	return false
}

// The session list is read once and kept while the store's session
// generation holds: an idle tick reads no rows (5,020 rows a tick on this
// machine before), a session added or deleted through the same store is
// seen on the next tick, and a catch-up that re-upserts an unchanged row
// does not force a reload.
func TestWatcherKeepsTheSessionListUntilItChanges(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	reads := countListSessions(t)
	_, catchUps := countPeeks(t)
	w := NewWatcher()
	for i := 0; i < 2; i++ {
		if _, err := w.Tick(st, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if *reads != 1 {
		t.Fatalf("idle ticks read the session table %d times, want 1", *reads)
	}

	file := filepath.Join(root, "elsewhere", "chat_history.jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	age(t, file)
	sess := store.Session{JSONL: file, SessionID: "g1", Harness: "grok", Workspace: "/Users/jay/dev/api", Project: "jay/api"}
	if err := st.UpsertSession(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(st, Options{}); err != nil {
		t.Fatal(err)
	}
	if *reads != 2 || catchUps[file] != 1 {
		t.Fatalf("an added session: reads %d catch-ups %d, want 2 and 1", *reads, catchUps[file])
	}
	if _, err := w.Tick(st, Options{}); err != nil {
		t.Fatal(err)
	}
	if *reads != 2 {
		t.Fatalf("the catch-up's unchanged upsert forced a reload: %d reads", *reads)
	}

	if err := st.DeleteSession(file); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(st, Options{}); err != nil {
		t.Fatal(err)
	}
	if *reads != 3 || knownHas(w, file) {
		t.Fatalf("a deleted session: reads %d, still known %v", *reads, knownHas(w, file))
	}
}

// Another process (the CLI) writes the same database through its own
// store; its rows reach the watcher at the next full pass.
func TestWatcherSeesAnotherStoresSessionAtTheVerifyPass(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	other, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	w := NewWatcher()
	if _, err := w.Tick(st, Options{}); err != nil {
		t.Fatal(err)
	}
	file := "/gone/elsewhere/chat_history.jsonl"
	if err := other.UpsertSession(store.Session{JSONL: file, SessionID: "g2", Harness: "grok", Project: "jay/api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(st, Options{}); err != nil {
		t.Fatal(err)
	}
	if knownHas(w, file) {
		t.Fatal("expected the cached list until the verify pass")
	}
	w.lastVerify = time.Now().Add(-verifyEvery)
	if _, err := w.Tick(st, Options{}); err != nil {
		t.Fatal(err)
	}
	if !knownHas(w, file) {
		t.Fatal("the verify pass did not reload the session list")
	}
}
