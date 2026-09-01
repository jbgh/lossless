package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A session file past 64MB is capped per delta, not refused: a long
// Claude session must keep ingesting its complete lines.
func TestCatchUpIngestsFileOver64MB(t *testing.T) {
	st := tmpStore(t)
	ws := t.TempDir()
	p := filepath.Join(ws, "big.jsonl")
	head := strings.Repeat(`{"type":"assistant","content":"Redis token bucket failed in src/middleware/auth.ts staging."}`+"\n", 3)
	if err := os.WriteFile(p, []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(p, maxCatchUpBytes+1); err != nil {
		t.Fatal(err)
	}
	out, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", Harness: "grok", SessionID: "big", Source: "turn"})
	if err != nil {
		t.Fatalf("oversized file refused: %v", err)
	}
	if out.Copied == 0 {
		t.Fatalf("nothing copied from the complete-line prefix: %+v", out)
	}
}
