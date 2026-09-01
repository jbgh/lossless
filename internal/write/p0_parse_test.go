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
