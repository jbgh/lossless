package inspect

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// ~/.grok/sessions was emptied on 2026-09-15: 1,641 Grok cursors then read
// "missing" and inspect printed one row per session (125 on this repo,
// 1,516 on memora) although raw/ keeps every byte of that tape. A source
// the harness cleaned up needs no operator: it collapses into a count.
func TestFormatCollapsesGoneSources(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := t.TempDir()
	mk := func(name, sid, harness string, cursor int64, remove bool) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.SetCursor(p, cursor); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertSession(store.Session{JSONL: p, SessionID: sid, Harness: harness, Project: "acme/api"}); err != nil {
			t.Fatal(err)
		}
		if remove {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("a.jsonl", "ok1", "claude", 6, false)
	mk("b.jsonl", "ok2", "claude", 6, false)
	mk("c.jsonl", "lag", "claude", 2, false)
	mk("g1.jsonl", "gone1", "grok", 6, true)
	mk("g2.jsonl", "gone2", "grok", 6, true)
	mk("g3.jsonl", "gone3", "grok", 4, true)

	rep, err := Build(st, "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	Format(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "sessions  2 ok  (claude 2)") {
		t.Fatalf("ok count:\n%s", out)
	}
	if !strings.Contains(out, "sessions  3 source gone, tape kept  (grok 3)") {
		t.Fatalf("gone sources must collapse into one count:\n%s", out)
	}
	if strings.Count(out, "  session  ") != 1 || !strings.Contains(out, "  session  claude  lag  c.jsonl  behind") {
		t.Fatalf("only the behind session should print:\n%s", out)
	}

	all, err := Build(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := all.CursorNote["acme/api"]; got != "1 behind 2 ok 3 gone" {
		t.Fatalf("cursor note %q", got)
	}
	joined := strings.Join(all.Notes, "\n")
	if !strings.Contains(joined, "3 sessions whose source file is no longer on disk") {
		t.Fatalf("health note missing: %q", joined)
	}
}

// A folder without a git origin whose every source is gone can never grow
// again. On the live store 150-odd of them printed a row each.
func TestOverviewFoldsPathHashProjectsWithNoLiveSource(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := t.TempDir()
	seed := func(project, name string, remove bool) {
		for _, txt := range []string{"Use jose, not jsonwebtoken, for Edge.", "Redis limiter blew the p95 budget.", "Pin zod to 3.23 for the parser."} {
			if _, err := st.WriteClaim(claim.Record{Type: "decision", Text: txt + " " + project, ProjectKey: project}); err != nil {
				t.Fatal(err)
			}
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.SetCursor(p, 3); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertSession(store.Session{JSONL: p, SessionID: name, Harness: "grok", Project: project}); err != nil {
			t.Fatal(err)
		}
		if remove {
			_ = os.Remove(p)
		}
	}
	seed("path-00000000deadbeef", "dead.jsonl", true)
	seed("path-00000000feedface", "live.jsonl", false)
	rep, err := Build(st, "")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	Format(&buf, rep)
	out := buf.String()
	if strings.Contains(out, "path-00000000deadbeef") {
		t.Fatalf("a path-hash project with no live source must fold into the path-* line:\n%s", out)
	}
	if !strings.Contains(out, "path-00000000feedface") {
		t.Fatalf("a path-hash project with a live source still prints:\n%s", out)
	}
	if !strings.Contains(out, "path-* × 1") {
		t.Fatalf("folded line:\n%s", out)
	}
}
