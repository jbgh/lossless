package inspect

import (
	"lossless/internal/claim"
	"lossless/internal/projectkey"
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

func Prune(st *store.Store, project string) (PruneResult, error) {
	var out PruneResult
	key := projectkey.Normalize(project)
	sess, err := st.ListSessions()
	if err != nil {
		return out, err
	}
	by := map[string][]store.Session{}
	for _, s := range sess {
		if key != "" && s.Project != key {
			continue
		}
		by[s.Project] = append(by[s.Project], s)
	}
	for proj, list := range by {
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
		if len(live) == 0 && proj != "" {
			if err := dropOrphanProject(st, proj, &out); err != nil {
				return out, err
			}
		}
	}
	if err := dropFixtureClaims(st, key, &out); err != nil {
		return out, err
	}
	if err := supersedeNoise(st, key, &out); err != nil {
		return out, err
	}
	return out, nil
}

func activeFor(st *store.Store, project string) ([]claim.Record, error) {
	if project == "" {
		return st.ListAllActive()
	}
	return st.ListActive(project)
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

func dropFixtureClaims(st *store.Store, project string, out *PruneResult) error {
	recs, err := activeFor(st, project)
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

func supersedeNoise(st *store.Store, project string, out *PruneResult) error {
	recs, err := activeFor(st, project)
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
