package gate

import "regexp"

// StatusReport is status narration rather than a failure of the
// project: a self-labeled known/flaky failure, a clean pass report, or a
// non-failure status. It answers the shape of the sentence only; it
// does not know whether anything the sentence names has failed before.
// That guard lives in the write layer (write.StatusReportSkip), which
// also owns StatusTallyFailing.
//
// It is deliberately evaluated only from the extract path — the
// read-time gate (StatusFailed and friends) is unchanged — so these
// sentences never store as failed.
func StatusReport(s string) bool {
	folded := statusProbe(s)
	if statusCoStatedFailure(folded) {
		return false
	}
	return statusFlakeLabel(folded) || statusPassReport(folded) || statusNonFailureRE.MatchString(folded)
}

// statusQuotedRE is a quoted span: a label inside quotes is a mention,
// not a use. A review finding that quotes "'the known failure',
// 'pre-existing'" as uncovered labels is about the predicate, not a
// flake report. Single quotes must open after a space or bracket and
// close before punctuation or a space, so an apostrophe ("isn't") is
// not a quote.
var statusQuotedRE = regexp.MustCompile(`"[^"]{1,300}"|\x{201C}[^\x{201D}]{1,300}\x{201D}|(^|[\s(\[:])'[^']{1,300}'([\s,.;:)\]!?]|$)`)

// statusHistoryRE is a failure count stated as history ("previously 2/5
// failed"): the report is about the fix, not a current failure.
var statusHistoryRE = regexp.MustCompile(`\bpreviously\s+\d+(\s*/\s*\d+)?\s+(tests?\s+)?fail\w*`)

// statusProbe folds a sentence and removes what is not the sentence's
// own claim: quoted spans and failure counts stated as history.
func statusProbe(s string) string {
	folded := Fold(s)
	folded = statusQuotedRE.ReplaceAllString(folded, "$1 $2")
	return statusHistoryRE.ReplaceAllString(folded, " ")
}

// statusFlakeLabelRE is a known/documented label within two tokens of
// the flake entity it labels ("the documented load-flake", "the known
// `export.spec.ts` bundled-font one", "the known pre-existing failure"),
// plus standalone flake phrases.
// The window stays at two tokens on purpose: "the known cause of the
// failure" and "the documented steps to reproduce the test failure"
// are root-cause narration and must keep extracting as failed.
// "unrelated" is a label only when it says what is unrelated ("unrelated
// to my change", "an unrelated test failure", "the failure is unrelated"):
// "a uniform failure across unrelated PRs" is a diagnosis.
var statusFlakeLabelRE = regexp.MustCompile(
	`\b(known|documented|pre-existing|preexisting)\s+([\w'.` + "`" + `-]+\s+){0,2}(test|tests|check|checks|case|cases|flake|flaky|one|issue|failure|failures)\b` +
		`|\bexpected (to )?fail\w*\b` +
		`|\bworktree-only\b` +
		`|only fail\w* in a worktree` +
		`|\bpass\w* in isolation\b` +
		`|\bignored per\b` +
		`|\bunrelated\s+to\b` +
		`|\b(is|are|was|were|looks|seems|seem)\s+(\w+\s+)?unrelated\b` +
		`|\bunrelated\s+([\w'.` + "`" + `-]+\s+){0,2}(test|tests|check|checks|case|cases|flake|flaky|failure|failures|issue|issues|error|errors|warning|warnings|suite|suites)\b` +
		`|\bpass\w*\s+(alone|standalone|when run alone)\b` +
		`|\brerun alone\b` +
		`|\bpass\w*\s+on\s+(a|the)\s+re-?run\b` +
		`|\boutside (my|this|the) (files|change|diff|scope)\b` +
		`|\b(isn't|is not|not) part of (this|my) (change|diff|pr)\b` +
		`|\bmy change (doesn't|does not) touch\b` +
		`|\bwatching it for flak\w*\b`)

// statusLabelProcedureRE is a label that modifies a procedure rather
// than a test: "the documented test steps to reproduce the auth
// failure" is reproduction narration, so the sentence keeps extracting
// as a real failure instead of dropping as a status report.
var statusLabelProcedureRE = regexp.MustCompile(
	`\b(known|documented|pre-existing|preexisting)\s+([\w'.` + "`" + `-]+\s+){0,2}(test|tests|check|checks|case|cases|issue)s?\s+` +
		`(steps?|procedure|procedures|instructions?|guide|walkthrough|notes)\b`)

