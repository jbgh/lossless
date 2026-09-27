package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A bench run is reproducible, so it must leave nothing in -home. Every
// run used to add a TestBench*000 dir that nothing ever removed; the
// live home accumulated them (15 of them, ~35 MB, on 2026-09-25).
func TestBenchLeavesNothingInHome(t *testing.T) {
	home := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "bench"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Skipf("bench fixtures missing: %v", err)
	}
	runBench([]string{"-home", home, "-root", root})
	ents, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "TestBench") {
			t.Errorf("bench left %q behind in -home", e.Name())
		}
	}
	if len(ents) != 0 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("bench left %v in -home", names)
	}
}
