// internal/backup/schedule.go
package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"lossless/internal/version"
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

	tickDone      chan struct{} // tests: closed-loop stepping
	lastConfigErr string        // last logged LoadConfig error, so it is not repeated every tick
	lastEvery     time.Duration // interval the due time was computed from
	lastOK        string        // last_ok the due time was computed from
}

func NewScheduler(home string) *Scheduler {
	s := &Scheduler{Home: home, Now: time.Now}
	s.Logf = func(format string, args ...any) {
		stamp := time.Now().UTC().Format(time.RFC3339)
		fmt.Fprintf(os.Stderr, stamp+" lossless backup ("+version.Version+"): "+format+"\n", args...)
	}
	s.Run = func(ctx context.Context, home string) error {
		sum, err := Run(ctx, home, RunOptions{Out: &logWriter{logf: s.Logf}})
		if err != nil {
			return err
		}
		if sum.NoChange {
			s.Logf("no change (%d files, %s)", sum.Scanned, sum.Elapsed.Round(time.Millisecond))
		} else {
			s.Logf("generation %s uploaded %d (%d bytes) deleted %d dropped %v (%s)",
				sum.Generation, sum.Uploaded, sum.Bytes, sum.Deleted, sum.Dropped, sum.Elapsed.Round(time.Millisecond))
		}
		return nil
	}
	return s
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

// logWriter routes Run's diagnostics (the "backup: …" lines dropGenerations
// prints for a drop that could not finish) into the daemon log. Other
// output, like the "no change" line the CLI shows, is dropped: the
// scheduler logs its own summary.
type logWriter struct {
	logf func(format string, args ...any)
	buf  []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		if rest, ok := strings.CutPrefix(line, "backup: "); ok {
			w.logf("%s", rest)
		}
	}
}

// safeRun turns a panic inside one backup into an error, the way
// watch.safeTick does for a tick: the scheduler shares the daemon with the
// HTTP surface, and one bad cache entry must not take ask and catch-up down.
func (s *Scheduler) safeRun(home string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("backup panic: %v", r)
		}
	}()
	return s.Run(context.Background(), home)
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
	if err != nil {
		if errors.Is(err, ErrNotConfigured) {
			s.lastConfigErr = ""
		} else if err.Error() != s.lastConfigErr {
			s.Logf("config: %v", err)
			s.lastConfigErr = err.Error()
		}
		*configured = false
		return
	}
	if s.lastConfigErr != "" {
		s.Logf("config ok")
		s.lastConfigErr = ""
	}
	if cfg.Every <= 0 {
		*configured = false
		return
	}
	now := s.Now()
	// The due time follows backup.env and backup-state.json: an edited
	// interval or a manual `lossless backup` success moves it. A failure
	// retry changes neither, so the due time chosen below stands.
	st := LoadState(s.Home)
	if !*configured || cfg.Every != s.lastEvery || st.LastOK != s.lastOK {
		last, has := st.LastOKTime()
		*due = nextDue(start, last, has, cfg.Every)
		*configured = true
		s.lastEvery, s.lastOK = cfg.Every, st.LastOK
	}
	if now.Before(*due) {
		return
	}
	err = s.safeRun(s.Home)
	now = s.Now()
	switch {
	case err == nil:
		*due = now.Add(cfg.Every)
	case errors.Is(err, ErrLocked):
		s.Logf("manual run in progress; retrying next minute")
		*due = now.Add(time.Minute)
	case errors.Is(err, ErrEmptyStore):
		s.Logf("nothing to back up yet")
		*due = now.Add(cfg.Every)
	default:
		s.Logf("%v", err)
		*due = now.Add(min(failRetry, cfg.Every))
	}
}
