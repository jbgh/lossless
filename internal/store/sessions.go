package store

import "lossless/internal/projectkey"

// SessionsGen moves whenever this Store inserts, changes or deletes a
// session row. The watcher keeps the session list between ticks and
// reloads it when this moves; a row written by another process (the CLI)
// is seen at the watcher's next full pass.
func (s *Store) SessionsGen() uint64 { return s.sessionsGen.Load() }

type Session struct {
	JSONL     string
	SessionID string
	Harness   string
	Workspace string
	Project   string
}

func (s *Store) UpsertSession(sess Session) error {
	if sess.JSONL == "" {
		return nil
	}
	if sess.Project != "" {
		sess.Project = projectkey.Normalize(sess.Project)
	}
	// A catch-up re-upserts its session's row every time; the WHERE
	// makes an unchanged row a no-op, so it writes nothing and leaves
	// SessionsGen alone.
	return withBusyRetry(func() error {
		res, err := s.DB.Exec(`INSERT INTO sessions(jsonl, session_id, harness, workspace, project_key)
		VALUES(?,?,?,?,?)
		ON CONFLICT(jsonl) DO UPDATE SET
			session_id=excluded.session_id,
			harness=excluded.harness,
			workspace=excluded.workspace,
			project_key=excluded.project_key
		WHERE sessions.session_id IS NOT excluded.session_id
			OR sessions.harness IS NOT excluded.harness
			OR sessions.workspace IS NOT excluded.workspace
			OR sessions.project_key IS NOT excluded.project_key`,
			sess.JSONL, sess.SessionID, sess.Harness, sess.Workspace, sess.Project)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			s.sessionsGen.Add(1)
		}
		return nil
	})
}

func (s *Store) SessionByJSONL(jsonl string) (Session, bool) {
	var out Session
	err := s.DB.QueryRow(
		`SELECT jsonl, session_id, harness, workspace, project_key FROM sessions WHERE jsonl = ?`,
		jsonl,
	).Scan(&out.JSONL, &out.SessionID, &out.Harness, &out.Workspace, &out.Project)
	if err != nil {
		return Session{}, false
	}
	return out, true
}

func (s *Store) SessionByID(sessionID string) (Session, bool) {
	if sessionID == "" {
		return Session{}, false
	}
	var out Session
	err := s.DB.QueryRow(
		`SELECT jsonl, session_id, harness, workspace, project_key FROM sessions WHERE session_id = ? ORDER BY rowid DESC LIMIT 1`,
		sessionID,
	).Scan(&out.JSONL, &out.SessionID, &out.Harness, &out.Workspace, &out.Project)
	if err != nil {
		return Session{}, false
	}
	return out, true
}

func (s *Store) ListSessions() ([]Session, error) {
	rows, err := s.DB.Query(`SELECT jsonl, session_id, harness, workspace, project_key FROM sessions`)
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
