package write

import (
	"strings"
	"testing"
)

// stripHarnessChrome must locate tags case-insensitively without
// changing byte offsets: lowercasing İ (2→1 bytes) or K (3→1) shifted
// the slice and panicked or mangled text.
func TestStripHarnessChromeNonASCII(t *testing.T) {
	cases := map[string]string{
		"Ⱥ<system-reminder>x</system-reminder>":                                         "Ⱥ",
		"İstanbul deploy notes <system-reminder>hidden</system-reminder> we decided X.": "İstanbul deploy notes  we decided X.",
		"Kelvin <SYSTEM-REMINDER>hidden</SYSTEM-REMINDER> stays":                        "Kelvin  stays",
	}
	for in, want := range cases {
		got := stripHarnessChrome(in)
		if got != want {
			t.Errorf("stripHarnessChrome(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(got, "hidden") {
			t.Errorf("chrome survived in %q", got)
		}
	}
}

// Pi injects skill bodies wrapped in <skill name=… location=…>…</skill>
// and <skills>. Open tags with attributes must strip; lookalike tags and
// user-typed angle brackets must survive.
func TestStripSkillSpans(t *testing.T) {
	cases := map[string]string{
		`before <skill name="lossless" location="/x/SKILL.md">Never undo a decision.</skill> after`: "before  after",
		`<skills><skill>a</skill><skill name="k" location="l">b</skill></skills>kept`:               "kept",
		`<SKILL NAME="x" LOCATION="y">hidden</SKILL>`:                                               "",
		`<skillful>docs</skillful>`:                             "<skillful>docs</skillful>",
		`unclosed <skill name="x">drops to end`:                 "unclosed",
		`İstanbul <skill name="x">hidden</skill> we decided X.`: "İstanbul  we decided X.",
	}
	for in, want := range cases {
		got := stripHarnessChrome(in)
		if got != want {
			t.Errorf("stripHarnessChrome(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(got, "hidden") && !strings.Contains(in, "skillful") {
			t.Errorf("skill chrome survived in %q", got)
		}
	}
}
