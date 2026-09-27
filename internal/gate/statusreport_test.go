package gate

import "testing"

// The misfire shapes are distilled from the post-0.1.32 active `failed`
// records in the live store (2026-09-23T00:47Z onward), redacted. The
// negatives are the shapes that must keep extracting as failed.
func TestStatusReportDropsMisfires(t *testing.T) {
	for _, s := range []string{
		// gate summaries with a failure count
		"Gates: tsc clean; npm test 882 passed/16 skipped/0 failed; npm run build clean; playwright 39 passed/1 skipped/1 failed (export.spec.ts headline-line-count, unrelated to any change here)",
		"Playwright: 26 passed, 1 failed; the failure is the known export.spec.ts bundled-font one.",
		// a self-labeled known / flaky failure
		"The failure is the bundled font check in export.spec, which only fails in a worktree; the same test passed on the main tree.",
		"The one expected failure is the known bundled-font test in the worktree; everything else passes.",
		"One test for the area-picking logic failed once and then passed on a re-run; that code isn't part of this change, so I'm watching it for flakiness.",
		// aggregate pass reports
		"5/5 consecutive full npm test runs pass under load",
		// non-failure statuses
		"CI is pending (not failed) on all open PRs",
		// a label behind an ellipsis: the store's own shape. The
		// two-token window cannot consume "…", so the same sentence
		// dropped only when the ellipsis was absent.
		"the failure is the known worktree-only … test",
		"the failure is the known bundled-font test in export.spec.ts",
		"Playwright had 26 passed and 1 failed: the known worktree-only bundled-font test",
		// self-labels the design names
		"the known pre-existing failure in export.spec.ts",
		"auth.spec.ts is expected to fail on arm64",
	} {
		if !StatusReport(s) {
			t.Errorf("misfire not a status report: %q", s)
		}
	}
}

func TestStatusReportKeepsRealFailures(t *testing.T) {
	for _, s := range []string{
		"three tests fail, and the commit went through anyway — the pipe hid the failure",
		"the recurring tool-call failure is the image-read cap",
		"auth.spec.ts fails because the refresh token expired",
		// root-cause narration the label words must not swallow
		"the known cause of the failure is the expired cert",
		"the documented steps to reproduce the test failure",
		// reproduction narration: the label modifies the steps, not a test
		"the documented test steps to reproduce the auth failure",
		// a pass mentioned beside a real failure, in wording hardFailedRE
		// reads as a failure and statusFailWordRE used to miss
		"The build passes, but the new migrator doesn't work on Postgres 16.",
		"CI is green; internal/dsl/parse.go doesn't compile on arm64.",
		// a self-labeled flake beside a co-stated real failure
		"No failures in the api suite, but src/auth/token.go failed to compile on 16.",
		"The flake is unrelated to the deploy, but src/db/pool.go failed to connect on the retry.",
		"The known flaky suite aside, AuthService.refresh failed with a nil pointer in staging.",
		// failure wording with a count that is not a tally: the fraction
		// used to supply the tally context, so these were tallies
		"1 test fails on Safari; 20/20 e2e pass.",
		"3 failures after the merge, and the api suite never recovered",
		// pass wording plus a real failure: the contraction "don't pass"
		// is failure wording, like "didn't pass" and "doesn't pass".
		"The auth unit tests don't pass in src/middleware/auth.test.ts.",
	} {
		if StatusReport(s) {
			t.Errorf("real failure read as a status report: %q", s)
		}
	}
}

func TestStatusTallyFailing(t *testing.T) {
	for _, s := range []string{
		"Test summary: npm test 697 passed / 15 skipped / 1 failed (tests/sim/sim-seeded.test.ts, the documented load-flake), rerun alone 6/6 passed.",
		"vitest 2 passed/1 failed (auth.spec.ts, the token refresh breaks)",
		"npm test 882 passed/16 skipped/0 failed; playwright 39 passed/1 skipped/1 failed (export.spec.ts headline-line-count)",
		"The full vitest run has 1 failure, in `grid-fit.test.ts`.",
	} {
		if !StatusTallyFailing(s) {
			t.Errorf("failing tally not recognized: %q", s)
		}
	}
	for _, s := range []string{
		// a zero-failure tally is a pass report, not a failing tally
		"tsc clean; vitest 711 passed/16 skipped/0 failed; build green",
		// failure wording with no tally context
		"three tests fail, and the commit went through anyway",
		"the recurring tool-call failure is the image-read cap",
		// a bare suite name is not a runner, and a fraction alone is not
		// a second tally cell
		"1 test fails on Safari; 20/20 e2e pass.",
		"3 failures after the merge, and the api suite never recovered",
	} {
		if StatusTallyFailing(s) {
			t.Errorf("non-tally read as a failing tally: %q", s)
		}
	}
}

