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
		"react-query",  // kebab package name
		"date-fns",     // kebab package name
		"x-api-key",    // kebab header name
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
		"HTTP",         // bare acronym: caller's call, not code shape
		"list",
	}
	for _, s := range no {
		if CodeShaped(s) {
			t.Errorf("CodeShaped(%q) = true, want false", s)
		}
	}
}
