package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactWorkContextLastUserAndPaths(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chat_history.jsonl")
	body := strings.Join([]string{
		`{"type":"user","content":"add billing export in src/billing/export.ts"}`,
		`{"type":"assistant","content":"Stripe invoice webhook failed in src/billing/export.ts."}`,
		`{"type":"user","content":"the Redis limiter failed in src/middleware/auth.ts — keep it in-process"}`,
		`{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}`,
	}, "\n") + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	goal, paths := CompactWorkContext(p)
	if !strings.Contains(goal, "Redis limiter") {
		t.Fatalf("goal=%q", goal)
	}
	blob := strings.Join(paths, " ")
	if !strings.Contains(blob, "src/middleware/auth.ts") {
		t.Fatalf("paths=%v", paths)
	}
}

func TestCompactWorkContextSkipsOwnAskPayload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chat.jsonl")
	body := `{"type":"user","content":"fix Redis in src/middleware/auth.ts"}` + "\n" +
		`{"type":"assistant","content":"{\"context\":[],\"warnings\":[\"A prior attempt failed.\"]}"}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	goal, paths := CompactWorkContext(p)
	if !strings.Contains(goal, "Redis") {
		t.Fatalf("goal=%q", goal)
	}
	if strings.Contains(goal, "warnings") {
		t.Fatalf("used ask payload as goal: %q", goal)
	}
	if !strings.Contains(strings.Join(paths, " "), "auth.ts") {
		t.Fatalf("paths=%v", paths)
	}
}

func TestCompactWorkContextMissingFile(t *testing.T) {
	goal, paths := CompactWorkContext("/no/such/session.jsonl")
	if goal != "" || len(paths) != 0 {
		t.Fatalf("goal=%q paths=%v", goal, paths)
	}
}
