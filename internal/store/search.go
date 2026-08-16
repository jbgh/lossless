package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
)

type FTSHit struct {
	ID   string
	BM25 float64
}

func (s *Store) CountActive() int {
	var n int
	_ = s.DB.QueryRow(`SELECT count(*) FROM records WHERE status = 'active'`).Scan(&n)
	return n
}

func (s *Store) GetMany(ids []string) (map[string]claim.Record, error) {
	out := make(map[string]claim.Record, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const chunk = 200
	for i := 0; i < len(ids); i += chunk {
		end := i + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[i:end]
		ph := strings.Repeat("?,", len(part))
		ph = ph[:len(ph)-1]
		args := make([]any, len(part))
		for j, id := range part {
			args[j] = id
		}
		rows, err := s.DB.Query(`SELECT `+claimCols+` FROM records WHERE id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out[r.ID] = r
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) SearchFTS(project, match string, limit int) ([]FTSHit, error) {
	if match == "" || limit <= 0 {
		return nil, nil
	}
	project = projectkey.Normalize(project)
	rows, err := s.DB.Query(`
SELECT f.record_id, bm25(records_fts)
FROM records_fts f
JOIN records r ON r.id = f.record_id
WHERE records_fts MATCH ? AND r.project_key = ? AND r.status = 'active'
ORDER BY bm25(records_fts)
LIMIT ?`, match, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FTSHit
	seen := map[string]bool{}
	for rows.Next() {
		var h FTSHit
		if err := rows.Scan(&h.ID, &h.BM25); err != nil {
			return nil, err
		}
		if seen[h.ID] {
			continue
		}
		seen[h.ID] = true
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) IDsByPath(project string, keys []string, per, total int) ([]string, error) {
	return s.idsByPosting(`path_postings`, `path_key`, project, keys, per, total)
}

func (s *Store) IDsBySymbol(project string, keys []string, per, total int) ([]string, error) {
	return s.idsByPosting(`symbol_postings`, `symbol`, project, keys, per, total)
}

func (s *Store) idsByPosting(table, col, project string, keys []string, per, total int) ([]string, error) {
	project = projectkey.Normalize(project)
	seen := map[string]bool{}
	var out []string
	if per <= 0 || total <= 0 {
		return nil, nil
	}
	q := fmt.Sprintf(`
SELECT p.record_id FROM %s p
JOIN records r ON r.id = p.record_id
WHERE p.project_key = ? AND p.%s = ? AND r.status = 'active'
LIMIT ?`, table, col)
	for _, k := range keys {
		if k == "" || len(out) >= total {
			break
		}
		rows, err := s.DB.Query(q, project, k, per)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
			if len(out) >= total {
				break
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) IDsByType(project, typ, since string, limit int) ([]string, error) {
	project = projectkey.Normalize(project)
	if limit <= 0 {
		return nil, nil
	}
	var rows interface {
		Next() bool
		Scan(dest ...any) error
		Err() error
		Close() error
	}
	var err error
	if since != "" {
		rows, err = s.DB.Query(`
SELECT id FROM records
WHERE project_key = ? AND status = 'active' AND type = ? AND created_at >= ?
ORDER BY created_at DESC
LIMIT ?`, project, typ, since, limit)
	} else {
		rows, err = s.DB.Query(`
SELECT id FROM records
WHERE project_key = ? AND status = 'active' AND type = ?
ORDER BY created_at DESC
LIMIT ?`, project, typ, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) DecisionIDsOverlapping(project string, pathKeys, symbols []string, limit int) ([]string, error) {
	return s.TypeIDsOverlapping(project, "decision", pathKeys, symbols, limit)
}

func (s *Store) ConstraintIDsOverlapping(project string, pathKeys, symbols []string, limit int) ([]string, error) {
	return s.TypeIDsOverlapping(project, "constraint", pathKeys, symbols, limit)
}

func (s *Store) TypeIDsOverlapping(project, typ string, pathKeys, symbols []string, limit int) ([]string, error) {
	if limit <= 0 || (len(pathKeys) == 0 && len(symbols) == 0) {
		return nil, nil
	}
	project = projectkey.Normalize(project)
	seen := map[string]bool{}
	var out []string
	add := func(ids []string) {
		for _, id := range ids {
			if seen[id] || len(out) >= limit {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(pathKeys) > 0 {
		ids, err := s.idsByPostingType(`path_postings`, `path_key`, project, typ, pathKeys, 40, limit)
		if err != nil {
			return nil, err
		}
		add(ids)
	}
	if len(out) < limit && len(symbols) > 0 {
		ids, err := s.idsByPostingType(`symbol_postings`, `symbol`, project, typ, symbols, 40, limit)
		if err != nil {
			return nil, err
		}
		add(ids)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) idsByPostingType(table, col, project, typ string, keys []string, per, total int) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	q := fmt.Sprintf(`
SELECT p.record_id FROM %s p
JOIN records r ON r.id = p.record_id
WHERE p.project_key = ? AND p.%s = ? AND r.status = 'active' AND r.type = ?
LIMIT ?`, table, col)
	for _, k := range keys {
		if k == "" || len(out) >= total {
			break
		}
		rows, err := s.DB.Query(q, project, k, typ, per)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
			if len(out) >= total {
				break
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) HeadPriorityIDs(project string, failedN, decisionN, constraintN int) ([]string, error) {
	project = projectkey.Normalize(project)
	seen := map[string]bool{}
	var out []string
	add := func(typ string, n int) error {
		if n <= 0 {
			return nil
		}
		ids, err := s.IDsByType(project, typ, "", n)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		return nil
	}
	if err := add("failed", failedN); err != nil {
		return nil, err
	}
	if err := add("decision", decisionN); err != nil {
		return nil, err
	}
	if err := add("constraint", constraintN); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ColdPriorityIDs(project string, limit int) ([]string, error) {
	project = projectkey.Normalize(project)
	if limit <= 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, typ := range []string{"failed", "decision", "constraint"} {
		if len(out) >= limit {
			break
		}
		ids, err := s.IDsByType(project, typ, "", limit-len(out))
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *Store) RecentPaths(project string, limit int) ([]string, error) {
	project = projectkey.Normalize(project)
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(`
SELECT paths_json FROM records
WHERE project_key = ? AND status = 'active'
ORDER BY created_at DESC
LIMIT ?`, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var paths []string
		if err := json.Unmarshal([]byte(raw), &paths); err != nil {
			continue
		}
		for _, p := range claim.PathKeys(paths) {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) reindex(rec claim.Record) error {
	if _, err := s.DB.Exec(`DELETE FROM records_fts WHERE record_id = ?`, rec.ID); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`DELETE FROM path_postings WHERE record_id = ?`, rec.ID); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`DELETE FROM symbol_postings WHERE record_id = ?`, rec.ID); err != nil {
		return err
	}
	if rec.Status != "active" {
		return nil
	}
	var body strings.Builder
	body.WriteString(rec.Text)
	for _, sy := range rec.Symbols {
		body.WriteByte(' ')
		body.WriteString(sy)
	}
	for _, k := range claim.PathKeys(rec.Paths) {
		body.WriteByte(' ')
		body.WriteString(k)
		if st := claim.Stem(k); st != "" && st != k {
			body.WriteByte(' ')
			body.WriteString(st)
		}
	}
	if _, err := s.DB.Exec(`INSERT INTO records_fts(body, record_id) VALUES(?, ?)`, body.String(), rec.ID); err != nil {
		return err
	}
	for _, k := range claim.PathKeys(rec.Paths) {
		if _, err := s.DB.Exec(
			`INSERT OR IGNORE INTO path_postings(project_key, path_key, record_id) VALUES(?,?,?)`,
			rec.ProjectKey, k, rec.ID,
		); err != nil {
			return err
		}
	}
	for _, sy := range rec.Symbols {
		key := strings.ToLower(sy)
		if key == "" {
			continue
		}
		if _, err := s.DB.Exec(
			`INSERT OR IGNORE INTO symbol_postings(project_key, symbol, record_id) VALUES(?,?,?)`,
			rec.ProjectKey, key, rec.ID,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillIndex() error {
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM records_fts`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var recs int
	if err := s.DB.QueryRow(`SELECT count(*) FROM records`).Scan(&recs); err != nil {
		return err
	}
	if recs == 0 {
		return nil
	}
	rows, err := s.DB.Query(`SELECT ` + claimCols + ` FROM records`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return err
		}
		if err := s.reindex(r); err != nil {
			return err
		}
	}
	return rows.Err()
}
