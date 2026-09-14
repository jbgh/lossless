// internal/backup/schedule.go
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	startFloor = 2 * time.Minute
	failRetry  = 15 * time.Minute
)

// Scheduler runs backups inside serve --watch on a due time derived from
// the last success, so a daemon that restarts often still backs up and a
// laptop that sleeps past its due time backs up soon after waking.
type Scheduler struct {
	Home string
	Logf func(format string, args ...any)
	Now  func() time.Time
	Run  func(ctx context.Context, home string) error

	tickDone chan struct{} // tests: closed-loop stepping
}

func NewScheduler(home string) *Scheduler {
	return &Scheduler{
		Home: home,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, time.Now().UTC().Format(time.RFC3339)+" lossless backup: "+format+"\n", args...)
		},
		Now: time.Now,
		Run: func(ctx context.Context, home string) error {
			sum, err := Run(ctx, home, RunOptions{Out: io.Discard})
			if err != nil {
				return err
			}
			if sum.NoChange {
				fmt.Fprintf(os.Stderr, "%s lossless backup: no change (%d files, %s)\n", time.Now().UTC().Format(time.RFC3339), sum.Scanned, sum.Elapsed.Round(time.Millisecond))
			} else {
				fmt.Fprintf(os.Stderr, "%s lossless backup: generation %s uploaded %d (%d bytes) deleted %d dropped %v (%s)\n",
					time.Now().UTC().Format(time.RFC3339), sum.Generation, sum.Uploaded, sum.Bytes, sum.Deleted, sum.Dropped, sum.Elapsed.Round(time.Millisecond))
			}
			return nil
		},
	}
}

func nextDue(start, lastOK time.Time, hasLast bool, every time.Duration) time.Time {
	floor := start.Add(startFloor)
	if !hasLast {
		return floor
	}
	if due := lastOK.Add(every); due.After(floor) {
		return due
	}
	return floor
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// Loop reads backup.env on every tick, so a later `backup init` or an
// edited interval takes effect within a minute. Ticks before the due time
// do nothing. The run is synchronous: a tick can never overlap a run.
//
// start is captured once, here, rather than whenever config first loads
// successfully: the startFloor is measured from serve start (see
// DoctorCheck's "due within Xm of serve start"), so a backup that was
// never configured until after serve came up still runs promptly instead
// of getting a fresh two-minute grace period at whatever later tick
// discovers the config.
func (s *Scheduler) Loop(ctx context.Context, ticks <-chan time.Time) {
	start := s.Now()
	var (
		due        time.Time
		configured bool
	)
	// Tests: signal that start is captured, so a fake clock's first tick
	// is never sent before this line runs.
	if s.tickDone != nil {
		s.tickDone <- struct{}{}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			s.step(start, &due, &configured)
			if s.tickDone != nil {
				s.tickDone <- struct{}{}
			}
		}
	}
}

func (s *Scheduler) step(start time.Time, due *time.Time, configured *bool) {
	cfg, err := LoadConfig(s.Home)
	if err != nil || cfg.Every <= 0 {
		if err != nil && !errors.Is(err, ErrNotConfigured) {
			s.Logf("config: %v", err)
		}
		*configured = false
		return
	}
	now := s.Now()
	if !*configured {
		last, has := LoadState(s.Home).LastOKTime()
		*due = nextDue(start, last, has, cfg.Every)
		*configured = true
	}
	if now.Before(*due) {
		return
	}
	err = s.Run(context.Background(), s.Home)
	now = s.Now()
	switch {
	case err == nil:
		*due = now.Add(cfg.Every)
	case errors.Is(err, ErrLocked):
		s.Logf("manual run in progress; retrying next minute")
		*due = now.Add(time.Minute)
	default:
		s.Logf("%v", err)
		*due = now.Add(minDuration(failRetry, cfg.Every))
	}
}
