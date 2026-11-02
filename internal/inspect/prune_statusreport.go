package inspect

import (
	"lossless/internal/store"
	"lossless/internal/write"
)

// supersedeStatusReports retires the failed records that are really
// status reports, using the same predicate extract drops them with. It
// supersedes, never deletes: the row and its text survive.
//
// The seen-before lookup counts only records created before this one, so
// of several reports of the same failure the earliest survives: it is the
// first real report the capture-time guard would have kept. A brand-new
// failure whose only report is worded as a status report survives too.
func supersedeStatusReports(st *store.Store, project string, out *PruneResult) error {
	recs, err := activeFor(st, project)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if r.Type != "failed" {
			continue
		}
		self := r
		seen := func(name string) bool {
			ok, err := st.FailedNameSeen(self.ProjectKey, name, self.ID, self.CreatedAt)
			return err == nil && ok
		}
		reason, drop := write.StatusReportSkip(self.Text, seen)
		if !drop {
			continue
		}
		// A red step is a capture-time shape: an agent that wrote the
		// sentence with remember meant to keep it.
		if reason == "red-step" && self.Source == "remember" {
			continue
		}
		if err := st.Supersede(self.ID); err != nil {
			return err
		}
		out.SupersededStatusReports++
	}
	return nil
}
