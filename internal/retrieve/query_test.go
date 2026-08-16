package retrieve

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"lossless/internal/claim"
)

func TestNormalizeAndHelpers(t *testing.T) {
	q, err := normalize(Request{
		Project:     "Acme/API",
		Question:    `why not "jsonwebtoken"?`,
		Goal:        "pick a jwt library",
		Paths:       []string{"src/middleware/auth.ts"},
		LimitTokens: 0,
	})
	if err != nil || q.ProjectKey != "acme/api" || q.Cold || q.LimitTokens != DefaultLimit {
		t.Fatalf("%+v %v", q, err)
	}
	if len(q.PathKeys) < 2 || len(q.Symbols) == 0 || len(q.LookupTokens) == 0 {
		t.Fatalf("paths/symbols/lookup: %+v", q)
	}
	if _, err := normalize(Request{}); err == nil {
		t.Fatal("bad request")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	cmd = exec.Command("git", "remote", "add", "origin", "https://github.com/Acme/API.git")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	q, err = normalize(Request{WorkspaceRoot: dir})
	if err != nil || q.ProjectKey != "acme/api" || !q.Cold {
		t.Fatalf("%+v %v", q, err)
	}

	if identLower("ab") || identLower("1abc") || identLower("ab-c") {
		t.Fatal("identLower false")
	}
	if !identLower("jose") || !identLower("_id") {
		t.Fatal("identLower true")
	}
	if ftsMatch([]string{`"`, "jose", "  "}) != `"jose"` {
		t.Fatal(ftsMatch([]string{`"`, "jose", "  "}))
	}
	if jaccard(nil, []string{"a"}) != 0 || jaccard([]string{""}, []string{""}) != 0 {
		t.Fatal("jaccard empty")
	}
	if jaccard([]string{"a", "b"}, []string{"b", "c"}) != 1.0/3.0 {
		t.Fatal(jaccard([]string{"a", "b"}, []string{"b", "c"}))
	}
	if estimateTokens("") != 1 || estimateTokens("abcd") != 1 || estimateTokens("abcde") != 2 {
		t.Fatal("tokens")
	}
	if uniq([]string{"", "a", "a", "b"})[0] != "a" {
		t.Fatal(uniq([]string{"", "a", "a", "b"}))
	}
	_ = os.Remove(filepath.Join(dir, "nope"))
}

func TestMustJSONAndToHit(t *testing.T) {
	if mustJSON(make(chan int)) != "" {
		t.Fatal("chan should fail marshal")
	}
	h := toHit(scored{rec: claim.Record{ID: "I", Type: "decision", Text: "t"}}, false)
	if h.Paths == nil {
		t.Fatal("nil paths")
	}
}
