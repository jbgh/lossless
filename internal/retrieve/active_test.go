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
	if !CompactSource("PreCompact") || !CompactSource("session.compacting") {
		t.Fatal("compact")
	}
	if CompactSource("turn") || CompactSource("Stop") || CompactSource("session_end") {
		t.Fatal("not compact")
	}
}

func TestWriteActiveFile(t *testing.T) {
	home := t.TempDir()
	out := Response{
		Project:  "acme/api",
		Warnings: []string{"A prior attempt failed (see 01J)."},
		Context: []Hit{{
			Type: "failed",
			Text: "Redis token bucket failed in src/middleware/auth.ts staging.",
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
	if !strings.Contains(s, "project: acme/api") || !strings.Contains(s, "> Redis") || !strings.Contains(s, "warnings") {
		t.Fatal(s)
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
	})
	if _, err := os.Stat(filepath.Join(home, "active", "acme__api.md")); !os.IsNotExist(err) {
		t.Fatal("turn wrote active")
	}
	RefreshActive(st, home, write.CatchUpRequest{
		Project: "acme/api", Source: "PreCompact",
	})
	body, err := os.ReadFile(filepath.Join(home, "active", "acme__api.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Redis") {
		t.Fatal(string(body))
	}
}
