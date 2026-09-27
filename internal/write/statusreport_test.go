package write

import (
	"strings"
	"testing"
)

func seenAll(string) bool { return true }

func seenNone(string) bool { return false }

func TestTallyNames(t *testing.T) {
	got := tallyNames("Test summary: npm test 697 passed / 15 skipped / 1 failed (tests/sim/sim-seeded.test.ts, the documented load-flake), rerun alone 6/6 passed.")
	want := "tests/sim/sim-seeded.test.ts"
	if !contains(got, want) {
		t.Fatalf("missing %q in %v", want, got)
	}
	for _, n := range got {
		if n != strings.ToLower(n) {
			t.Fatalf("name not lowercased: %q", n)
		}
	}
	if dup := uniq(got); len(dup) != len(got) {
		t.Fatalf("duplicate names: %v", got)
	}
}

func TestTallyNamesSingleTokenBacktick(t *testing.T) {
	got := tallyNames("Playwright: 25 passed, 1 failed - a different `hud.spec` test on each run.")
	if !contains(got, "hud.spec") {
		t.Fatalf("backtick name not extracted: %v", got)
	}
	// A multi-token code span is prose about code, not a failing name.
	if got := tallyNames("vitest 1 failed, see `npm test --watch auth`"); contains(got, "npm test --watch auth") {
		t.Fatalf("multi-token span read as a name: %v", got)
	}
}

func TestStatusReportSkipDropsReport(t *testing.T) {
	reason, drop := StatusReportSkip("CI is pending (not failed) on all open PRs", seenNone)
	if !drop || reason != "status-report" {
		t.Fatalf("reason=%q drop=%v", reason, drop)
	}
}

// The seen-before guard covers every status-report shape, not only
// tallies: a self-labeled flake whose test has never failed stores once,
// and drops on the repeats.
func TestStatusReportSkipGuardsEveryShape(t *testing.T) {
	s := "the failure is the known bundled-font test in export.spec.ts"
	seen := func(name string) bool { return name == "export.spec.ts" }
	if reason, drop := StatusReportSkip(s, seen); !drop || reason != "status-report" {
		t.Errorf("known flake with a seen name: reason=%q drop=%v", reason, drop)
	}
	if reason, drop := StatusReportSkip(s, seenNone); drop {
		t.Errorf("first report of an unseen failure dropped as %q: %q", reason, s)
	}
	if reason, drop := StatusReportSkip(s, nil); drop {
		t.Errorf("unverifiable name dropped as %q: %q", reason, s)
	}
}

// A co-stated real failure keeps the sentence even when the flake
// wording and every name in it are already known.
func TestStatusReportSkipKeepsCoStatedFailure(t *testing.T) {
	for _, s := range []string{
		"No failures in the api suite, but src/auth/token.go failed to compile on 16.",
		"The flake is unrelated to the deploy, but src/db/pool.go failed to connect on the retry.",
		"The known flaky suite aside, AuthService.refresh failed with a nil pointer in staging.",
		"1 test fails on Safari; 20/20 e2e pass.",
		"3 failures after the merge, and the api suite never recovered",
	} {
		if reason, drop := StatusReportSkip(s, seenAll); drop {
			t.Errorf("real failure dropped as %q: %q", reason, s)
		}
	}
}

func TestStatusReportSkipDropsTallyWhenAllNamesSeen(t *testing.T) {
	s := "Test summary: npm test 697 passed / 15 skipped / 1 failed (tests/sim/sim-seeded.test.ts, the documented load-flake), rerun alone 6/6 passed."
	reason, drop := StatusReportSkip(s, seenAll)
	if !drop || reason == "" {
		t.Fatalf("reason=%q drop=%v", reason, drop)
	}
}

func TestStatusReportSkipKeepsFirstTimeTally(t *testing.T) {
	s := "vitest 2 passed/1 failed (auth.spec.ts, the token refresh breaks)"
	if reason, drop := StatusReportSkip(s, seenNone); drop {
		t.Fatalf("first-time tally dropped as %q", reason)
	}
	// A nil callback (inspect --jsonl replay has no store) keeps tallies.
	if reason, drop := StatusReportSkip(s, nil); drop {
		t.Fatalf("tally dropped without a store as %q", reason)
	}
}

func TestStatusReportSkipKeepsTallyWithUnseenName(t *testing.T) {
	s := "npm test 882 passed/16 skipped/0 failed; playwright 39 passed/1 skipped/1 failed (export.spec.ts headline-line-count)"
	seen := func(name string) bool { return name != "export.spec.ts" }
	if reason, drop := StatusReportSkip(s, seen); drop {
		t.Fatalf("tally with an unseen name dropped as %q", reason)
	}
}

// A bare failing count says nothing about whether the failure is known,
// so it stores unless the sentence labels it; a labeled one drops.
func TestStatusReportSkipNamelessTally(t *testing.T) {
	for _, s := range []string{
		"vitest 711 passed, 1 failed",
		"Checks run in a scratch worktree: tsc clean, build passes, vitest has 1 failure, playwright has 18 failures.",
	} {
		if reason, drop := StatusReportSkip(s, seenAll); drop {
			t.Errorf("unlabeled nameless tally dropped as %q: %q", reason, s)
		}
	}
	for _, s := range []string{
		"Vitest: 684 passed, 1 failed - a sim test timing out under load; it passes alone.",
		"playwright 26 passed/1 failed (the documented worktree-only font case, ignored per common rules)",
	} {
		if _, drop := StatusReportSkip(s, seenNone); !drop {
			t.Errorf("labeled nameless tally kept: %q", s)
		}
	}
}

