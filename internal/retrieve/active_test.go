package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/write"
)

func TestCompactSource(t *testing.T) {
	if !CompactSource("PreCompact") || !CompactSource("session.compacting") || !CompactSource("session_before_compact") {
		t.Fatal("compact")
	}
	if CompactSource("PostCompact") || CompactSource("session.compacted") {
		t.Fatal("after compact")
	}
	if CompactSource("turn") || CompactSource("Stop") || CompactSource("session_end") || CompactSource("") {
		t.Fatal("not compact")
	}
}

func TestWriteActiveFile(t *testing.T) {
	home := t.TempDir()
	out := Response{
		Project:  "acme/api",
		Warnings: []string{"A prior attempt failed (see 01J)."},
		Context: []Hit{{
			ID:         "01JFAIL",
			Type:       "failed",
			Text:       "Redis token bucket failed in src/middleware/auth.ts staging.",
			HasExcerpt: true,
		}},
	}
	if err := WriteActive(home, out, time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(home, "active", "acme__api.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "project: acme/api") || !strings.Contains(s, "Redis") || !strings.Contains(s, "warnings") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "`01JFAIL`") || !strings.Contains(s, "has_excerpt=true") {
		t.Fatalf("not a bibliography: %s", s)
	}
	if !strings.Contains(s, "> Redis") {
		t.Fatal("cite must stay blockquoted so extract SkipProse")
	}
}

func TestWriteActiveSkipsEmpty(t *testing.T) {
	home := t.TempDir()
	if err := WriteActive(home, Response{Project: "acme/api"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "active", "acme__api.md")); !os.IsNotExist(err) {
		t.Fatal("empty checkout")
	}
}

func TestRefreshActiveOnlyOnCompact(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JFAIL", Type: "failed",
		Text:  "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths: []string{"src/middleware/auth.ts"},
	})
	home := st.Root
	RefreshActive(st, home, write.CatchUpRequest{
		Project: "acme/api", Source: "turn",
	}, "")
	if _, err := os.Stat(filepath.Join(home, "active", "acme__api.md")); !os.IsNotExist(err) {
		t.Fatal("turn wrote active")
	}
	RefreshActive(st, home, write.CatchUpRequest{
		Project: "acme/api", Source: "PreCompact",
	}, "")
	body, err := os.ReadFile(filepath.Join(home, "active", "acme__api.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Redis") {
		t.Fatal(string(body))
	}
}

func TestRefreshActiveHotAskUsesTapeTail(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JFAIL", Type: "failed",
		Text:  "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths: []string{"src/middleware/auth.ts"},
	})
	writeRec(t, st, claim.Record{
		ID: "01JBILL", Type: "failed",
		Text:  "Stripe invoice webhook failed in src/billing/export.ts.",
		Paths: []string{"src/billing/export.ts"},
	})
	ws := t.TempDir()
	jsonl := writeJSONL(t, ws, "chat_history.jsonl",
		`{"type":"user","content":"the Redis limiter failed in src/middleware/auth.ts — keep it in-process"}`+"\n"+
			`{"type":"assistant","content":"Use limiter, not Redis, in src/middleware/auth.ts."}`+"\n")
	req := write.CatchUpRequest{
		JSONL: jsonl, Project: "acme/api", WorkspaceRoot: ws,
		Harness: "grok", SessionID: "s-compact", Source: "PreCompact",
	}
	res, err := write.CatchUp(st, req)
	if err != nil || res.RawPath == "" {
		t.Fatalf("catch-up: %+v %v", res, err)
	}
	// Harness file may shrink or become a summary. Hot ask still uses owned raw.
	if err := os.WriteFile(jsonl, []byte(`{"type":"assistant","content":"Conversation compacted."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := st.Root
	RefreshActive(st, home, req, res.RawPath)
	body, err := os.ReadFile(filepath.Join(home, "active", "acme__api.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "Redis") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "`01JFAIL`") {
		t.Fatalf("missing cite id: %s", s)
	}
	goal, paths := write.CompactWorkContext(res.RawPath)
	if !strings.Contains(goal, "Redis") {
		t.Fatalf("owned raw goal=%q", goal)
	}
	if !strings.Contains(strings.Join(paths, " "), "auth.ts") {
		t.Fatalf("owned raw paths=%v", paths)
	}
	if g, _ := write.CompactWorkContext(jsonl); strings.Contains(g, "Redis") {
		t.Fatal("harness file still had the user line")
	}
}
