package watch

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"lossless/internal/store"
)

// TestRealRootsTickCost measures one discovery pass and one tick over the
// real harness roots of this machine, read-only, against a store at
// LOSSLESS_REAL_BENCH_HOME (a temp copy: ticks write to it). It never runs
// in `make test`; it is how the watcher's idle cost is measured:
//
//	LOSSLESS_REAL_BENCH_HOME=/tmp/benchhome go test ./internal/watch -run TestRealRootsTickCost -v -count=1
func TestRealRootsTickCost(t *testing.T) {
	home := os.Getenv("LOSSLESS_REAL_BENCH_HOME")
	if home == "" {
		t.Skip("set LOSSLESS_REAL_BENCH_HOME to a temp store copy")
	}
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	t.Setenv("LOSSLESS_URL", "")
	t.Setenv("LOSSLESS_BASE_URL", "")

	var peeks atomic.Int64
	pc, pk, pp, po := peekClaudeCWD, peekCodexMeta, peekPiCWD, peekOmpCWD
	peekClaudeCWD = func(p string) string { peeks.Add(1); return pc(p) }
	peekCodexMeta = func(p string) (string, string) { peeks.Add(1); return pk(p) }
	peekPiCWD = func(p string) string { peeks.Add(1); return pp(p) }
	peekOmpCWD = func(p string) string { peeks.Add(1); return po(p) }
	t.Cleanup(func() { peekClaudeCWD, peekCodexMeta, peekPiCWD, peekOmpCWD = pc, pk, pp, po })

	opts := Defaults()
	known, err := st.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcher()
	measure := func(name string, n int, f func() int) {
		for i := 0; i < n; i++ {
			peeks.Store(0)
			cpu0 := cpuTime()
			t0 := time.Now()
			seen := f()
			t.Logf("%s #%d: wall %v cpu %v peeks %d targets %d",
				name, i, time.Since(t0).Round(time.Millisecond), (cpuTime() - cpu0).Round(time.Millisecond), peeks.Load(), seen)
		}
	}
	measure("discover (uncached)", 2, func() int { return len(Discover(opts, known)) })
	measure("discover (watcher)", 3, func() int { return len(w.discover(opts, known)) })
	measure("tick (watcher)", 6, func() int {
		res, err := w.Tick(st, opts)
		if err != nil {
			t.Fatal(err)
		}
		return res.Seen
	})
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
