package watch

import (
	"context"
	"errors"
	"testing"
	"time"

	"lossless/internal/store"
)

// One panicking tick must not take the daemon down: Run keeps ticking
// and returns only when its context ends.
func TestRunSurvivesTickPanic(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	fired := 0
	testPanicTick = func() {
		fired++
		if fired == 1 {
			panic("İstanbul slice bounds")
		}
	}
	t.Cleanup(func() { testPanicTick = nil })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	err = Run(ctx, st, Options{Interval: 10 * time.Millisecond, GrokRoot: t.TempDir(), ClaudeRoot: t.TempDir(), CodexRoot: t.TempDir(), PiRoot: t.TempDir()})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v, want context deadline", err)
	}
	if fired < 2 {
		t.Fatalf("watcher stopped ticking after the panic (ticks=%d)", fired)
	}
}
