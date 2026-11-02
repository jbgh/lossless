package gate

import "regexp"

// RedStep is a test-first report: a new test failing on purpose before
// the fix ("Every new test failed first and then passed", "RED: 12
// failed against the old code"). The failure was the plan, so it is not
// a lesson for the next agent. Like StatusReport it is evaluated only on
// the extract path.
//
// A strong marker (failed first, a leading RED:, captured RED, src
// stashed) is enough. A weak one ("before the fix", "against the old
// code") also reads as a post-mortem ("The auth test failed before the
// fix: the token cache ignored the tenant id"), so it needs a new test or
// a pass after it. Either way the sentence names a test, and a stated
// cause (bisected to, caused by, because, a colon after the failure)
// keeps it as a failure.
func RedStep(s string) bool {
	folded := statusProbe(s)
	if !redStepTestRE.MatchString(folded) || redStepCaused(folded) {
		return false
	}
	if redStrongRE.MatchString(folded) {
		return true
	}
	return redWeakRE.MatchString(folded) && redResolvedRE.MatchString(folded)
}

// redStrongRE is wording only a test-first report uses. RED counts only
// as a TDD step: leading the sentence (RED:, **RED was**) or in the
// fixed phrases below, never as a CI color ("CI red:", "saw red").
var redStrongRE = regexp.MustCompile(
	`^[\s*#>-]*red(\*\*)?\s*(:|/|was\b|run\b|before\b|first\b|\d)` +
		`|\bred\s*/\s*green\b|\bred-green\b|\(red\)|\(red before\b` +
		`|\b(captured|capture|capturing|recovered)\s+(the\s+)?red\b|\bseen red\b|\bthe red run\b|\bred was\b` +
		`|\bfail\w*\s+first\s*([.,;:()]|$)|\bwritten first and failed\b` +
		`|\bsrc\s+(stashed|reverted|moved aside)\b|\bwatched it fail\b`)

// redWeakRE is test-first wording that a post-mortem also uses.
var redWeakRE = regexp.MustCompile(
	`\b(against|on|with)\s+the\s+(old|original|previous|unfixed|pre-fix|prior)\s+(code|source|sources|src|implementation|markup|pages?|[\w-]+\.(py|js|ts|tsx|jsx|go|swift|kt|rb|rs|java|mjs|html|css))\b` +
		`|\bbefore\s+(the|any|my)\s+(fix|fixes|change|changes|implementation|code)\b` +
		`|\bpre-fix\b` +
		// these read as a post-mortem too ("failed first with a timeout",
		// "red on CI and green locally") unless a new test or a pass follows
		`|\bfail\w*\s+first\s+(and|then|against|before|with)\b|\bred\b[^.;]{0,60}\bgreen\b`)

// redResolvedRE is the other half of a test-first report: the test is
// new, or it passes after.
var redResolvedRE = regexp.MustCompile(
	`\bnew\s+([\w'-]+\s+){0,3}(tests?|cases?|specs?|checks?)\b` +
		`|\bpass(es|ed)?\s+now\b|\bnow\s+pass(es|ed)?\b|\bthen\s+pass(es|ed)?\b|\band\s+pass(es|ed)?\b` +
		`|\bpin\w*\s+the\s+fix\w*\b`)

// redStepTestRE says the failing thing is a test.
var redStepTestRE = regexp.MustCompile(`\b(tests?|suites?|specs?|pytest|jest|vitest|playwright|cases?)\b|^[\s*#>-]*red\b|\b(captured|capture|capturing|recovered|seen)\s+(the\s+)?red\b|\bpass(es|ed)?\s+now\b|\bthen\s+pass(es|ed)?\b`)

// redCauseRE is a cause stated outright: "failed first because the
// migration …", "failed before the fix: the token cache ignored …".
var redCauseRE = regexp.MustCompile(`\bbecause\b|\bdue to\b|\bfail(ed|s)? to\b|\bfail\w*\b[^:]{0,80}:\s+\S`)

// redLeadRE is a label whose colon is not a cause: a leading "RED:" and
// "Watched it fail:".
var redLeadRE = regexp.MustCompile(`^[\s*#>-]*red(\*\*)?\s*:(\*\*)?|\bwatched it fail:`)

func redStepCaused(folded string) bool {
	if statusRootCause(folded) {
		return true
	}
	return redCauseRE.MatchString(redLeadRE.ReplaceAllString(folded, " "))
}
