package serve

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"syscall"
	"time"

	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/version"
)

// slowAskAfter is the wall time past which an ask writes one line to
// serve.log saying where the time went. An ask is about 100 ms of CPU;
// 2.5 s asks were seen on 2026-10-23 and did not reproduce.
var slowAskAfter = time.Second

// slowAskOut is serve.log (the daemon's stderr). Tests capture it.
var slowAskOut io.Writer = os.Stderr

// asksInFlight counts asks being answered right now, so a slow line shows
// whether it queued behind others.
var asksInFlight atomic.Int64

// timedAsk answers an ask and logs it when it was slow.
func timedAsk(st *store.Store, req retrieve.Request) (retrieve.Response, error) {
	n := asksInFlight.Add(1)
	defer asksInFlight.Add(-1)
	start, cpu0 := time.Now(), processCPU()
	out, tm, err := retrieve.AskTimed(st, req)
	if wall := time.Since(start); wall >= slowAskAfter {
		line := slowAskLine(wall, processCPU()-cpu0, tm, req.SessionID != "", n)
		fmt.Fprintf(slowAskOut, "%s lossless serve (%s): %s\n",
			time.Now().UTC().Format(time.RFC3339), version.Version, line)
	}
	return out, err
}

// slowAskLine splits an ask's wall time. "rest" is search, scoring and
// packing, which is all CPU: when it is large and the daemon's CPU over
// the same window is small, the daemon was waiting to be scheduled.
// Daemon CPU covers every goroutine (the watcher too), not this ask alone.
// The line carries no goal, path, project or session id.
func slowAskLine(wall, cpu time.Duration, tm retrieve.AskTiming, sid bool, inFlight int64) string {
	rest := wall - tm.CatchUp - tm.Project
	if rest < 0 {
		rest = 0
	}
	sessions := "sessions"
	if tm.CatchUps == 1 {
		sessions = "session"
	}
	yn := "no"
	if sid {
		yn = "yes"
	}
	return fmt.Sprintf("slow ask %.2fs: catch-up %.2fs (%d %s), project %.2fs, rest %.2fs; daemon cpu %.2fs during it, sid=%s, in flight %d",
		wall.Seconds(), tm.CatchUp.Seconds(), tm.CatchUps, sessions, tm.Project.Seconds(), rest.Seconds(), cpu.Seconds(), yn, inFlight)
}

// processCPU is this process's user plus system CPU time so far.
func processCPU() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
