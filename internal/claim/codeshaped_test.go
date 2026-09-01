package claim

import "testing"

func TestCodeShaped(t *testing.T) {
	yes := []string{
		"tokenBucket",  // camelCase
		"parse_codex",  // snake
		"v0.1.21",      // version
		"auth.ts",      // dotted
		"cmd/lossless", // slashed
		"jwt",          // package alias
		"sqlite3",      // digit
		"Re-Check",     // hyphen with leading capital
		"x-api-key",    // kebab with a short segment
		"sqlite3-wal",  // kebab with a digit
	}
	for _, s := range yes {
		if !CodeShaped(s) {
			t.Errorf("CodeShaped(%q) = false, want true", s)
		}
	}
	no := []string{
		"staging",      // plain word
		"Redis",        // sentence-case proper noun, not code shape
		"re-proposing", // hyphenated English
		"follow-up",    // noun-particle English
		"trade-off",
		"read-only",
		"react-query", // plain kebab: grounded by its verb (standardizing on), not by shape
		"date-fns",
		"HTTP", // bare acronym: caller's call, not code shape
		"list",
	}
	for _, s := range no {
		if CodeShaped(s) {
			t.Errorf("CodeShaped(%q) = true, want false", s)
		}
	}
}
