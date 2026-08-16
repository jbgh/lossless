package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCasesMissingDir(t *testing.T) {
	cs, err := LoadCases(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 0 {
		t.Fatal(cs)
	}
}

func TestLoadCasesBadJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cases", "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCases(root); err == nil {
		t.Fatal("want error")
	}
}

func TestRunDirIsolatesStores(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "bench")
	if _, err := os.Stat(filepath.Join(root, "cases")); err != nil {
		t.Skip(err)
	}
	rep, err := RunDir(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rep.CasePass != rep.CaseTotal || rep.AskPass != rep.AskTotal {
		t.Fatal(FormatReport(rep))
	}
}
