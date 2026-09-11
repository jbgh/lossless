package gate

import "testing"

// Live residue on 2026-09-11: 31 rows shaped <summary>…</summary>, six
// XCTest runner lines, and five clip chops. None is a claim.
func TestTagWrappedNotificationIsChrome(t *testing.T) {
	for _, s := range []string{
		`<summary>Background command "Wait for main 4536; check the #3804 re-review" failed with exit code 1</summary>`,
		`<summary>Background command "Start filtered logcat for A5" failed with exit code 255</summary>`,
		`<status>completed</status>`,
	} {
		if !SkipProse(s) {
			t.Fatalf("should skip: %q", s)
		}
	}
	if SkipProse("The <summary> tag in docs/ask.md failed to render on the site.") {
		t.Fatal("a sentence mentioning a tag is prose")
	}
}

func TestRunnerOutputIsNotAClaim(t *testing.T) {
	for _, s := range []string{
		"Executed 1 test, with 1 failure (0 unexpected) in 24.117 (24.119) seconds",
		"Test Suite 'LightboxHeartUITests' failed at 2026-09-04 20:27:22.636.",
		"Test Case '-[MemoraUITests.LightboxHeartUITests testHeart]' failed (24.1 seconds).",
		"--- FAIL: TestCatchUpIngestsFileOver64MB (0.02s)",
		"FAIL\tlossless/internal/write\t5.108s",
		"npm ERR! code ELIFECYCLE",
	} {
		if !RunnerOutput(s) || !SkipProse(s) {
			t.Fatalf("runner output should skip: %q", s)
		}
	}
	for _, s := range []string{
		"Redis token bucket failed in src/middleware/auth.ts staging.",
		"The lightbox test failed on device because familyId is never passed from Home.",
		"iOS package tests FAILED: MemoraNetworking",
		"Main 4536 failed npm-audit again after the hotfix passed on 4533.",
	} {
		if RunnerOutput(s) {
			t.Fatalf("prose flagged as runner output: %q", s)
		}
	}
}

func TestLeadingChopVariantsSkip(t *testing.T) {
	for _, s := range []string{
		"e verbObjRE's `[a-z]` capture already assumes it), and longer-term thread the folded string through.",
		"n LLM process; no bank has published LLM-signal crowding numbers; Bloomberg has never explained it.",
		"ve origin/main` + the PR diff applied with `patch`), never the worktree; the step was replayed verbatim.",
		"al/bench_test.go 0.95 floor this manifests as an inexplicable one-off CI failure.",
		"or `sharedCodeIdent` (claim.Tokens never yields hyphens), the ungrounded-decision check sits after.",
		"tially but restrict the pack to five records.",
	} {
		if !SkipProse(s) {
			t.Fatalf("chop should skip: %q", s)
		}
	}
	for _, s := range []string{
		"a failed build blocks the release train.",
		"i think redis failed here because of the token bucket.",
		"env exists; do not print secrets",
		"qa-fix-loop failed on the second pass in src/qa.ts",
		"go test failed in internal/write because of the lock.",
		"os/exec failed on the runner with exit status 1.",
		"io/fs walk failed on the symlink in testdata.",
		"we decided to use jose, not jsonwebtoken, for Edge.",
		"ok so redis failed in staging again.",
	} {
		if leadingWordChop(s) {
			t.Fatalf("real sentence flagged as chop: %q", s)
		}
	}
}
