package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

func testdata(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "testdata", "sim", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatal(p, err)
	}
	return p
}

func TestGrokThenClaudeCrossModel(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	grok, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: testdata(t, "grok-auth.jsonl"), Project: "acme/api",
		Harness: "grok", SessionID: "grok-auth", Source: "compact",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if grok.Extracted < 2 {
		t.Fatalf("grok extract too thin: %+v", grok)
	}

	eng := retrieve.Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	}}

	// New Claude, empty window, no question. Compile from Grok raw tail.
	cold, err := eng.Ask(retrieve.Request{Project: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	blob := textsOfHits(cold)
	if !strings.Contains(blob, "Redis") && !strings.Contains(blob, "jose") {
		t.Fatalf("thin ask missed grok work: extracted=%d packet=%+v", grok.Extracted, cold)
	}

	// Claude starts implementing. Rich ask with the new goal.
	hot, err := eng.Ask(retrieve.Request{
		Project:  "acme/api",
		Question: "add rate limiting and pick a JWT library",
		Goal:     "add rate limiting",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hotText := textsOfHits(hot)
	if !strings.Contains(hotText, "Redis") {
		t.Fatalf("claude hot ask missed redis failed: %+v", hot)
	}
	if !strings.Contains(hotText, "jose") {
		t.Fatalf("claude hot ask missed jose decision: %+v", hot)
	}
	if !hasAnyWarn(hot, "failed") {
		t.Fatalf("expected failed warning: %v", hot.Warnings)
	}

	// Ingest the Claude session too. Same project, different harness.
	claude, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: testdata(t, "claude-jwt.jsonl"), Project: "acme/api",
		Harness: "claude", SessionID: "claude-jwt", Source: "turn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if claude.Copied == 0 {
		t.Fatalf("claude catch-up copied nothing: %+v", claude)
	}

	again, err := eng.Ask(retrieve.Request{
		Project: "acme/api",
		Goal:    "add rate limiting",
		Paths:   []string{"src/middleware/auth.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textsOfHits(again), "Redis") {
		t.Fatalf("after claude ingest, redis gone: %+v", again)
	}
}

func textsOfHits(out retrieve.Response) string {
	var b strings.Builder
	for _, h := range out.Context {
		b.WriteString(h.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func hasAnyWarn(out retrieve.Response, needle string) bool {
	for _, w := range out.Warnings {
		if strings.Contains(strings.ToLower(w), needle) {
			return true
		}
	}
	return false
}
