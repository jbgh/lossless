package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"lossless/internal/claim"
	"lossless/internal/projectkey"

	_ "modernc.org/sqlite"
)

type Excerpt struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ProjectKey  string `json:"project_key"`
	StartOffset int64  `json:"start_offset"`
	EndOffset   int64  `json:"end_offset"`
	Text        string `json:"text"`
}

func ExcerptID(sessionID string, start, end int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", sessionID, start, end)))
	return hex.EncodeToString(sum[:])
}

func (s *Store) excerptPath(month string) string {
	return filepath.Join(s.Root, "index", "excerpts-"+month+".sqlite")
}

func (s *Store) openExcerpt(month string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", s.excerptPath(month))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS excerpts (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  project_key TEXT NOT NULL,
  start_offset INTEGER NOT NULL,
  end_offset INTEGER NOT NULL,
  text TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ex_sess ON excerpts(session_id, start_offset, end_offset);
`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Store) WriteExcerpts(month string, xs []Excerpt) error {
	if len(xs) == 0 {
		return nil
	}
	if month == "" {
		month = time.Now().UTC().Format("2006-01")
	}
	db, err := s.openExcerpt(month)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO excerpts(id, session_id, project_key, start_offset, end_offset, text) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, x := range xs {
		if x.ID == "" {
			x.ID = ExcerptID(x.SessionID, x.StartOffset, x.EndOffset)
		}
		if _, err := stmt.Exec(x.ID, x.SessionID, projectkey.Normalize(x.ProjectKey), x.StartOffset, x.EndOffset, x.Text); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

type RecordView struct {
	claim.Record
	Excerpt string `json:"excerpt,omitempty"`
}

func (s *Store) View(id string) (RecordView, bool) {
	rec, ok := s.Get(id)
	if !ok {
		return RecordView{}, false
	}
	v := RecordView{Record: rec}
	if rec.TranscriptRef != nil {
		created, _ := time.Parse(time.RFC3339, rec.CreatedAt)
		if ex, ok := s.ExcerptCovering(rec.TranscriptRef, created); ok {
			v.Excerpt = ex.Text
		}
	}
	return v, true
}

func (s *Store) ExcerptCovering(ref *claim.TranscriptRef, hint time.Time) (Excerpt, bool) {
	if ref == nil || ref.SessionID == "" {
		return Excerpt{}, false
	}
	months := excerptMonths(hint)
	for _, month := range months {
		ex, ok := s.lookupExcerpt(month, ref)
		if ok {
			return ex, true
		}
	}
	return Excerpt{}, false
}

func excerptMonths(hint time.Time) []string {
	if hint.IsZero() {
		hint = time.Now().UTC()
	}
	var out []string
	for i := 0; i < 13; i++ {
		t := hint.AddDate(0, -i, 0)
		out = append(out, t.Format("2006-01"))
	}
	return out
}

func (s *Store) lookupExcerpt(month string, ref *claim.TranscriptRef) (Excerpt, bool) {
	db, err := sql.Open("sqlite", s.excerptPath(month))
	if err != nil {
		return Excerpt{}, false
	}
	defer db.Close()
	var x Excerpt
	err = db.QueryRow(`
SELECT id, session_id, project_key, start_offset, end_offset, text
FROM excerpts
WHERE session_id = ? AND start_offset <= ? AND end_offset >= ?
ORDER BY (end_offset - start_offset) ASC
LIMIT 1`, ref.SessionID, ref.StartOffset, ref.StartOffset).Scan(
		&x.ID, &x.SessionID, &x.ProjectKey, &x.StartOffset, &x.EndOffset, &x.Text)
	if err != nil {
		return Excerpt{}, false
	}
	return x, true
}
