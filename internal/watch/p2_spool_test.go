package watch

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/store"
	"lossless/internal/write"
)

// 74 hook spool jobs sat under ~/.lossless/spool for three weeks because
// only the `ensure` CLI ever replayed them. The watcher tick replays the
// spool while the daemon is up.
func TestTickReplaysSpool(t *testing.T) {
	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	src := filepath.Join(t.TempDir(), "chat_history.jsonl")
	if err := os.WriteFile(src, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, err := write.WriteSpool(home, write.SpoolJob{JSONL: src, Project: "acme/api", Harness: "grok", SessionID: "s1", Source: "turn"})
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		GrokRoot: t.TempDir(), ClaudeRoot: t.TempDir(), CodexRoot: t.TempDir(), PiRoot: t.TempDir(),
	}
	if _, err := Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(job); !os.IsNotExist(err) {
		t.Fatal("spool job not consumed by the tick")
	}
	if _, ok := st.SessionByJSONL(src); !ok {
		t.Fatal("spooled catch-up not ingested")
	}
}
