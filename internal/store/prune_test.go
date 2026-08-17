package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfinedPathRefusesEscapeAndSymlink(t *testing.T) {
	root := t.TempDir()
	if _, err := confinedPath(root, true, ".."); err == nil {
		t.Fatal("..")
	}
	if _, err := confinedPath(root, true, "raw", "acme__api/../../etc"); err == nil {
		t.Fatal("slash")
	}
	dest := filepath.Join(root, "raw", "acme__api")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := confinedPath(root, true, "raw", "acme__api")
	if err != nil || got != dest && filepath.Clean(got) != filepath.Clean(dest) {
		t.Fatalf("%q %v", got, err)
	}
	link := filepath.Join(root, "raw", "linkout")
	if err := os.Symlink("/tmp", link); err != nil {
		t.Fatal(err)
	}
	if _, err := confinedPath(root, true, "raw", "linkout"); err == nil {
		t.Fatal("symlink")
	}
}