// statusLabelFillerRE is punctuation a label can sit behind. The
// two-token window is built from word tokens, so an ellipsis, an arrow,
// a comma or a semicolon breaks it: "the failure is the known
// worktree-only … test" — a shape the store actually holds, and the
// main source of export.spec.ts pollution — did not match the same
// sentence without the filler.
var statusLabelFillerRE = regexp.MustCompile("…|→|,|;")

// statusRealLabelRE is a label that asserts the failure is real: "a
// real, documented failure" is a finding, not a known flake.
var statusRealLabelRE = regexp.MustCompile(`\b(real|genuine|actual|new)\s+(known|documented|pre-existing|preexisting)\b`)

// statusNotUnrelatedRE is a negated "unrelated": "the failure is not
// unrelated — my change broke it" owns the failure.
var statusNotUnrelatedRE = regexp.MustCompile(`\b(not|never|\w+n't)\s+unrelated\b`)

func statusFlakeLabel(s string) bool {
	probe := statusLabelFillerRE.ReplaceAllString(s, " ")
	probe = statusRealLabelRE.ReplaceAllString(probe, " ")
	probe = statusNotUnrelatedRE.ReplaceAllString(probe, " related ")
	if statusLabelProcedureRE.MatchString(probe) {
		return false
	}
	return statusFlakeLabelRE.MatchString(probe)
}

// statusCountRE is a pass, skip or zero-failure tally cell ("711
// passed", "16 skipped", "0 failed") plus a bare fraction ("5/5").
// Counts say nothing about which test failed, so pass-report matching
// strips them before looking for failure wording. A nonzero failure
// count is not stripped: "1 test fails on Safari; 20/20 e2e pass" is a
// failure report, not a clean pass.
var statusCountRE = regexp.MustCompile(`\b\d+\s*(tests?\s+)?(passed|skipped)\b|\b0\s*(tests?\s+)?fail\w*\b|\b\d+\s*/\s*\d+\b`)

// statusFailCountRE is a tally with at least one failure ("1 failed",
// "3 failures"). Zero-failure tallies are pass reports.
var statusFailCountRE = regexp.MustCompile(`\b[1-9]\d*\s*(tests?\s+)?fail\w*\b`)

// StatusTallyFailing is a test tally that includes a failure count: a
// failure count beside a runner name or beside a second tally cell.
// Shape only; the seen-before guard is the write layer's, see
// write.StatusReportSkip.
func StatusTallyFailing(s string) bool {
	folded := statusProbe(s)
	if !statusFailCountRE.MatchString(folded) {
		return false
	}
	// A tally that also traces a cause is a failure report: "1 failed:
	// grid-fit.test.ts, which I bisected to commit 9e452fc". Only the
	// root-cause and diagnosis shapes count here: a count followed by
	// "failed in <file>" is the tally itself, not a cause.
	if statusRootCause(folded) {
		return false
	}
	// Require a tally context: another tally cell (a passed/skipped
	// count) or a runner name beside the failure count. A bare
	// "suite" is not a runner and a bare fraction is not a cell, so
	// "3 failures after the merge" is a failure report, not a tally.
	return statusTallyContextRE.MatchString(folded)
}

var statusTallyContextRE = regexp.MustCompile(
	`\b\d+\s*(tests?\s+)?(passed|skipped)\b` +
		`|\b(vitest|playwright|jest|mocha|pytest|go test|npm test|npm run|tsc|cypress|ava|tap)\b`)

// statusPassWordRE is an explicit pass word. It is an alternation, not
// a stem: pass\w* also matched password, bypass, passthrough and
// compass, so sentences about credentials read as clean pass reports.
var statusPassWordRE = regexp.MustCompile(`\b(pass|passes|passed|passing|green|clean)\b`)

var statusPassVocabRE = regexp.MustCompile(
	`\b(build|builds|tests?|runs?|suites?|checks?|gates?|tsc|vitest|playwright|jest|npm|ci|everything|all|before/after|shards?)\b`)

