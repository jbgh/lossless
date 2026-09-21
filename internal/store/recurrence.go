package store

import (
	"lossless/internal/projectkey"
)

// recurrenceCols is the lean projection for the recurrence scan: the
// scan runs on every ask, so it hydrates only what the clusterer reads
// instead of the full 17-column claim row.
const recurrenceCols = `id, type, session_id, created_at, text`

// RecurrenceRow is the lean projection the recurrence scan needs.
type RecurrenceRow struct {
	ID        string
	Type      string
	SessionID string
	CreatedAt string
	Text      string
}

// RecordsForRecurrence returns the active failed/constraint rows of a
// project created since the given RFC3339 time. The recurrence scan reads
// the store directly — deliberately NOT through the read-time extract
// gates, so a gate-named failed that pack-time noise rules would drop
// still counts toward a recurrence trigger.
func (s *Store) RecordsForRecurrence(project, since string) ([]RecurrenceRow, error) {
	rows, err := s.DB.Query(`SELECT `+recurrenceCols+` FROM records
		WHERE project_key = ? AND status = 'active' AND type IN ('failed','constraint')
		AND created_at >= ?`, projectkey.Normalize(project), since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecurrenceRow
	for rows.Next() {
		var r RecurrenceRow
		if err := rows.Scan(&r.ID, &r.Type, &r.SessionID, &r.CreatedAt, &r.Text); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
