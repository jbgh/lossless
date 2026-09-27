package store

import (
	"strings"

	"lossless/internal/projectkey"
)

// FailedNameSeen reports whether an earlier failed record of the project
// mentions the name. excludeID skips a record's own row, so the first
// report of a failing test is not its own evidence. before, when set, is
// the asking record's created_at: only records created before it (id
// order breaks a tie) count, so of several reports of the same failure
// the earliest one has nothing before it and survives the prune sweep.
// Without the bound every report found the others and all of them were
// retired, the first real report included. An empty before (capture
// time, where the sentence is newer than every stored row) counts every
// row.
//
// Matching is exact and on word boundaries, not LIKE: _ and % are LIKE
// wildcards, so LIKE '%sim_seeded.test.ts%' also matches a row naming
// "simXseeded.test.ts". instr() narrows the rows, then the boundary
// check drops a match inside a longer word ("grid-fit.ts" is not a
// mention of "fit.ts").
//
// Any status counts: a superseded record still says the test has failed
// before. Like RecordsForRecurrence, this reads the store directly
// instead of through the read-time extract gates, so a gate-named failed
// still counts.
func (s *Store) FailedNameSeen(project, name, excludeID, before string) (bool, error) {
	if name == "" {
		return false, nil
	}
	// SQLite's lower() folds ASCII only. An ASCII name is narrowed with
	// instr() and compared ASCII-folded, matching lower(); a name with
	// non-ASCII letters ("Überweisung.test.ts") skips the prefilter and
	// is compared Unicode-folded on both sides.
	ascii := isASCII(name)
	q := `SELECT text FROM records
		WHERE project_key = ? AND type = 'failed' AND id != ?
		AND (? = '' OR created_at < ? OR (created_at = ? AND id < ?))`
	args := []any{projectkey.Normalize(project), excludeID, before, before, before, excludeID}
	if ascii {
		q += ` AND instr(lower(text), lower(?)) > 0`
		args = append(args, name)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	fold := strings.ToLower
	if ascii {
		fold = asciiLower
	}
	want := fold(name)
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return false, err
		}
		if mentionsWord(fold(text), want) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// mentionsWord reports whether name occurs in text with no identifier
// character on either side. A hyphen joins file-name words
// ("sim-seeded.test.ts"), so it counts as one.
func mentionsWord(text, name string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], name)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(name)
		if (start == 0 || !identByte(text[start-1])) && (end == len(text) || !identByte(text[end])) {
			return true
		}
		i = start + 1
	}
}

func identByte(b byte) bool {
	return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
