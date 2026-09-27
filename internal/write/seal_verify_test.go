package write

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A seal is only a copy the store can still read: the .zst must
// decompress to the same bytes, and the plaintext is gone only after
// the rename and the directory fsync.
func TestSealRawRoundTripsPlaintext(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tape.jsonl")
	var body strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&body, `{"type":"message","role":"user","content":"turn %d"}`+"\n", i)
	}
	if err := os.WriteFile(p, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	zst, err := SealRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("plaintext should be gone")
	}
	if _, err := os.Stat(zst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(zst + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("tmp file left behind")
	}
	got, err := ReadRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(body.String())) {
		t.Fatalf("decompressed %d bytes, want %d", len(got), body.Len())
	}
}

// A .zst sibling already holds an earlier seal. Overwriting it would drop
// tape: remember.jsonl has no .partN rollover, so its next seal would
// replace the .zst written by the last one.
func TestSealRawRefusesToClobberExistingZst(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "remember.jsonl")
	if err := os.WriteFile(p, []byte("new line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SealRaw(p); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(p + ".zst")
	if err != nil {
		t.Fatal(err)
	}
	// Remembering again after the seal reopens the same plain path.
	if err := os.WriteFile(p, []byte("new line\nlater line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = SealRaw(p)
	if !errors.Is(err, ErrAlreadySealed) {
		t.Fatalf("want ErrAlreadySealed, got %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("plaintext must survive a refused seal")
	}
	after, err := os.ReadFile(p + ".zst")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, after) {
		t.Fatal("earlier .zst was overwritten")
	}
}

// A failed seal deletes nothing: the plaintext is the only copy until
// the .zst is verified and renamed.
func TestSealRawKeepsPlaintextWhenTheCopyFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tape.jsonl")
	body := []byte("only copy\n")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory where the tmp file goes makes the copy fail.
	if err := os.Mkdir(p+".zst.tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := SealRaw(p); err == nil {
		t.Fatal("expected a copy failure")
	}
	got, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal(string(got), err)
	}
}

// verifySeal is what stands between a bad compress and a deleted tape:
// a digest that does not match the plaintext it was written from is a
// failure, and the caller keeps both files.
func TestVerifySealRejectsADigestMismatch(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "tape.jsonl")
	if err := os.WriteFile(plain, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	zst, err := SealRaw(plain)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("hello\n"))
	if err := verifySeal(zst, want[:]); err != nil {
		t.Fatal(err)
	}
	other := sha256.Sum256([]byte("something else\n"))
	if err := verifySeal(zst, other[:]); err == nil {
		t.Fatal("a digest mismatch must fail the verify")
	}
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatal("a verify failure never happens after the unlink")
	}
}

// A seal is idempotent on an already-sealed path and on a plaintext that
// is already gone, so the watcher and the sweep can both call it.
func TestSealRawAlreadySealedAndMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tape.jsonl")
	if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	zst, err := SealRaw(p)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := SealRaw(p); err != nil || again != zst {
		t.Fatal(again, err)
	}
	if again, err := SealRaw(zst); err != nil || again != zst {
		t.Fatal(again, err)
	}
	if _, err := SealRaw(filepath.Join(dir, "never-existed.jsonl")); err == nil {
		t.Fatal("a missing tape with no .zst is an error")
	}
}
