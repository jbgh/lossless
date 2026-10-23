package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lossless/internal/store"
)

// countStats swaps the per-target stat for a counting wrapper.
func countStats(t *testing.T) map[string]int {
	t.Helper()
	n := map[string]int{}
	real := statFile
	statFile = func(p string) (os.FileInfo, error) { n[p]++; return real(p) }
	t.Cleanup(func() { statFile = real })
	return n
}

// slotDueNext reports whether key's cold slot comes up on the next tick.
func slotDueNext(w *Watcher, key string) bool {
	return (uint64(slotOf(key))+w.gen+1)%w.coldEvery == 0
}

func appendLine(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"assistant","content":"` + text + `"}` + "\n")
	_ = f.Close()
}

func setMod(t *testing.T, p string, mod time.Time) {
	t.Helper()
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// A file idle for longer than coldAfter is checked on its slot, once every
// coldEvery ticks, not every tick: a grown cold file is caught on its slot
// and not before.
func TestWatcherChecksAColdFileOnItsSlot(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	_, catchUps := countPeeks(t)
	w := NewWatcher()
	w.coldEvery = 4
	age(t, cfile, cdir, filepath.Dir(cdir))
	if _, err := w.Tick(st, opts); err != nil || catchUps[cfile] != 1 {
		t.Fatalf("first tick: catch-ups %d %v", catchUps[cfile], err)
	}
	// The tick after a catch-up finds the file caught up and quiet.
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	// A real append moves the mtime to now; a minute back keeps it out of
	// the racy window. The watcher still holds the hour-old stat, so it
	// treats the file as cold until it looks again.
	appendLine(t, cfile, "We decided to keep Postgres, not DynamoDB, for billing.")
	setMod(t, cfile, time.Now().Add(-time.Minute))
	caught := false
	for i := 0; i < int(w.coldEvery); i++ {
		due := slotDueNext(w, cfile)
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
		got := catchUps[cfile] == 2
		if got != due && !caught {
			t.Fatalf("tick %d: caught %v, slot due %v", i, got, due)
		}
		caught = caught || got
	}
	if !caught {
		t.Fatal("a grown cold file was not caught within coldEvery ticks")
	}
	// Caught up, the file is hot again: a resumed session's next turn is
	// caught on the next tick, not at its next slot.
	w.coldEvery = 1 << 30
	for i := 0; i < 3; i++ {
		// Let any racy-window stat after the catch-up settle first.
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
		appendLine(t, cfile, fmt.Sprintf("We decided to resume batch %d.", i))
		setMod(t, cfile, time.Now().Add(-time.Minute+time.Duration(i)*time.Second))
		if slotDueNext(w, cfile) {
			t.Skip("slot happens to be due")
		}
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
		if catchUps[cfile] != 3+i {
			t.Fatalf("resumed file append %d: catch-ups %d, want %d", i, catchUps[cfile], 3+i)
		}
	}
}

// The verify pass checks every file whatever its slot.
func TestWatcherVerifyPassChecksEveryColdFile(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	_, catchUps := countPeeks(t)
	w := NewWatcher()
	w.coldEvery = 1 << 30
	age(t, cfile, cdir, filepath.Dir(cdir))
	for i := 0; i < 2; i++ { // catch up, then found quiet
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
	}
	appendLine(t, cfile, "We decided to pin Go 1.25.")
	age(t, cfile)
	if slotDueNext(w, cfile) {
		t.Skip("slot happens to be due")
	}
	if _, err := w.Tick(st, opts); err != nil || catchUps[cfile] != 1 {
		t.Fatalf("off-slot tick caught a cold file: %d %v", catchUps[cfile], err)
	}
	w.lastVerify = time.Now().Add(-verifyEvery)
	if _, err := w.Tick(st, opts); err != nil || catchUps[cfile] != 2 {
		t.Fatalf("verify pass missed a grown cold file: %d %v", catchUps[cfile], err)
	}
}

// A file that changed within coldAfter is checked on every tick.
func TestWatcherChecksAHotFileEveryTick(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	_, catchUps := countPeeks(t)
	w := NewWatcher()
	w.coldEvery = 1 << 30
	age(t, cdir, filepath.Dir(cdir))
	recent := time.Now().Add(-time.Minute) // past racyWindow, inside coldAfter
	setMod(t, cfile, recent)
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		appendLine(t, cfile, fmt.Sprintf("We decided to ship batch %d on Fridays.", i))
		setMod(t, cfile, recent.Add(time.Duration(i+1)*time.Second))
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
		if catchUps[cfile] != i+2 {
			t.Fatalf("hot file append %d: catch-ups %d", i, catchUps[cfile])
		}
	}
}

// A known session whose transcript is gone costs a stat on its slot, not
// one every tick (1,352 of 5,800 per-tick stats on this machine were for
// gone files).
func TestWatcherChecksAGoneFileOnlyOnItsSlot(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	gone := filepath.Join(root, "gone", "chat_history.jsonl")
	if err := st.UpsertSession(store.Session{JSONL: gone, SessionID: "g", Harness: "grok", Project: "jay/api"}); err != nil {
		t.Fatal(err)
	}
	stats := countStats(t)
	w := NewWatcher()
	w.coldEvery = 5
	if _, err := w.Tick(st, Options{}); err != nil || stats[gone] != 1 {
		t.Fatalf("first tick: stats %d %v", stats[gone], err)
	}
	want := 1
	for i := 0; i < 10; i++ {
		if slotDueNext(w, gone) {
			want++
		}
		if _, err := w.Tick(st, Options{}); err != nil {
			t.Fatal(err)
		}
		if stats[gone] != want {
			t.Fatalf("tick %d: stats %d, want %d", i, stats[gone], want)
		}
	}
	if want != 3 {
		t.Fatalf("ten ticks at coldEvery 5 hit the slot %d times", want-1)
	}
}

// A new session in an idle project directory is found when that
// directory's slot comes up; a new project directory moves the root,
// which is checked every tick, so it is found at once.
func TestWatcherFindsANewFileInAColdDirectoryOnItsSlot(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	_, catchUps := countPeeks(t)
	w := NewWatcher()
	w.coldEvery = 4
	age(t, cfile, cdir, filepath.Dir(cdir))
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(cdir, "uuid-2.jsonl")
	writeClaude(t, newFile, "/Users/jay/dev/api", "We decided to ship the cron on Fridays.")
	age(t, newFile, cdir)
	setMod(t, cdir, time.Now().Add(-30*time.Minute)) // moved, but still cold
	found := false
	for i := 0; i < int(w.coldEvery); i++ {
		due := slotDueNext(w, cdir)
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
		got := catchUps[newFile] == 1
		if got != due && !found {
			t.Fatalf("tick %d: found %v, dir slot due %v", i, got, due)
		}
		found = found || got
	}
	if !found {
		t.Fatal("a new file in a cold directory was not found within coldEvery ticks")
	}
	ndir := filepath.Join(opts.ClaudeRoot, "-Users-jay-dev-web")
	if err := os.MkdirAll(ndir, 0o755); err != nil {
		t.Fatal(err)
	}
	nfile := filepath.Join(ndir, "uuid-3.jsonl")
	writeClaude(t, nfile, "/Users/jay/dev/web", "We decided to drop the CDN.")
	if _, err := w.Tick(st, opts); err != nil || catchUps[nfile] != 1 {
		t.Fatalf("a new project directory was not found on the next tick: %d %v", catchUps[nfile], err)
	}
}

// Cold checks are spread over the slots: across coldEvery ticks every
// cold file is checked exactly once, never all on one tick.
func TestWatcherSpreadsColdChecksOverTicks(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const n = 60
	for i := 0; i < n; i++ {
		p := filepath.Join(root, "gone", fmt.Sprintf("s%02d", i), "chat_history.jsonl")
		if err := st.UpsertSession(store.Session{JSONL: p, SessionID: fmt.Sprint(i), Harness: "grok", Project: "jay/api"}); err != nil {
			t.Fatal(err)
		}
	}
	stats := countStats(t)
	w := NewWatcher()
	if _, err := w.Tick(st, Options{}); err != nil || total(stats) != n {
		t.Fatalf("first tick stats %d %v", total(stats), err)
	}
	before := total(stats)
	most := 0
	for i := 0; i < int(w.coldEvery); i++ {
		b := total(stats)
		if _, err := w.Tick(st, Options{}); err != nil {
			t.Fatal(err)
		}
		if d := total(stats) - b; d > most {
			most = d
		}
	}
	if got := total(stats) - before; got != n {
		t.Fatalf("over coldEvery ticks: %d stats, want %d (each cold file once)", got, n)
	}
	if most > n/2 {
		t.Fatalf("one tick ran %d of %d cold checks", most, n)
	}
}
