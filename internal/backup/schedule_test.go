// internal/backup/schedule_test.go
package backup

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// drive sends one tick per step and lets the loop process it before the
// next; runAt records the fake times at which Run fired.
type harness struct {
	clock  *fakeClock
	ticks  chan time.Time
	runAt  []time.Time
	runErr error
	done   chan struct{}
	s      *Scheduler
}

func newHarness(t *testing.T, home string, start time.Time) *harness {
	t.Helper()
	h := &harness{clock: &fakeClock{t: start}, ticks: make(chan time.Time), done: make(chan struct{})}
	stepDone := make(chan struct{}, 1)
	h.s = &Scheduler{
		Home: home,
		Logf: func(string, ...any) {},
		Now:  h.clock.now,
		Run: func(context.Context, string) error {
			h.runAt = append(h.runAt, h.clock.t)
			return h.runErr
		},
		tickDone: stepDone,
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { h.s.Loop(ctx, h.ticks); close(h.done) }()
	<-stepDone // wait for Loop to capture its start time before the first tick
	return h
}

func (h *harness) tickAt(offset time.Duration) {
	h.clock.t = h.clock.t.Add(offset)
	h.ticks <- h.clock.t
	<-h.s.tickDone
}

func scheduledHome(t *testing.T, every time.Duration, lastOK string) string {
	t.Helper()
	clearBackupEnv(t)
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "a")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "s")
	home := t.TempDir()
	must(t, Init(home, InitOptions{URL: "s3://b", Every: every}))
	if lastOK != "" {
		st := LoadState(home)
		st.LastOK = lastOK
		must(t, st.Save(home))
	}
	return home
}

func TestNextDue(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if d := nextDue(start, time.Time{}, false, time.Hour); !d.Equal(start.Add(2 * time.Minute)) {
		t.Fatal("no last_ok: floor", d)
	}
	if d := nextDue(start, start.Add(-3*time.Hour), true, time.Hour); !d.Equal(start.Add(2 * time.Minute)) {
		t.Fatal("overdue: floor", d)
	}
	if d := nextDue(start, start.Add(-50*time.Minute), true, time.Hour); !d.Equal(start.Add(10 * time.Minute)) {
		t.Fatal("fresh: last_ok + every", d)
	}
}

func TestSchedulerRunsAtDueTime(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, time.Hour, start.Add(-50*time.Minute).Format(time.RFC3339))
	h := newHarness(t, home, start)
	h.tickAt(time.Minute) // 12:01, due 12:10
	h.tickAt(8 * time.Minute)
	if len(h.runAt) != 0 {
		t.Fatalf("ran early: %v", h.runAt)
	}
	h.tickAt(2 * time.Minute) // 12:11
	if len(h.runAt) != 1 {
		t.Fatalf("want one run, got %v", h.runAt)
	}
	h.tickAt(30 * time.Minute) // 12:41, next due 13:11
	if len(h.runAt) != 1 {
		t.Fatal("ran before the interval elapsed")
	}
	h.tickAt(31 * time.Minute) // 13:12
	if len(h.runAt) != 2 {
		t.Fatalf("want two runs, got %v", h.runAt)
	}
}

func TestSchedulerOverdueWaitsForFloor(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, time.Hour, "")
	h := newHarness(t, home, start)
	h.tickAt(time.Minute)
	if len(h.runAt) != 0 {
		t.Fatal("must not run before the two-minute floor")
	}
	h.tickAt(2 * time.Minute)
	if len(h.runAt) != 1 || !h.runAt[0].Equal(start.Add(3*time.Minute)) {
		t.Fatalf("%v", h.runAt)
	}
}

func TestSchedulerFailureRetriesSoonAndLockedRetriesNextMinute(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, time.Hour, "")
	h := newHarness(t, home, start)
	h.runErr = errors.New("boom")
	h.tickAt(3 * time.Minute) // runs, fails
	h.tickAt(14 * time.Minute)
	if len(h.runAt) != 1 {
		t.Fatal("failed run must not retry before 15m")
	}
	h.tickAt(2 * time.Minute) // 12:19 ≥ 12:18
	if len(h.runAt) != 2 {
		t.Fatalf("want retry after 15m, got %v", h.runAt)
	}
	h.runErr = ErrLocked
	h.tickAt(16 * time.Minute) // 12:35, runs, locked
	h.tickAt(time.Minute)      // 12:36: locked means try again next minute
	if len(h.runAt) != 4 {
		t.Fatalf("locked must retry next tick, got %v", h.runAt)
	}
}

func TestSchedulerEveryZeroNeverRuns(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	home := scheduledHome(t, 0, "")
	h := newHarness(t, home, start)
	for i := 0; i < 5; i++ {
		h.tickAt(time.Hour)
	}
	if len(h.runAt) != 0 {
		t.Fatal(h.runAt)
	}
}

func TestSchedulerPicksUpLaterInit(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	clearBackupEnv(t)
	home := t.TempDir()
	h := newHarness(t, home, start)
	h.tickAt(time.Hour)
	if len(h.runAt) != 0 {
		t.Fatal("unconfigured must not run")
	}
	t.Setenv("LOSSLESS_BACKUP_ACCESS_KEY", "a")
	t.Setenv("LOSSLESS_BACKUP_SECRET_KEY", "s")
	must(t, Init(home, InitOptions{URL: "s3://b", Every: time.Hour}))
	h.tickAt(time.Minute) // configured now; floor starts here
	h.tickAt(3 * time.Minute)
	if len(h.runAt) != 1 {
		t.Fatalf("a daemon that was up before init must start backing up: %v", h.runAt)
	}
}
