package inspect

import (
	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

type PruneResult struct {
	DroppedProjects []string `json:"dropped_projects,omitempty"`
	DroppedSessions int      `json:"dropped_sessions"`
	DroppedRecords  int      `json:"dropped_records"`
	SupersededNoise int      `json:"superseded_noise"`
}

func testSession(s store.Session) bool {
	return claim.FixtureSession(s.SessionID) || write.GoTestPath(s.Workspace) || write.GoTestPath(s.JSONL)
}

func Prune(st *store.Store) (PruneResult, error) {
	var out PruneResult
	sess, err := st.ListSessions()
	if err != nil {
		return out, err
	}
	by := map[string][]store.Session{}
	for _, s := range sess {
		by[s.Project] = append(by[s.Project], s)
	}
	for key, list := range by {
		var test, live []store.Session
		for _, s := range list {
			if testSession(s) {
				test = append(test, s)
			} else {
				live = append(live, s)
			}
		}
		if len(test) == 0 {
			continue
		}
		if err := dropSessions(st, test, &out); err != nil {
			return out, err
		}
		if len(live) == 0 && key != "" {
			if err := dropOrphanProject(st, key, &out); err != nil {
				return out, err
			}
		}
	}
	if err := dropFixtureClaims(st, &out); err != nil {
		return out, err
	}
	if err := supersedeNoise(st, &out); err != nil {
		return out, err
	}
	return out, nil
}

func dropSessions(st *store.Store, sess []store.Session, out *PruneResult) error {
	seen := map[string]bool{}
	for _, s := range sess {
		if seen[s.JSONL] {
			continue
		}
		seen[s.JSONL] = true
		recs, err := st.ListActive(s.Project)
		if err != nil {
			return err
		}
		for _, r := range recs {
			if r.SessionID != s.SessionID {
				continue
			}
			if err := st.DeleteRecord(r.ID); err != nil {
				return err
			}
			out.DroppedRecords++
		}
		if err := st.DeleteSession(s.JSONL); err != nil {
			return err
		}
		out.DroppedSessions++
	}
	return nil
}

func dropOrphanProject(st *store.Store, key string, out *PruneResult) error {
	left, err := st.ListActive(key)
	if err != nil {
		return err
	}
	for _, r := range left {
		if err := st.DeleteRecord(r.ID); err != nil {
			return err
		}
		out.DroppedRecords++
	}
	if err := st.DeleteActions(key); err != nil {
		return err
	}
	if err := st.RemoveProjectRaw(key); err != nil {
		return err
	}
	out.DroppedProjects = append(out.DroppedProjects, key)
	return nil
}

func dropFixtureClaims(st *store.Store, out *PruneResult) error {
	recs, err := st.ListAllActive()
	if err != nil {
		return err
	}
	for _, r := range recs {
		if !claim.FixtureSession(r.SessionID) {
			continue
		}
		if err := st.DeleteRecord(r.ID); err != nil {
			return err
		}
		out.DroppedRecords++
	}
	return nil
}

func supersedeNoise(st *store.Store, out *PruneResult) error {
	recs, err := st.ListAllActive()
	if err != nil {
		return err
	}
	for _, r := range recs {
		if !retrieve.ExtractNoise(r) {
			continue
		}
		if err := st.Supersede(r.ID); err != nil {
			return err
		}
		out.SupersededNoise++
	}
	return nil
}
