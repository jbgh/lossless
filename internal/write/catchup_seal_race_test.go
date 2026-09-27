package write

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// CatchUp opens the live tape, then waits for its lock. A seal that
// unlinks the tape in between (the daemon's raw sweep, or idle-seal)
// used to leave CatchUp writing into an inode nothing points at, while
// its cursor moved past the lines. The writer holds the lock here while
// the tape is unlinked, the way SealRaw does, so CatchUp must notice the
// unlink after it gets the lock and write to the next part instead.
func TestCatchUpDuringSealDoesNotLoseTheLine(t *testing.T) {
	st := tmpStore(t)
	dir := t.TempDir()
	src := writeJSONL(t, dir, "chat.jsonl", `{"type":"user","content":"first turn"}`+"\n")
	req := CatchUpRequest{JSONL: src, Project: "acme/api", Harness: "grok", SessionID: "grok-race"}
	res, err := CatchUp(st, req)
	if err != nil || res.Copied == 0 {
		t.Fatal(res, err)
	}
	raw := res.RawPath

	// Hold the tape's lock, as a seal in progress does.
	sealer, err := os.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(sealer.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"user","content":"turn during the seal"}` + "\n")
	_ = f.Close()

	done := make(chan CatchUpResult, 1)
	go func() {
		r, err := CatchUp(st, req)
		if err != nil {
			t.Errorf("catch-up: %v", err)
		}
		done <- r
	}()
	// Give CatchUp time to open the tape and block on the lock. If it
	// has not opened it yet it opens the next part directly, and the
	// assertions below still hold.
	time.Sleep(200 * time.Millisecond)

	// Finish the "seal": a .zst takes the tape's place and the
	// plaintext is unlinked, then the lock is released.
	if err := os.WriteFile(raw+".zst", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(raw); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Flock(int(sealer.Fd()), syscall.LOCK_UN)
	_ = sealer.Close()

	r := <-done
	if r.RawPath == raw {
		t.Fatalf("catch-up wrote to the unlinked tape %s", raw)
	}
	body, err := os.ReadFile(r.RawPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "turn during the seal") {
		t.Fatalf("line lost; %s holds %q", r.RawPath, body)
	}
}

// A seal that finishes between resolving the path and opening it leaves
// only the .zst, so O_CREATE makes a fresh plain file beside it. The
// writer must not keep that file: every later seal would refuse to
// overwrite the .zst and the tape would grow unsealed.
func TestOpenLiveLockedSkipsFreshFileBesideSeal(t *testing.T) {
	st := tmpStore(t)
	now := time.Now()
	base := st.LiveRawPath("acme/api", "s-fresh", now)
	if err := os.MkdirAll(filepath.Dir(base), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+".zst", []byte("sealed"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	f, err := openLiveLocked(func() string {
		calls++
		if calls == 1 {
			return base // resolved before the seal finished
		}
		return st.LiveRawPath("acme/api", "s-fresh", now)
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Name() == base {
		t.Fatalf("kept a fresh plain tape beside %s.zst", base)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("fresh plain tape left behind: %v", err)
	}
}

// The manual tape has no .partN rollover: a remember beside a sealed
// month file must still write, not retry until it gives up.
func TestRememberBesideSealedManualTape(t *testing.T) {
	st := tmpStore(t)
	p := st.ManualRawPath(time.Now())
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".zst", []byte("sealed"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openLiveLocked(func() string { return p }, false)
	if err != nil {
		t.Fatalf("remember path refused beside a sealed manual tape: %v", err)
	}
	_ = f.Close()
}

// At the month boundary a remember may resolve last month's manual tape
// just before the sweep seals it. Resolving again gives the new month's
// path, so the empty file beside the old .zst is dropped and the line
// goes to the new month.
func TestRememberAtMonthBoundaryMovesOn(t *testing.T) {
	st := tmpStore(t)
	old := st.ManualRawPath(time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC))
	next := st.ManualRawPath(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old+".zst", []byte("sealed"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	f, err := openLiveLocked(func() string {
		calls++
		if calls == 1 {
			return old
		}
		return next
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Name() != next {
		t.Fatalf("wrote to %s, want the new month's %s", f.Name(), next)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("empty old-month tape left beside its .zst: %v", err)
	}
}
