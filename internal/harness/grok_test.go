package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeURIComponent(t *testing.T) {
	got := encodeURIComponent("/Users/jay/dev/api")
	if got != "%2FUsers%2Fjay%2Fdev%2Fapi" {
		t.Fatalf("got %q", got)
	}
	if encodeURIComponent("AZaz09-_.~") != "AZaz09-_.~" {
		t.Fatal("unreserved")
	}
	if encodeURIComponent(" ") != "%20" {
		t.Fatal("space")
	}
}

func TestLocateGrok(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GROK_HOME", root)
	ws := "/tmp/acme-api"
	sid := "sess-1"
	enc := encodeURIComponent(ws)
	dir := filepath.Join(root, "sessions", enc, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(dir, "chat_history.jsonl")
	missing := LocateGrok(ws, sid)
	if missing.JSONL != hist || missing.SessionID != sid || missing.CWD != ws {
		t.Fatalf("missing locate: %+v", missing)
	}
	if fileExists(hist) {
		t.Fatal("should not exist yet")
	}
	if fileExists(dir) {
		t.Fatal("dir is not a file")
	}
	if err := os.WriteFile(hist, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := LocateGrok(ws, sid)
	if found.JSONL != hist {
		t.Fatalf("found %q", found.JSONL)
	}
	if !fileExists(hist) {
		t.Fatal("exists")
	}
}

func TestLocateGrokDefaultHome(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	t.Setenv("HOME", t.TempDir())
	loc := LocateGrok("/ws", "s")
	if loc.JSONL == "" || loc.SessionID != "s" {
		t.Fatalf("%+v", loc)
	}
}
