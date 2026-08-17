package write

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckIngestFileRejectsNonJSONL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "passwd")
	if err := os.WriteFile(p, []byte("root:x:0:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkIngestFile(p); err == nil {
		t.Fatal("expected reject")
	}
	if err := checkIngestFile("/etc/passwd"); err == nil {
		t.Fatal("etc/passwd")
	}
}

func TestCheckIngestFileAcceptsJSONL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkIngestFile(p); err != nil {
		t.Fatal(err)
	}
}

func TestCatchUpRejectsNonJSONLPath(t *testing.T) {
	st := tmpStore(t)
	p := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(p, []byte("-----BEGIN RSA PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "x"}); err == nil {
		t.Fatal("expected reject")
	}
}

func TestCheckIngestFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secret")
	if err := os.WriteFile(target, []byte("root:x:0:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "chat.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := checkIngestFile(link); err == nil {
		t.Fatal("expected symlink reject")
	}
	st := tmpStore(t)
	if _, err := CatchUp(st, CatchUpRequest{JSONL: link, Project: "acme/api", SessionID: "x"}); err == nil {
		t.Fatal("catch-up must refuse symlink")
	}
}