// The self-label's own wording is narration, and the narration copula
// must not read as a co-stated failure. Both shapes are store records
// that have to keep dropping.
func TestStatusReportNarrationIsNotAFailure(t *testing.T) {
	for _, s := range []string{
		"the failure is the known worktree-only bundled-font check in export.spec.ts",
		"Playwright had 26 passed and 1 failed: the known worktree-only bundled-font test",
	} {
		if !StatusReport(s) {
			t.Errorf("narration read as a real failure: %q", s)
		}
	}
}

// pass\w* also matched password, bypass and passthrough, so a sentence
// about credentials was a clean pass report.
func TestStatusPassReportRejectsPassWordStems(t *testing.T) {
	for _, s := range []string{
		"the passthrough tests are wired in the deploy",
		"the password rotation runs on the deploy host",
	} {
		if statusPassReport(s) {
			t.Errorf("pass-word stem read as a pass report: %q", s)
		}
	}
	for _, s := range []string{
		"the api tests pass in isolation",
		"the deploy runs and everything is clean",
	} {
		if !statusPassReport(s) {
			t.Errorf("pass report missed: %q", s)
		}
	}
}

// Shapes the first version of the predicate dropped from a live store,
// redacted. Each is a finding, not a status report.
func TestStatusReportKeepsFindings(t *testing.T) {
	for _, s := range []string{
		// a lesson about what tests miss: a pass beside a negation
		"A Compose IconButton over the PlayerView never receives the tap on the emulator; the Robolectric tests of the button in isolation pass either way.",
		"All albums showed green placeholder covers after the round-trip; Back landed on the list, not Home.",
		// a finding the label word qualifies as real
		"The searches surfaced a real, documented failure: a token pool whose funds were stuck because the canister ran out of cycles.",
		// "unrelated" that labels nothing
		"A fast, uniform failure across unrelated PRs points at a shared early gate, not six independent bugs.",
		// a diagnosis: the failure is a named cause
		"The handlers failure is a shared test-DB drift (`version 102`: this checkout only has migrations through 101), unrelated to my change.",
		"The build failure is a pre-existing code signing issue (missing Info.plist for the test target), not related to my change.",
		// labels quoted as text, not used
		`major: statusreport.go - 'the known failure', 'pre-existing', 'expected to fail' are uncovered self-labels.`,
	} {
		if StatusReport(s) {
			t.Errorf("finding read as a status report: %q", s)
		}
	}
}

func TestStatusReportDropsFlakeQualifiers(t *testing.T) {
	for _, s := range []string{
		"It passes when run alone, so the failure was a timing flake from the shared machine.",
		"Vitest: 684 passed, 1 failed - a sim test timing out under load; it passes alone.",
		"The TokenStoreTests failure is a pre-existing issue (not related to my changes).",
		"This is a pre-existing backend test failure, unrelated to my changes.",
		// a failure count stated as history is part of a pass report
		"After the fix: 5/5 consecutive full npm test runs pass under the same load (717 passed each run, previously 2/5 failed).",
	} {
		if !StatusReport(s) {
			t.Errorf("status report missed: %q", s)
		}
	}
}

// A tally that states a cause is a failure report.
func TestStatusTallyWithCauseIsAFailure(t *testing.T) {
	s := "vitest 381 passed, 1 failed: `tests/render/grid-fit.test.ts`, which I bisected to commit 9e452fc."
	if StatusTallyFailing(s) || StatusReport(s) {
		t.Fatalf("bisected failure read as status: %q", s)
	}
}

// "N failed in <file>" is the tally, not a cause: the repeat must stay a
// failing tally so the seen-before guard can drop it.
func TestStatusTallyFailedInIsATally(t *testing.T) {
	for _, s := range []string{
		"vitest: 40 passed, 1 failed in checkout.spec.ts",
		"playwright 12 passed, 2 failed on webkit (checkout.spec.ts)",
	} {
		if !StatusTallyFailing(s) {
			t.Errorf("tally not recognized: %q", s)
		}
	}
}

func TestStatusReportPredicativeUnrelated(t *testing.T) {
	for _, s := range []string{
		"Playwright: 26 passed, 1 failed; the failure is unrelated.",
		"The e2e failure in checkout.spec.ts is unrelated.",
		"That timeout looks unrelated.",
	} {
		if !StatusReport(s) {
			t.Errorf("predicative unrelated missed: %q", s)
		}
	}
}

func TestStatusReportNegatedUnrelatedOwnsTheFailure(t *testing.T) {
	for _, s := range []string{
		"The checkout.spec.ts failure is not unrelated: my change broke it.",
		"That timeout was never unrelated.",
		"The e2e failure isn't unrelated.",
	} {
		if StatusReport(s) {
			t.Errorf("negated unrelated read as a label: %q", s)
		}
	}
}
