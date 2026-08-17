package store

import (
	"os"
	"path/filepath"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
)

func (s *Store) ListAllActive() ([]claim.Record, error) {
	rows, err := s.DB.Query(`SELECT ` + claimCols + ` FROM records WHERE status = 'active' ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claim.Record
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRecord(id string) error {
	rec, ok := s.Get(id)
	if !ok {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	err := withBusyRetry(func() error {
		if _, err := s.DB.Exec(`DELETE FROM records WHERE id = ?`, id); err != nil {
			return err
		}
		if err := s.reindex(claim.Record{ID: id, Status: "superseded"}); err != nil {
			return err
		}
		_ = s.DeleteVector(id)
		return nil
	})
	if err != nil {
		return err
	}
	enc := projectkey.Encode(rec.ProjectKey)
	if enc != "" && enc != "unknown" && SafeRecordID(rec.ID) {
		_ = os.Remove(filepath.Join(s.Root, "export", enc, rec.ID+".md"))
	}
	return nil
}

func (s *Store) Supersede(id string) error {
	return s.rewriteStatus(id, "superseded")
}

func (s *Store) DeleteSession(jsonl string) error {
	if jsonl == "" {
		return nil
	}
	if _, err := s.DB.Exec(`DELETE FROM sessions WHERE jsonl = ?`, jsonl); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM cursors WHERE path = ?`, jsonl)
	return err
}

func (s *Store) DeleteActions(project string) error {
	project = projectkey.Normalize(project)
	if project == "" {
		return nil
	}
	_, err := s.DB.Exec(`DELETE FROM actions WHERE project_key = ?`, project)
	return err
}

func (s *Store) RemoveProjectRaw(project string) error {
	enc := projectkey.Encode(project)
	if enc == "" || enc == "unknown" {
		return nil
	}
	_ = os.RemoveAll(filepath.Join(s.Root, "raw", enc))
	_ = os.RemoveAll(filepath.Join(s.Root, "export", enc))
	return nil
}
