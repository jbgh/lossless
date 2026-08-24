package redact

import "testing"

func TestAdv191ScratchPathFilter(t *testing.T) {
	got := FilterPaths([]string{
		"src/tmp/limiter.ts",
		"docs/qa-report.md",
		"qa-report.md",
		"tmp/x.png",
		"/tmp/phone-qa/qa-report.md",
	})
	if len(got) != 2 || got[0] != "src/tmp/limiter.ts" || got[1] != "docs/qa-report.md" {
		t.Fatalf("keep src/tmp + docs/qa-report.md, drop exact/tmp/abs: %v", got)
	}
	got = FilterPaths([]string{"qa-report.md", "tmp/x.png"})
	if len(got) != 0 {
		t.Fatalf("exact qa-report.md and tmp/x.png kept: %v", got)
	}
	got = FilterPaths([]string{"docs/qa-report.md"})
	if len(got) != 1 || got[0] != "docs/qa-report.md" {
		t.Fatalf("docs/qa-report.md dropped: %v", got)
	}
	got = FilterPaths([]string{"src/tmp/limiter.ts"})
	if len(got) != 1 || got[0] != "src/tmp/limiter.ts" {
		t.Fatalf("src/tmp/limiter.ts dropped: %v", got)
	}
}
