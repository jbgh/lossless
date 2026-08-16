package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/store"
)

func TestSealRawSessionEnd(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s-seal", Source: "session_end"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sealed == "" {
		t.Fatal("expected sealed path")
	}
	if _, err := os.Stat(res.RawPath); !os.IsNotExist(err) {
		t.Fatal("uncompressed should be gone")
	}
	if _, err := os.Stat(res.Sealed); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRaw(res.RawPath)
	if err != nil || !strings.Contains(string(got), "jose") {
		t.Fatalf("decompress: %s %v", got, err)
	}
}

func TestResumeAfterSealWritesPart2(t *testing.T) {
	st := tmpStore(t)
	dir := t.TempDir()
	src := writeJSONL(t, dir, "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	first, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s-part", Source: "session_end"})
	if err != nil {
		t.Fatal(err)
	}
	zst := first.Sealed
	zstInfo, err := os.Stat(zst)
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"assistant","content":"Redis token bucket failed in staging yesterday."}` + "\n")
	_ = f.Close()

	second, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s-part", Source: "turn"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second.RawPath, ".part2.jsonl") {
		t.Fatalf("expected part2, got %s", second.RawPath)
	}
	if _, err := os.Stat(second.RawPath); err != nil {
		t.Fatal("part2 missing")
	}
	after, _ := os.Stat(zst)
	if after.Size() != zstInfo.Size() {
		t.Fatal("zst mutated")
	}
	old, err := ReadRaw(strings.TrimSuffix(zst, ".zst"))
	if err != nil || !strings.Contains(string(old), "jose") {
		t.Fatal(string(old), err)
	}
	part2, err := os.ReadFile(second.RawPath)
	if err != nil || !strings.Contains(string(part2), "Redis") {
		t.Fatal(string(part2), err)
	}
	if strings.Contains(string(part2), "jose") {
		t.Fatal("part2 should only have new bytes")
	}
}

func TestSealRawIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")
	if err := os.WriteFile(p, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := SealRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SealRaw(p)
	if err != nil || b != a {
		t.Fatal(b, err)
	}
	c, err := SealRaw(a)
	if err != nil || c != a {
		t.Fatal(c, err)
	}
}

func TestLiveRawPathAfterSeal(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	when := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	live := st.LiveRawPath("acme/api", "sid", when)
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SealRaw(live); err != nil {
		t.Fatal(err)
	}
	next := st.LiveRawPath("acme/api", "sid", when)
	if !strings.Contains(next, ".part2.jsonl") {
		t.Fatal(next)
	}
}
