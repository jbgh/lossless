package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if dest, err := s.confinedFile("export", projectkey.Encode(rec.ProjectKey), rec.ID+".md"); err == nil {
		_ = os.Remove(dest)
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
	if dest, err := s.confinedDir("raw", enc); err == nil {
		_ = os.RemoveAll(dest)
	}
	if dest, err := s.confinedDir("export", enc); err == nil {
		_ = os.RemoveAll(dest)
	}
	return nil
}

func (s *Store) confinedDir(elem ...string) (string, error) {
	return confinedPath(s.Root, true, elem...)
}

func (s *Store) confinedFile(elem ...string) (string, error) {
	return confinedPath(s.Root, false, elem...)
}

func confinedPath(root string, dir bool, elem ...string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	for _, e := range elem {
		if e == "" || e == "." || e == ".." || strings.Contains(e, "..") ||
			strings.Contains(e, "/") || strings.Contains(e, string(filepath.Separator)) {
			return "", fmt.Errorf("refusing path element %q", e)
		}
	}
	dest := filepath.Join(append([]string{rootAbs}, elem...)...)
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, destAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes home")
	}
	fi, err := os.Lstat(destAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return destAbs, nil
		}
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing symlink")
	}
	if dir && !fi.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	if !dir && !fi.Mode().IsRegular() && fi.Mode()&os.ModeSymlink == 0 && fi.IsDir() {
		return "", fmt.Errorf("not a file")
	}
	return destAbs, nil
}
