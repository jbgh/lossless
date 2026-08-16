package store

import (
	"encoding/json"
	"strings"
	"time"

	"lossless/internal/projectkey"
)

const (
	ActionAsk      = "ask"
	ActionGet      = "get"
	ActionRemember = "remember"
	ActionWarn     = "warn"
)

type Action struct {
	ProjectKey string
	SessionID  string
	Kind       string
	ClaimID    string
	Paths      []string
	Tokens     []string
	At         string
}

func (s *Store) AppendActions(acts []Action) error {
	if len(acts) == 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO actions(project_key, session_id, kind, claim_id, paths_json, tokens_json, at) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	seenSess := map[string]bool{}
	for _, a := range acts {
		a.ProjectKey = projectkey.Normalize(a.ProjectKey)
		if a.ProjectKey == "" || a.Kind == "" || a.At == "" {
			continue
		}
		if a.SessionID == "" {
			a.SessionID = "default"
		}
		pj, _ := json.Marshal(a.Paths)
		if a.Paths == nil {
			pj = []byte("[]")
		}
		tj, _ := json.Marshal(a.Tokens)
		if a.Tokens == nil {
			tj = []byte("[]")
		}
		if _, err := stmt.Exec(a.ProjectKey, a.SessionID, a.Kind, a.ClaimID, string(pj), string(tj), a.At); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return err
		}
		seenSess[a.ProjectKey+"\x00"+a.SessionID] = true
	}
	_ = stmt.Close()
	for key := range seenSess {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		if _, err := tx.Exec(`
DELETE FROM actions WHERE project_key = ? AND session_id = ? AND id NOT IN (
  SELECT id FROM actions WHERE project_key = ? AND session_id = ? ORDER BY at DESC, id DESC LIMIT ?
)`, parts[0], parts[1], parts[0], parts[1], 40); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RecentActions(project, session string, limit int) ([]Action, error) {
	project = projectkey.Normalize(project)
	if limit <= 0 {
		return nil, nil
	}
	var (
		rows interface {
			Next() bool
			Scan(dest ...any) error
			Err() error
			Close() error
		}
		err error
	)
	if session != "" {
		rows, err = s.DB.Query(`
SELECT project_key, session_id, kind, claim_id, paths_json, tokens_json, at
FROM actions WHERE project_key = ? AND session_id = ?
ORDER BY at DESC, id DESC LIMIT ?`, project, session, limit)
	} else {
		rows, err = s.DB.Query(`
SELECT project_key, session_id, kind, claim_id, paths_json, tokens_json, at
FROM actions WHERE project_key = ?
ORDER BY at DESC, id DESC LIMIT ?`, project, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var a Action
		var pj, tj string
		if err := rows.Scan(&a.ProjectKey, &a.SessionID, &a.Kind, &a.ClaimID, &pj, &tj, &a.At); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pj), &a.Paths)
		_ = json.Unmarshal([]byte(tj), &a.Tokens)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) NewestSessionID(project string) string {
	project = projectkey.Normalize(project)
	var id string
	_ = s.DB.QueryRow(`SELECT session_id FROM actions WHERE project_key = ? ORDER BY at DESC, id DESC LIMIT 1`, project).Scan(&id)
	if id != "" {
		return id
	}
	_ = s.DB.QueryRow(`SELECT session_id FROM sessions WHERE project_key = ? ORDER BY rowid DESC LIMIT 1`, project).Scan(&id)
	return id
}

func (s *Store) RecordDwell(project, session, claimID string) {
	if claimID == "" {
		return
	}
	if project == "" {
		if rec, ok := s.Get(claimID); ok {
			project = rec.ProjectKey
		}
	}
	project = projectkey.Normalize(project)
	if project == "" {
		return
	}
	if session == "" {
		session = s.NewestSessionID(project)
	}
	if session == "" {
		session = "default"
	}
	var paths []string
	if rec, ok := s.Get(claimID); ok {
		paths = rec.Paths
	}
	_ = s.AppendActions([]Action{{
		ProjectKey: project,
		SessionID:  session,
		Kind:       ActionGet,
		ClaimID:    claimID,
		Paths:      paths,
		At:         time.Now().UTC().Format(time.RFC3339),
	}})
}
