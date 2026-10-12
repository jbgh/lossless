package store

import "lossless/internal/projectkey"

// SaidOpenings lists (session, opening) of a project's active said
// records, where opening is the first runes characters of the turn: all
// the template check reads.
func (s *Store) SaidOpenings(project string, runes int) ([][2]string, error) {
	rows, err := s.DB.Query(
		`SELECT session_id, substr(text, 1, ?) FROM records WHERE project_key = ? AND type = 'said' AND status = 'active'`,
		runes, projectkey.Normalize(project))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var sess, text string
		if err := rows.Scan(&sess, &text); err != nil {
			return nil, err
		}
		out = append(out, [2]string{sess, text})
	}
	return out, rows.Err()
}

// KnownHash reports whether any record, active or retired, carries hash.
// Reading old tapes again must not bring back what was superseded.
func (s *Store) KnownHash(hash string) bool {
	var one int
	err := s.DB.QueryRow(`SELECT 1 FROM records WHERE claim_hash = ? LIMIT 1`, hash).Scan(&one)
	return err == nil
}
