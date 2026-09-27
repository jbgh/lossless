package inspect

import (
	"time"

	"lossless/internal/retrieve"
	"lossless/internal/store"
)

// staleConstraints lists the project's decayed, still-active constraints: the
// incident-shaped rules whose warning window has closed
// (docs/algorithm.md §7a).
//
// It is a listing, not a sweep. `inspect --prune` already supersedes extract
// noise; this only names the candidates, because retiring a constraint is the
// operator's call — `lossless supersede <id> [reason]` does it explicitly and
// records why. Nothing here supersedes and nothing is ever deleted; a
// superseded record simply leaves the listing, and its row and text stay in
// the store either way.
//
// With no project it lists every project's, like the other prune rules;
// it used to pass the empty key through and list nothing.
func staleConstraints(st *store.Store, project string) ([]retrieve.StaleCandidate, error) {
	recs, err := activeFor(st, project)
	if err != nil {
		return nil, err
	}
	return retrieve.StaleCandidatesIn(st, recs, time.Now(), retrieve.StaleWindow()), nil
}
