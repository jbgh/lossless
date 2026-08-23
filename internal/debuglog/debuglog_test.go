package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendTailAndIdentity(t *testing.T) {
	home := t.TempDir()
	Append(home, Event{Kind: "ask", Project: "memora/memora", Identity: Identity("", "memora/memora"), Hits: 5})
	Append(home, Event{Kind: "ask", Project: "jbgh/lossless", Identity: Identity("", "path-abc"), Hits: 0})
	got := Tail(home, 8, "memora/memora")
	if len(got) != 1 || got[0].Hits != 5 || got[0].Identity != "origin" {
		t.Fatalf("%+v", got)
	}
	if Identity("memora/memora", "path-x") != "given" {
		t.Fatal("given")
	}
	if Identity("", "path-90f6202b6bcfba05") != "path" {
		t.Fatal("path")
	}
	p := Path(home)
	if !strings.HasSuffix(p, "debug/events.jsonl") {
		t.Fatal(p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

func TestRotate(t *testing.T) {
	home := t.TempDir()
	p := path(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, maxBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	Append(home, Event{Kind: "ask", Project: "acme/api"})
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatal("expected rotate")
	}
	body, _ := os.ReadFile(p)
	if !strings.Contains(string(body), "acme/api") {
		t.Fatalf("%s", body)
	}
}
