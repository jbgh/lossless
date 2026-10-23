package watch

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lossless/internal/harness"
)

const codexThreadsSchema = `
CREATE TABLE threads (
  id TEXT PRIMARY KEY,
  rollout_path TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  cwd TEXT NOT NULL,
  first_user_message TEXT NOT NULL DEFAULT ''
);`

// countCodexReads swaps the Codex state reader for a counting wrapper.
func countCodexReads(t *testing.T) *int {
	t.Helper()
	n := new(int)
	real := codexStateRows
	codexStateRows = func(home string) []harness.CodexThread { *n++; return real(home) }
	t.Cleanup(func() { codexStateRows = real })
	return n
}

// An unchanged Codex state database is not opened again on every tick (it
// was: sql.Open and a full query each tick, on a database idle for months).
// A write to its WAL is read on the next tick; a rollout file that appears
// after its thread row becomes a JSONL target without a database change.
func TestWatcherCachesCodexStateRows(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	// Outside sessions/, so only the state row can make it a target.
	rollout := filepath.Join(home, "archived", "rollout-2026-10-23T00-00-00-thread-a.jsonl")
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(codexThreadsSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES('thread-a', ?, 5, '/Users/jay/dev/api', 'Use jose.')`, rollout); err != nil {
		t.Fatal(err)
	}
	wal := dbPath + "-wal"
	agePresent := func() {
		for _, p := range []string{dbPath, wal} {
			if _, err := os.Stat(p); err == nil {
				age(t, p)
			}
		}
	}
	agePresent()
	reads := countCodexReads(t)
	opts := Options{CodexRoot: filepath.Join(home, "sessions")}
	w := NewWatcher()
	discover := func() []Target {
		w.beginTick(time.Now())
		defer w.endTick()
		return w.discover(opts, nil)
	}
	for i := 0; i < 3; i++ {
		got := discover()
		if len(got) != 1 || got[0].JSONL != "" || got[0].SessionID != "thread-a" {
			t.Fatalf("tick %d: %+v", i, got)
		}
	}
	if *reads != 1 {
		t.Fatalf("unchanged state db read %d times over three ticks, want 1", *reads)
	}

	if _, err := db.Exec(`INSERT INTO threads VALUES('thread-b', '', 6, '/Users/jay/dev/web', 'Drop the CDN.')`); err != nil {
		t.Fatal(err)
	}
	agePresent()
	if got := discover(); len(got) != 2 {
		t.Fatalf("after a WAL write: %+v", got)
	}
	if *reads != 2 {
		t.Fatalf("a WAL write was not reread: %d reads", *reads)
	}

	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollout, []byte(`{"type":"session_meta","payload":{"id":"thread-a","cwd":"/Users/jay/dev/api"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, tg := range discover() {
		if tg.JSONL == rollout && tg.SessionID == "thread-a" {
			found = true
		}
	}
	if !found {
		t.Fatal("a rollout file that appeared later is not a JSONL target")
	}
	if *reads != 2 {
		t.Fatalf("the rollout check reopened the db: %d reads", *reads)
	}
}
