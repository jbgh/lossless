package retrieve

import (
	"time"

	"lossless/internal/projectkey"
	"lossless/internal/store"
)

// AskTiming is where one ask spent its wall time, for the daemon's
// slow-ask log line. An ask is CPU-bound (about 100 ms on a store with
// thousands of records); a slow one either waited on something here or
// was starved of CPU, and the split tells the two apart.
type AskTiming struct {
	// CatchUp is the ask-time catch-up of behind session files, less
	// the project lookups it made (those count in Project).
	CatchUp time.Duration
	// CatchUps counts the sessions that catch-up copied lines from.
	CatchUps int
	// Project is resolving the workspace to a project key (git), wherever
	// in the ask it happened.
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

// startCatchUp times the ask-time catch-up; the returned func stops it.
// Project lookups made inside it are left out of CatchUp.
func (t *AskTiming) startCatchUp() func() {
	if t == nil {
		return func() {}
	}
	start, project0 := time.Now(), t.Project
	return func() { t.CatchUp += time.Since(start) - (t.Project - project0) }
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

// project resolves a workspace to its project key through the cache,
// timed into Project.
func (t *AskTiming) project(workspace string) string {
	defer t.addProject(time.Now())
	return projectkey.Cached(workspace)
}
