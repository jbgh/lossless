package retrieve

import (
	"time"

	"lossless/internal/store"
)

// AskTiming is where one ask spent its wall time, for the daemon's
// slow-ask log line. An ask is CPU-bound (about 100 ms on a store with
// thousands of records); a slow one either waited on something here or
// was starved of CPU, and the split tells the two apart.
type AskTiming struct {
	// CatchUp is the ask-time catch-up of behind session files.
	CatchUp time.Duration
	// CatchUps counts the sessions that catch-up copied lines from.
	CatchUps int
	// Project is resolving the workspace to a project key (git).
	Project time.Duration
}

// AskTimed is Ask with its timing.
func AskTimed(st *store.Store, req Request) (Response, AskTiming, error) {
	t := &AskTiming{}
	req.timing = t
	out, err := Ask(st, req)
	return out, *t, err
}

// The methods take a nil receiver: an ask that is not timed records
// nothing.

func (t *AskTiming) addCatchUp(start time.Time) {
	if t != nil {
		t.CatchUp += time.Since(start)
	}
}

func (t *AskTiming) countCatchUp() {
	if t != nil {
		t.CatchUps++
	}
}

func (t *AskTiming) addProject(start time.Time) {
	if t != nil {
		t.Project += time.Since(start)
	}
}
