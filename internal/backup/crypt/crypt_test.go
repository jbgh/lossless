// internal/backup/crypt/crypt_test.go
package crypt

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

func testKeys(t *testing.T) *Keys {
	t.Helper()
	h, err := NewKeyHex()
	if err != nil || len(h) != 64 {
		t.Fatalf("NewKeyHex: %q %v", h, err)
	}
	k, err := ParseKeyHex(h + "\n")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestParseKeyHexRejectsBadInput(t *testing.T) {
	for _, bad := range []string{"", "abc", "zz" + string(make([]byte, 62))} {
		if _, err := ParseKeyHex(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestNameIsStableAndPathBound(t *testing.T) {
	k := testKeys(t)
	a, b := k.Name("raw/x/y.jsonl.zst"), k.Name("raw/x/y.jsonl.zst")
	if a != b || len(a) != 32 {
		t.Fatalf("%s %s", a, b)
	}
	if k.Name("raw/x/z.jsonl.zst") == a {
		t.Fatal("different paths must not collide")
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripSizes(t *testing.T) {
	k := testKeys(t)
	for _, n := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 7} {
		plain := make([]byte, n)
		_, _ = rand.Read(plain)
		var ct bytes.Buffer
		pSHA, cSHA, cLen, err := k.Encrypt(&ct, bytes.NewReader(plain), "p/a")
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if pSHA != sha(plain) || cSHA != sha(ct.Bytes()) || cLen != int64(ct.Len()) {
			t.Fatalf("n=%d: hashes or length wrong", n)
		}
		if cLen != CipherLen(int64(n)) {
			t.Fatalf("n=%d: CipherLen %d != %d", n, CipherLen(int64(n)), cLen)
		}
		var out bytes.Buffer
		got, err := k.Decrypt(&out, bytes.NewReader(ct.Bytes()), "p/a")
		if err != nil || got != pSHA || !bytes.Equal(out.Bytes(), plain) {
			t.Fatalf("n=%d: decrypt %v", n, err)
		}
	}
}

func encryptTwoChunks(t *testing.T, k *Keys) (plain, ct []byte) {
	t.Helper()
	plain = make([]byte, ChunkSize+100)
	_, _ = rand.Read(plain)
	var buf bytes.Buffer
	if _, _, _, err := k.Encrypt(&buf, bytes.NewReader(plain), "p/two"); err != nil {
		t.Fatal(err)
	}
	return plain, buf.Bytes()
}

func mustCorrupt(t *testing.T, k *Keys, ct []byte, relpath, what string) {
	t.Helper()
	_, err := k.Decrypt(io.Discard, bytes.NewReader(ct), relpath)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("%s: want ErrCorrupt, got %v", what, err)
	}
}

type errReader struct {
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	return 0, e.err
}

func TestTamperingIsDetected(t *testing.T) {
	k := testKeys(t)
	_, ct := encryptTwoChunks(t, k)

	flipped := append([]byte{}, ct...)
	flipped[len(flipped)-40] ^= 0x01
	mustCorrupt(t, k, flipped, "p/two", "flipped byte")

	mustCorrupt(t, k, ct[:len(ct)-1], "p/two", "truncated tail")

	// Truncate exactly at the boundary after chunk 0 (header + one full chunk).
	mustCorrupt(t, k, ct[:21+ChunkSize+16], "p/two", "truncated at chunk boundary")

	// Swap chunk 0 and chunk 1.
	c0 := ct[21 : 21+ChunkSize+16]
	c1 := ct[21+ChunkSize+16:]
	swapped := append(append(append([]byte{}, ct[:21]...), c1...), c0...)
	mustCorrupt(t, k, swapped, "p/two", "swapped chunks")

	mustCorrupt(t, k, ct, "p/other", "wrong relpath")

	other := testKeys(t)
	mustCorrupt(t, other, ct, "p/two", "wrong key")

	bad := append([]byte{}, ct...)
	bad[0] = 'X'
	mustCorrupt(t, k, bad, "p/two", "bad magic")
}

func TestIOErrorsPropagate(t *testing.T) {
	k := testKeys(t)
	testErr := errors.New("boom")

	// Encrypt should propagate the error, not mask it.
	_, _, _, err := k.Encrypt(io.Discard, &errReader{err: testErr}, "p/a")
	if !errors.Is(err, testErr) {
		t.Fatalf("Encrypt: want %v, got %v", testErr, err)
	}

	// Decrypt should propagate the error, not mask it as ErrCorrupt.
	_, err = k.Decrypt(io.Discard, &errReader{err: testErr}, "p/a")
	if !errors.Is(err, testErr) {
		t.Fatalf("Decrypt: want %v, got %v", testErr, err)
	}
}