// statusFailWordRE is failure wording outside the strippable counts. It
// is kept in step with the failure wording classify accepts
// (write.hardFailedRE), so "the new migrator doesn't work on Postgres
// 16" is not a clean pass report just because a pass is mentioned
// earlier in the same sentence.
var statusFailWordRE = regexp.MustCompile(
	`\b(fail\w*|broke|broken|crash\w*|threw|rejected|revert\w*|errors?` +
		`|didn't pass|doesn't pass|don't pass|won't pass|do not pass|does not pass|not passing` +
		`|didn't work|did not work|doesn't work|does not work|don't work|do not work` +
		`|didn't compile|doesn't compile|won't compile|does not compile|do not compile` +
		`|dead end)\b`)

// statusFailureCauseRE is a real failure stated with a preposition or a
// cause ("failed to compile", "fails with a nil pointer"). A
// self-labeled flake or a "no failures" status that also states one is
// still a failure report and must not drop: "No failures in the api
// suite, but src/auth/token.go failed to compile on 16."
//
// It is applied to a probe with the self-label's own wording removed, so
// "which only fails in a worktree" stays the self-label it is, and the
// narration copula ("the failure is the known … test") is deliberately
// not a match.
var statusFailureCauseRE = regexp.MustCompile(
	`\b(failed|fails|breaks|broke)\s+(with|to|on|in|after|because|when|under|during)\b`)

// statusRootCauseRE is a failure traced to its origin: "which I bisected
// to commit 9e452fc", "caused by the migration", "regressed in 2.3". A
// tally that says this is a failure report. Unlike statusFailureCauseRE
// it is safe on a tally: "1 failed in checkout.spec.ts" names where, not
// why.
var statusRootCauseRE = regexp.MustCompile(
	`\bbisect\w*\s+(it\s+)?to\b|\bcaused by\b|\bintroduced (by|in)\b|\bregress\w*\s+(in|at|since)\b`)

// statusDiagnosisRE is a failure explained by a named cause: "the
// handlers failure is a shared test-DB drift", "the build failure is a
// pre-existing code signing issue (missing Info.plist …)". The article
// is what separates a diagnosis ("is a … drift") from pointing at a
// known flake ("is the known … test"), and the cause nouns are the
// concrete ones: a bare "is a pre-existing issue" says nothing about
// why and still drops.
var statusDiagnosisRE = regexp.MustCompile(
	`\b(failure|failures|error|errors)\s+(is|was|are|were)\s+(a|an)\s+([\w'.` + "`" + `-]+\s+){0,4}` +
		`(drift|mismatch|regression|race|leak|(setup|harness|signing|config|configuration|environment|migration)\s+(issue|problem))\b`)

func statusCoStatedFailure(folded string) bool {
	if statusRootCause(folded) {
		return true
	}
	probe := statusCountRE.ReplaceAllString(folded, " ")
	probe = statusFlakeLabelRE.ReplaceAllString(probe, " ")
	return statusFailureCauseRE.MatchString(probe)
}

func statusRootCause(folded string) bool {
	probe := statusCountRE.ReplaceAllString(folded, " ")
	return statusDiagnosisRE.MatchString(probe) || statusRootCauseRE.MatchString(probe)
}

// statusPassReport is a pass report: pass/green/clean wording in a
// test/build context with no failure outside the strippable counts. "the
// tests fail but the build passes" has failure wording and keeps
// extracting.
func statusPassReport(s string) bool {
	folded := Fold(s)
	if StatusTallyFailing(folded) {
		return false
	}
	if !statusPassWordRE.MatchString(folded) || !statusPassVocabRE.MatchString(folded) {
		return false
	}
	rest := statusCountRE.ReplaceAllString(folded, " ")
	return !statusFailWordRE.MatchString(rest) && !statusPassNegationRE.MatchString(rest)
}

// statusPassNegationRE is a negation or contrast beside a pass: "the
// tests pass either way, but the button never receives the tap" is a
// lesson about what tests miss, not a clean pass report. "0 failed" and
// the other counts are stripped before this runs.
var statusPassNegationRE = regexp.MustCompile(`\b(never|not|no longer|cannot|\w+n't|but|unchanged|dead code)\b`)

// statusNonFailureRE is an explicit non-failure status.
var statusNonFailureRE = regexp.MustCompile(
	`\b(not failed|nothing failed|no failures|no new fail\w*|no blocked tickets|no (open|outstanding|blocked) (tickets|items|issues|prs|bugs|work))\b`)
