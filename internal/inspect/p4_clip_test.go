package inspect

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// inspect printed "…another \xe2\x80…": the 88-byte display clip cut an em
// dash in half, and grep then treated the whole report as binary.
func TestClipNeverSplitsARune(t *testing.T) {
	s := strings.Repeat("a", 87) + "—and the rest"
	got := clip(s, 88)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 87)) || !strings.HasSuffix(got, "…") {
		t.Fatalf("%q", got)
	}
	if clip("short", 88) != "short" {
		t.Fatal("short strings pass through")
	}
}
