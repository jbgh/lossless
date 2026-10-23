package store

import "lossless/internal/projectkey"

// SessionsForProject lists the sessions stored under one project, in the
// order they were first recorded. UpsertSession stores project keys
// normalized, so this is the filter callers used to run over a full
// ListSessions, done by the idx_sessions_proj index instead.
func (s *Store) SessionsForProject(project string) ([]Session, error) {
	project = projectkey.Normalize(project)
	if project == "" {
		return nil, nil
	}
	rows, err := s.DB.Query(
		`SELECT jsonl, session_id, harness, workspace, project_key FROM sessions WHERE project_key = ? ORDER BY rowid`,
		project,
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
