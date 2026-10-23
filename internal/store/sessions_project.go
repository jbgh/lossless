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

// SessionsVersion moves on every insert, update or delete of a session
// row, by any process and any release from 0.1.44 on: triggers on the
// sessions table bump it (see migrate). The watcher reads it each tick
// to tell whether its cached session list is stale; SessionsGen sees
// only this Store's writes.
func (s *Store) SessionsVersion() (int64, error) {
	var v int64
	err := s.DB.QueryRow(`SELECT version FROM table_versions WHERE name = 'sessions'`).Scan(&v)
	return v, err
}

// legacyFor returns the stored session keys, other than project itself,
// that normalize to project. The scan of distinct keys runs until it
// succeeds once per Store: every row written since uses the normalized
// form, so the set does not grow. A scan that fails (the database busy
// under a writer) is tried again on the next call.
func (s *Store) legacyFor(project string) []any {
	s.legacyMu.Lock()
	defer s.legacyMu.Unlock()
	if !s.legacyLoaded {
		if m, err := s.scanLegacyKeys(); err == nil {
			s.legacyKeys, s.legacyLoaded = m, true
		}
	}
	var out []any
	for _, k := range s.legacyKeys[project] {
		out = append(out, k)
	}
	return out
}

// scanLegacyKeys is a test hook over readLegacyKeys.
var scanLegacyKeysHook func(*Store) (map[string][]string, error)

func (s *Store) scanLegacyKeys() (map[string][]string, error) {
	if scanLegacyKeysHook != nil {
		return scanLegacyKeysHook(s)
	}
	return s.readLegacyKeys()
}

func (s *Store) readLegacyKeys() (map[string][]string, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT project_key FROM sessions WHERE project_key IS NOT NULL AND project_key != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string][]string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		if n := projectkey.Normalize(k); n != k && n != "" {
			m[n] = append(m[n], k)
		}
	}
	return m, rows.Err()
}
