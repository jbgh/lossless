package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/store"
	"lossless/internal/write"
)

// idleSeal used to resolve the tape with time.Now(), so a session whose
// tape lives in an earlier month's folder was never found once the month
// rolled over. It must seal the tape the session actually wrote.
func TestIdleSealSealsAPreviousMonthTape(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// The source transcript stopped in August; the tape was written
	// there. The month boundary has since passed.
	aug := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "chat.jsonl")
	body := `{"type":"message","role":"user","content":"the august turn"}` + "\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, aug, aug); err != nil {
		t.Fatal(err)
	}
	tape := st.RawPath("acme/api", "sess-aug", aug)
	if err := os.MkdirAll(filepath.Dir(tape), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tape, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tape, aug, aug); err != nil {
		t.Fatal(err)
	}

	tg := Target{JSONL: src, Harness: "grok", SessionID: "sess-aug", Project: "acme/api"}
	if !idleSeal(st, tg, Options{IdleSeal: time.Hour}, nil) {
		t.Fatal("idleSeal did not seal the previous month's tape")
	}
	if _, err := os.Stat(tape); !os.IsNotExist(err) {
		t.Fatal("the august plaintext should be sealed")
	}
	if _, err := os.Stat(tape + ".zst"); err != nil {
		t.Fatal(err)
	}
	// And it still reads back.
	got, err := write.ReadRaw(tape)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "august turn") {
		t.Fatal(string(got))
	}
}

// A tape inside the idle window is still left alone in the older month.
func TestIdleSealLeavesARecentPreviousMonthTape(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	aug := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	tape := st.RawPath("acme/api", "sess-aug", aug)
	if err := os.MkdirAll(filepath.Dir(tape), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tape, []byte("recent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Source mtime points at the month the tape is in.
	src := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(src, []byte("recent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	tg := Target{JSONL: src, Harness: "grok", SessionID: "sess-aug", Project: "acme/api"}
	if idleSeal(st, tg, Options{IdleSeal: 30 * 24 * time.Hour}, nil) {
		t.Fatal("a tape inside the idle window must not be sealed")
	}
	if _, err := os.Stat(tape); err != nil {
		t.Fatal(err)
	}
}
