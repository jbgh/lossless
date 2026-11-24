package main

import (
	"strings"
	"testing"

	"lossless/internal/inspect"
	"lossless/internal/store"
)

func TestSplitDetail(t *testing.T) {
	if got := splitDetail(nil); !strings.HasPrefix(got, "no repository is split") {
		t.Fatalf("no suggestions: %q", got)
	}
	list := []inspect.MergeSuggestion{
		{From: "acme/old", Into: "acme/new", Active: 65},
		{From: "path-1", Into: "acme/new", Active: 3},
		{From: "path-2", Into: "acme/api", Active: 2},
		{From: "path-3", Into: "acme/api", Active: 1},
	}
	got := splitDetail(list)
	for _, want := range []string{"4 split", "acme/old → acme/new, 65 records", "and 1 more", "lossless merge-project"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "path-3") {
		t.Fatalf("the fourth key should be folded into the count: %q", got)
	}
}

func TestMergeProjectArguments(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "")
	t.Setenv("LOSSLESS_SIDECAR", "off")
	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := mustClaim(t, st, "2026100100000000000000000000aa", "decision", "Shops close at dusk.")
	r.ProjectKey = "acme/old"
	r.ClaimHash = ""
	if _, err := st.WriteClaim(r); err != nil {
		t.Fatal(err)
	}
	under := func(project string) int {
		t.Helper()
		list, err := st.ListActive(project)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	// One key and a flag is a usage error, not a move into "--dry-run".
	for _, args := range [][]string{
		{"acme/old", "--dry-run"},
		{"acme/old"},
		{"--", "acme/old", "--dry-run"},
		{"acme/old", "acme/new", "acme/extra"},
	} {
		if code := runMergeProject(append([]string{"--home", home}, args...)); code != 2 {
			t.Fatalf("%v: exit %d, want 2", args, code)
		}
	}
	// The flag is read after the keys too, and changes nothing.
	if code := runMergeProject([]string{"--home", home, "acme/old", "acme/new", "--dry-run"}); code != 0 {
		t.Fatalf("dry run exit %d", code)
	}
	if under("acme/old") != 1 || under("acme/new") != 0 {
		t.Fatal("dry run moved a record")
	}
	// A key nothing is stored under is an error.
	if code := runMergeProject([]string{"--home", home, "acme/typo", "acme/new"}); code != 1 {
		t.Fatalf("missing FROM exit %d, want 1", code)
	}
	if code := runMergeProject([]string{"--home", home, "acme/old", "acme/new"}); code != 0 {
		t.Fatalf("merge exit %d", code)
	}
	if under("acme/old") != 0 || under("acme/new") != 1 {
		t.Fatal("record not moved")
	}
}
