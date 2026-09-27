package inspect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/store"
	"lossless/internal/write"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// writeTape drops a plain tape and ages it by the given idle time. The
// body repeats so a seal actually shrinks it, the way a real transcript
// does; a 50-byte file grows under zstd's frame overhead.
func writeTape(t *testing.T, st *store.Store, path string, idle time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&body, `{"type":"message","role":"user","content":"idle line %d"}`+"\n", i)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-idle)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// The sweep finds tapes idle past the window and reports what sealing
// them would save. It is dry-run by default: a report must not touch
// the tape.
func TestSealRawSweepDryRunTouchesNothing(t *testing.T) {
	st := testStore(t)
	month := time.Now().AddDate(0, -1, 0)
	old := st.RawPath("acme/api", "sess-old", month)
	fresh := st.RawPath("acme/api", "sess-fresh", month)
	writeTape(t, st, old, 48*time.Hour)
	writeTape(t, st, fresh, time.Hour)

	res, err := SealRawSweep(st, 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.DryRun != true {
		t.Fatal("the sweep must default to a dry run")
	}
	if res.Tapes != 1 || len(res.Candidates) != 1 {
		t.Fatalf("want 1 candidate, got %d (%+v)", res.Tapes, res.Candidates)
	}
	if res.Candidates[0].Path != old {
		t.Fatalf("candidate %q, want the idle tape %q", res.Candidates[0].Path, old)
	}
	if res.Bytes != res.Candidates[0].Bytes || res.Bytes == 0 {
		t.Fatalf("bytes %d, want the candidate's %d", res.Bytes, res.Candidates[0].Bytes)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("a dry run must not remove the tape")
	}
	if _, err := os.Stat(old + ".zst"); err == nil {
		t.Fatal("a dry run must not compress the tape")
	}
}

// A real run seals through SealRaw: the plaintext is replaced by a .zst
// that decompresses to the same bytes.
func TestSealRawSweepSealsIdleTapes(t *testing.T) {
	st := testStore(t)
	month := time.Now().AddDate(0, -1, 0)
	old := st.RawPath("acme/api", "sess-old", month)
	fresh := st.RawPath("acme/api", "sess-fresh", month)
	writeTape(t, st, old, 48*time.Hour)
	before, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	writeTape(t, st, fresh, time.Hour)

	res, err := SealRawSweep(st, 24*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tapes != 1 {
		t.Fatalf("want 1 sealed, got %d (%+v)", res.Tapes, res.Candidates)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the sealed plaintext should be gone")
	}
	zst, err := os.Stat(old + ".zst")
	if err != nil {
		t.Fatal(err)
	}
	if res.BytesSaved <= 0 {
		t.Fatalf("bytes saved %d, want a positive number", res.BytesSaved)
	}
	if res.Bytes != res.BytesSaved+zst.Size() {
		t.Fatalf("saved %d + zst %d != plain %d", res.BytesSaved, zst.Size(), res.Bytes)
	}
	if res.Sealed != 1 {
		t.Fatalf("sealed %d, want 1", res.Sealed)
	}
	// The tape still reads back as itself.
	got, err := write.ReadRaw(old)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(before) {
		t.Fatal("the sealed tape does not decompress to the plaintext")
	}
	// A tape inside the idle window is left alone.
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a fresh tape must not be sealed")
	}
	if _, err := os.Stat(fresh + ".zst"); err == nil {
		t.Fatal("a fresh tape must not be compressed")
	}
}

// manual/<month>/remember.jsonl is the open append target: `remember`
// keeps writing to it, and it has no .partN rollover, so sealing it
// would make the next seal refuse and strand the plaintext. The current
// month is skipped; an older month is closed and safe.
func TestSealRawSweepSkipsCurrentMonthRemember(t *testing.T) {
	st := testStore(t)
	live := st.ManualRawPath(time.Now())
	writeTape(t, st, live, 48*time.Hour)
	old := st.ManualRawPath(time.Now().AddDate(0, -1, 0))
	writeTape(t, st, old, 48*time.Hour)

	res, err := SealRawSweep(st, 24*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("the current month's remember.jsonl must not be sealed")
	}
	if _, err := os.Stat(live + ".zst"); err == nil {
		t.Fatal("the current month's remember.jsonl must not be compressed")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("a past month's remember.jsonl should be sealed")
	}
	if res.Tapes != 1 {
		t.Fatalf("want 1 sealed, got %d (%+v)", res.Tapes, res.Candidates)
	}
}

// A tape that is already sealed, or a sidecar that is not a tape, is
// never a candidate.
func TestSealRawSweepIgnoresNonTapes(t *testing.T) {
	st := testStore(t)
	month := time.Now().AddDate(0, -1, 0)
	old := st.RawPath("acme/api", "sess-old", month)
	writeTape(t, st, old, 48*time.Hour)
	sealed := st.RawPath("acme/api", "sess-sealed", month)
	writeTape(t, st, sealed, 48*time.Hour)
	if err := os.Remove(sealed); err != nil {
		t.Fatal(err)
	}
	// A .zst with no plaintext beside it (an orphan) and the lock
	// sidecars the writers keep.
	writeTape(t, st, sealed+".zst", 48*time.Hour)
	writeTape(t, st, old+".lock", 48*time.Hour)
	writeTape(t, st, old+".zst.tmp", 48*time.Hour)

	res, err := SealRawSweep(st, 24*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tapes != 1 || res.Candidates[0].Path != old {
		t.Fatalf("only the plain idle tape is a candidate, got %+v", res.Candidates)
	}
}
