package store

import (
	"os"
	"path/filepath"
	"sort"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
)

type ProjectStats struct {
	Key      string
	Active   int
	ByType   map[string]int
	Sessions int
	RawBytes int64
	RawFiles int
}

type CursorRow struct {
	Path   string
	Cursor int64
	Size   int64
	Status string // ok, behind, past-eof, missing
}

func (s *Store) ListProjectStats() ([]ProjectStats, error) {
	rows, err := s.DB.Query(`
SELECT project_key, type, COUNT(*) FROM records
WHERE status = 'active' GROUP BY project_key, type`)
	if err != nil {
		return nil, err
	}
	by := map[string]*ProjectStats{}
	for rows.Next() {
		var key, typ string
		var n int
		if err := rows.Scan(&key, &typ, &n); err != nil {
			rows.Close()
			return nil, err
		}
		p := by[key]
		if p == nil {
			p = &ProjectStats{Key: key, ByType: map[string]int{}}
			by[key] = p
		}
		p.Active += n
		p.ByType[typ] = n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	srows, err := s.DB.Query(`SELECT COALESCE(project_key,''), COUNT(*) FROM sessions GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var key string
		var n int
		if err := srows.Scan(&key, &n); err != nil {
			srows.Close()
			return nil, err
		}
		if key == "" {
			continue
		}
		p := by[key]
		if p == nil {
			p = &ProjectStats{Key: key, ByType: map[string]int{}}
			by[key] = p
		}
		p.Sessions = n
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return nil, err
	}
	srows.Close()

	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ProjectStats, 0, len(keys))
	for _, k := range keys {
		p := *by[k]
		p.RawBytes, p.RawFiles = s.rawUsage(k)
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) CountByType(project string) (map[string]int, error) {
	project = projectkey.Normalize(project)
	rows, err := s.DB.Query(`
SELECT type, COUNT(*) FROM records
WHERE project_key = ? AND status = 'active' GROUP BY type`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			return nil, err
		}
		out[typ] = n
	}
	return out, rows.Err()
}

func (s *Store) ListRecentActive(project string, limit int) ([]claim.Record, error) {
	project = projectkey.Normalize(project)
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(`SELECT `+claimCols+` FROM records
WHERE project_key = ? AND status = 'active'
ORDER BY created_at DESC, id DESC LIMIT ?`, project, limit)
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

func (s *Store) ListCursors() ([]CursorRow, error) {
	rows, err := s.DB.Query(`SELECT path, offset FROM cursors ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CursorRow
	for rows.Next() {
		var c CursorRow
		if err := rows.Scan(&c.Path, &c.Cursor); err != nil {
			return nil, err
		}
		fi, err := os.Stat(c.Path)
		if err != nil {
			c.Status = "missing"
		} else {
			c.Size = fi.Size()
			switch {
			case c.Cursor > c.Size:
				c.Status = "past-eof"
			case c.Cursor < c.Size:
				c.Status = "behind"
			default:
				c.Status = "ok"
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) rawUsage(project string) (bytes int64, files int) {
	enc := projectkey.Encode(project)
	if enc == "" || enc == "unknown" {
		return 0, 0
	}
	root := filepath.Join(s.Root, "raw", enc)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		files++
		bytes += info.Size()
		return nil
	})
	return bytes, files
}