// Abbreviations, short words, and assignments are not test names; "e.g"
// used to match nearly every stored failure and mark a first report seen.
func TestTallyNamesDropsNonNames(t *testing.T) {
	got := tallyNames("Robolectric tests pass either way (e.g. an ImageButton); confirmed via `ask` with `PW_PORT=5360`.")
	for _, bad := range []string{"e.g", "ask", "pw_port=5360"} {
		if contains(got, bad) {
			t.Errorf("non-name %q in %v", bad, got)
		}
	}
}

func TestStatusReportSkipKeepsRealFailures(t *testing.T) {
	for _, s := range []string{
		"three tests fail, and the commit went through anyway - the pipe hid the failure",
		"the recurring tool-call failure is the image-read cap",
	} {
		if reason, drop := StatusReportSkip(s, seenAll); drop {
			t.Errorf("real failure dropped as %q: %q", reason, s)
		}
	}
	// A first-time failure with a cause still stores.
	if _, drop := StatusReportSkip("The recurring auth.spec.ts failure is the expired refresh token.", seenNone); drop {
		t.Fatal("first-time failure with a cause dropped")
	}
}

func TestExtractDropsStatusReports(t *testing.T) {
	body := "Gates: tsc clean; npm test 882 passed/16 skipped/0 failed; npm run build clean; " +
		"playwright 39 passed/1 skipped/1 failed (export.spec.ts headline-line-count, unrelated to any change here).\n" +
		"CI is pending (not failed) on all open PRs.\n" +
		"The recurring auth.spec.ts failure is the expired refresh token."
	got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}},
		ExtractOpts{ProjectKey: "acme/api", FailedNameSeen: seenAll})
	if !liftedFailed(got, "auth.spec.ts") {
		t.Fatalf("real failure not extracted as failed: %s", recBlob(got))
	}
	for _, r := range got {
		if strings.Contains(r.Text, "playwright") || strings.Contains(r.Text, "CI is pending") {
			t.Fatalf("status report stored as %s: %q", r.Type, r.Text)
		}
	}
}

// The first report of a failing test stores even when it is worded as a
// status report; only the repeats drop.
func TestExtractKeepsFirstTimeStatusReport(t *testing.T) {
	body := "playwright 39 passed/1 skipped/1 failed (export.spec.ts headline-line-count, unrelated to any change here)."
	msgs := []Message{{Role: "assistant", Text: body, Offset: 1}}
	if !liftedFailed(Extract(msgs, ExtractOpts{ProjectKey: "acme/api", FailedNameSeen: seenNone}), "export.spec.ts") {
		t.Fatal("first report of a first-time failure dropped")
	}
	if liftedFailed(Extract(msgs, ExtractOpts{ProjectKey: "acme/api", FailedNameSeen: seenAll}), "export.spec.ts") {
		t.Fatal("repeat of an already-failed test stored")
	}
}

func TestExtractDropsTallyWhenNamesSeen(t *testing.T) {
	body := "Test summary: npm test 697 passed / 15 skipped / 1 failed " +
		"(tests/sim/sim-seeded.test.ts, the documented load-flake), rerun alone 6/6 passed."
	if liftedFailed(Extract([]Message{{Role: "assistant", Text: body, Offset: 1}},
		ExtractOpts{ProjectKey: "acme/api", FailedNameSeen: seenAll}), "sim-seeded") {
		t.Fatal("tally of an already-failed test stored")
	}
	if !liftedFailed(Extract([]Message{{Role: "assistant", Text: body, Offset: 1}},
		ExtractOpts{ProjectKey: "acme/api", FailedNameSeen: seenNone}), "sim-seeded") {
		t.Fatal("tally of a first-time failing test dropped")
	}
}

func TestExtractNilSeenKeepsTally(t *testing.T) {
	// inspect --jsonl replay has no store: without a callback tallies store.
	body := "Test summary: npm test 697 passed / 15 skipped / 1 failed " +
		"(tests/sim/sim-seeded.test.ts, the documented load-flake), rerun alone 6/6 passed."
	if !liftedFailed(Extract([]Message{{Role: "assistant", Text: body, Offset: 1}},
		ExtractOpts{ProjectKey: "acme/api"}), "sim-seeded") {
		t.Fatal("tally dropped with a nil callback")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// A doc cited beside the failing test does not make a repeat report look
// new, but a source file beside it does: it may be a new failure.
func TestStatusReportSkipCountsOnlyTestNames(t *testing.T) {
	s := "playwright 26 passed, 1 failed (the known worktree-only font flake in export.spec.ts called out in plan-notes.md)"
	seen := func(name string) bool { return name == "export.spec.ts" }
	if _, drop := StatusReportSkip(s, seen); !drop {
		t.Fatalf("repeat report kept for its doc name: %q", s)
	}
	// With no test name, a non-test name still guards.
	s = "The build failure is pre-existing (a unicode escape in `ProfileView.swift:77`, unrelated to my changes)."
	if _, drop := StatusReportSkip(s, seenNone); drop {
		t.Fatalf("first report of a named build failure dropped: %q", s)
	}
}

func TestStatusReportSkipSeesSourceBesideKnownTest(t *testing.T) {
	s := "The known flaky checkout.spec.ts failed again; src/billing/invoice.go now panics on nil."
	seen := func(name string) bool { return name == "checkout.spec.ts" }
	if _, drop := StatusReportSkip(s, seen); drop {
		t.Fatalf("new source failure beside a known test dropped: %q", s)
	}
}
