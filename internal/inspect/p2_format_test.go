package inspect

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/store"
)

// A project with 130 caught-up Grok sessions printed 130 identical "ok"
// lines and buried the one row that mattered. Sessions that are current
// collapse into a count; only behind / missing / no-cursor rows print.
func TestFormatCollapsesCurrentSessions(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := t.TempDir()
	mk := func(name string, cursor int64) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.SetCursor(p, cursor); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok1 := mk("a.jsonl", 6)
	ok2 := mk("b.jsonl", 6)
	ok3 := mk("c.jsonl", 6)
	behind := mk("d.jsonl", 2)
	for i, p := range []string{ok1, ok2, ok3} {
		if err := st.UpsertSession(store.Session{JSONL: p, SessionID: "ok" + string(rune('1'+i)), Harness: "grok", Project: "acme/api"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertSession(store.Session{JSONL: behind, SessionID: "lag", Harness: "claude", Project: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	rep, err := Build(st, "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	Format(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "sessions  3 ok") {
		t.Fatalf("missing collapsed count:\n%s", out)
	}
	if strings.Count(out, "  session  ") != 1 || !strings.Contains(out, "  session  claude  lag  d.jsonl  behind") {
		t.Fatalf("only the behind session should print:\n%s", out)
	}
}
