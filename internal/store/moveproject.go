package store

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
)

// MoveResult counts what MoveProject changed.
type MoveResult struct {
	From, Into string
	Moved      int // records now under Into, any status
	Active     int // of those, still active
	Deduped    int // active records retired because Into already held the same claim
	Sessions   int
}

// MoveProject puts every record, session and action under from into
// into. One checkout can end up under several keys: a path-hash key
// from before it had a remote, then an old owner/repo after a rename.
// Ask reads one key, so it saw only part of the repo's history.
//
// The claim hash includes the project key, so each moved record is
// rehashed. An active record whose claim into already holds is retired
// (superseded, row kept) and the record already in into stays. Raw tapes
// stay where they are: excerpts are found by session, not by project.
//
// Rows move in one transaction (dryRun rolls it back and reports the
// counts). Exports are rewritten after it commits; a failure there leaves
// the index right and the export stale.
func (s *Store) MoveProject(from, into string, dryRun bool) (MoveResult, error) {
	from, into = projectkey.Normalize(from), projectkey.Normalize(into)
	out := MoveResult{From: from, Into: into}
	if from == "" || into == "" || from == into {
		return out, fmt.Errorf("move needs two different project keys")
	}
	type row struct {
		id, typ, text, status string
		key                   string // the key as stored, for the old export
		newHash               string
	}
	load := func(q sqlQuerier, keys []string) ([]row, error) {
		rows, err := q.Query(`SELECT id, type, text, status, project_key FROM records WHERE project_key IN (?`+
			strings.Repeat(",?", len(keys)-1)+`) ORDER BY created_at, id`, anys(keys)...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var list []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.typ, &r.text, &r.status, &r.key); err != nil {
				return nil, err
			}
			r.newHash = claim.Hash(into, r.typ, r.text)
			list = append(list, r)
		}
		return list, rows.Err()
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var moved []row
	var retired []string
	var keys []string
	err := withBusyRetry(func() error {
		moved, retired, keys = nil, nil, nil
		out.Moved, out.Active, out.Deduped, out.Sessions = 0, 0, 0, 0
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// An older release may have stored the key in another form
		// ("Acme/API"). Rows under it are from's too; left behind, the
		// next backfill would write the old key back.
		keys, err = storedKeys(tx, from)
		if err != nil {
			return err
		}
		list, err := load(tx, keys)
		if err != nil {
			return err
		}
		// Hashes active under into, including ones moved earlier in this
		// loop: two from-records with one claim keep the older.
		held := map[string]bool{}
		for _, r := range list {
			if r.status != "active" {
				continue
			}
			if !held[r.newHash] {
				var n int
				if err := tx.QueryRow(`SELECT count(*) FROM records WHERE project_key = ? AND claim_hash = ? AND status = 'active'`, into, r.newHash).Scan(&n); err != nil {
					return err
				}
				held[r.newHash] = n > 0
			}
		}
		seen := map[string]bool{}
		for i := range list {
			r := &list[i]
			if r.status == "active" {
				if held[r.newHash] || seen[r.newHash] {
					r.status = "superseded"
					retired = append(retired, r.id)
					out.Deduped++
				} else {
					seen[r.newHash] = true
					out.Active++
				}
			}
			if _, err := tx.Exec(`UPDATE records SET project_key = ?, claim_hash = ?, status = ? WHERE id = ?`, into, r.newHash, r.status, r.id); err != nil {
				return err
			}
			out.Moved++
		}
		for _, key := range keys {
			for _, t := range []string{"path_postings", "symbol_postings", "claim_vectors", "actions"} {
				if _, err := tx.Exec(`UPDATE OR IGNORE `+t+` SET project_key = ? WHERE project_key = ?`, into, key); err != nil {
					return err
				}
				if _, err := tx.Exec(`DELETE FROM `+t+` WHERE project_key = ?`, key); err != nil {
					return err
				}
			}
			res, err := tx.Exec(`UPDATE sessions SET project_key = ? WHERE project_key = ?`, into, key)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			out.Sessions += int(n)
		}
		if dryRun {
			return nil // deferred Rollback undoes it
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		moved = list
		return nil
	})
	if err != nil || dryRun {
		return out, err
	}
	if out.Sessions > 0 {
		s.sessionsGen.Add(1)
	}
	// Retired rows leave the search index; their export goes with them.
	for _, id := range retired {
		if err := s.reindex(claim.Record{ID: id, Status: "superseded"}); err != nil {
			return out, err
		}
		_ = s.DeleteVector(id)
	}
	for _, r := range moved {
		if old, err := s.confinedFile("export", projectkey.Encode(r.key), r.id+".md"); err == nil {
			_ = os.Remove(old)
		}
		// A superseded record keeps its export too, as rewriteStatus does.
		rec, ok := s.Get(r.id)
		if !ok {
			continue
		}
		if err := s.writeFile(rec); err != nil {
			return out, err
		}
	}
	for _, key := range keys {
		if dir, err := s.confinedDir("export", projectkey.Encode(key)); err == nil {
			_ = os.Remove(dir) // only when empty
		}
	}
	return out, nil
}

// storedKeys is from plus every key the records and sessions tables hold
// that normalizes to it.
func storedKeys(q sqlQuerier, from string) ([]string, error) {
	keys := []string{from}
	for _, t := range []string{"records", "sessions"} {
		rows, err := q.Query(`SELECT DISTINCT project_key FROM ` + t + ` WHERE project_key IS NOT NULL`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return nil, err
			}
			if !slices.Contains(keys, k) && projectkey.Normalize(k) == from {
				keys = append(keys, k)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func anys(list []string) []any {
	out := make([]any, len(list))
	for i, v := range list {
		out[i] = v
	}
	return out
}

type sqlQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}
