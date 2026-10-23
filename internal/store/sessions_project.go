package store

import (
	"strings"

	"lossless/internal/projectkey"
)

// SessionsForProject lists the sessions whose project key normalizes to
// project, in the order they were first recorded: the filter callers used
// to run over a full ListSessions, done by the idx_sessions_proj index.
// UpsertSession stores keys normalized; a key an older release stored in
// another form (Normalize has changed over releases) is matched through
// legacyKeys.
func (s *Store) SessionsForProject(project string) ([]Session, error) {
	project = projectkey.Normalize(project)
	if project == "" {
		return nil, nil
	}
	keys := append([]any{project}, s.legacyFor(project)...)
	rows, err := s.DB.Query(
		`SELECT jsonl, session_id, harness, workspace, project_key FROM sessions WHERE project_key IN (?`+
			strings.Repeat(",?", len(keys)-1)+`) ORDER BY rowid`,
		keys...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.JSONL, &sess.SessionID, &sess.Harness, &sess.Workspace, &sess.Project); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// SessionsShape is the session table's row count and largest rowid. A
// session another process inserts or deletes (a CLI catch-up, backfill or
// prune) moves it, so the watcher can tell its cached list is stale
// without reading every row; SessionsGen only sees this Store's writes.
func (s *Store) SessionsShape() (count, maxRowid int64, err error) {
	err = s.DB.QueryRow(`SELECT count(*), COALESCE(max(rowid), 0) FROM sessions`).Scan(&count, &maxRowid)
	return count, maxRowid, err
}

// legacyFor returns the stored session keys, other than project itself,
// that normalize to project. The scan of distinct keys runs once per
// Store: every row written since uses the normalized form, so the set
// does not grow.
func (s *Store) legacyFor(project string) []any {
	s.legacyOnce.Do(func() {
		s.legacyKeys = map[string][]string{}
		rows, err := s.DB.Query(`SELECT DISTINCT project_key FROM sessions WHERE project_key IS NOT NULL AND project_key != ''`)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if rows.Scan(&k) != nil {
				continue
			}
			if n := projectkey.Normalize(k); n != k && n != "" {
				s.legacyKeys[n] = append(s.legacyKeys[n], k)
			}
		}
	})
	var out []any
	for _, k := range s.legacyKeys[project] {
		out = append(out, k)
	}
	return out
}
