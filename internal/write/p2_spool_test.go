package write

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 1,088 virtual-*.jsonl staging files from one day in August sat in the
// spool for three weeks. Ensure sweeps staging files older than a day;
// a fresh one (a POST in flight) stays.
func TestEnsureSweepsStaleVirtualSpool(t *testing.T) {
	st := tmpStore(t)
	dir := SpoolDir(st.Root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "virtual-ses_old.jsonl")
	fresh := filepath.Join(dir, "virtual-ses_new.jsonl")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(st, st.Root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("stale staging file kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh staging file removed")
	}
}

// A job whose file is gone, or that names Grok's updates.jsonl, can never
// replay. Ensure drops it instead of retrying on every watcher tick.
func TestEnsureDropsDeadJobs(t *testing.T) {
	st := tmpStore(t)
	gone, err := WriteSpool(st.Root, SpoolJob{JSONL: filepath.Join(t.TempDir(), "missing.jsonl"), Project: "acme/api", Harness: "grok", SessionID: "s1", Source: "turn"})
	if err != nil {
		t.Fatal(err)
	}
	updDir := filepath.Join(t.TempDir(), ".grok", "sessions", "x", "s2")
	if err := os.MkdirAll(updDir, 0o755); err != nil {
		t.Fatal(err)
	}
	upd := filepath.Join(updDir, "updates.jsonl")
	if err := os.WriteFile(upd, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stream, err := WriteSpool(st.Root, SpoolJob{JSONL: upd, Project: "acme/api", Harness: "claude", SessionID: "s2", Source: "turn"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := Ensure(st, st.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{gone, stream} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("dead job kept: %s (%+v)", filepath.Base(p), res)
		}
	}
	if res.Skipped != 2 || res.Failed != 0 {
		t.Fatalf("%+v", res)
	}
}
