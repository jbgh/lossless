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
	q, err = normalize(Request{Project: "acme/api", Question: "JWT library choice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range q.Symbols {
		if s == "library" || s == "choice" {
			t.Fatalf("english leftover leaked into symbols: %v", q.Symbols)
		}
	}
	foundJWT := false
	for _, s := range q.Symbols {
		if s == "jwt" || s == "jsonwebtoken" {
			foundJWT = true
		}
	}
	if !foundJWT {
		t.Fatalf("jwt should remain a symbol: %v", q.Symbols)
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

func TestExtractNoiseAndJobOverlap(t *testing.T) {
	if !extractNoise(claim.Record{Text: "| **Claims** | owner/repo | A Grok failed |"}) {
		t.Fatal("table")
	}
	if !extractNoise(claim.Record{Text: "Force in the best failed-overlap now."}) {
		t.Fatal("failed-overlap")
	}
	if !extractNoise(claim.Record{Type: "state", Text: "Next I'll check the tape after compact."}) {
		t.Fatal("next I'll")
	}
	if extractNoise(claim.Record{Type: "failed", Text: "Redis token bucket failed in staging.", Paths: []string{"src/middleware/auth.ts"}}) {
		t.Fatal("real failed")
	}
	if !extractNoise(claim.Record{Type: "failed", Text: "The live ask just returned five `failed`s, and four look like extract noise."}) {
		t.Fatal("backticked type mention")
	}
	qtoks := []string{"retrieve", "extract", "failed", "noise", "pack"}
	if contentOverlap(qtoks, jobOverlapText("Raising to 8 would make recall look better — extract noise classified as `failed`.")) >= OverlapStrongMin {
		t.Fatal("meta should not be strong job overlap")
	}
	if contentOverlap([]string{"redis", "limiter", "auth"}, jobOverlapText("Redis token bucket failed in src/middleware/auth.ts staging.")) < 1 {
		t.Fatal("real failed should still overlap redis")
	}
}
