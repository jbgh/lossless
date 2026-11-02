package gate

import "regexp"

// RedStep is a test-first report: a new test failing on purpose before
// the fix ("Every new test failed first and then passed", "RED: 12
// failed against the old code"). The failure was the plan, so it is not
// a lesson for the next agent. Like StatusReport it is evaluated only on
// the extract path. A sentence that also traces a root cause ("bisected
// to", "caused by", "regressed in") still stores, and so does one with no
// test in it ("the upload failed for every user before the fix landed").
func RedStep(s string) bool {
	folded := redColorRE.ReplaceAllString(statusProbe(s), " ")
	if !redStepRE.MatchString(folded) || !redStepTestRE.MatchString(folded) {
		return false
	}
	return !statusRootCause(folded) && !redStepCauseRE.MatchString(folded)
}

// redStepRE is test-first wording. "red" counts only as a TDD step (RED:,
// RED was, captured RED, red/green), not as a CI color ("CI is red",
// "goes red").
var redStepRE = regexp.MustCompile(
	`(^|[^\w])red\s*(/|-|and\s+)\s*green\b` +
		`|(^|[*\s(])red(\*\*)?\s*(:|\)|was\b|run\b|runs\b|before\b|first\b|step\b|phase\b|against\b|counts?\b|\d)` +
		`|\b(capture|captured|capturing|recovered|saw|seen|see|got|confirmed)\s+(the\s+)?red\b` +
		`|\bfail\w*\s+first\b` +
		`|\b(against|on|with)\s+the\s+(old|original|previous|unfixed|pre-fix|prior)\s+(code|source|sources|src|implementation|version|build|markup|pages?|files?|branch|binary|[\w-]+\.(py|js|ts|tsx|jsx|go|swift|kt|rb|rs|java|mjs|html|css))\b` +
		`|\bbefore\s+(the|any|my)\s+(fix|fixes|change|changes|implementation|code)\b` +
		`|\bsrc\s+(stashed|reverted|moved aside)\b` +
		`|\bpre-fix\b|\bwatched it fail\b`)

// redColorRE is red as a CI or pipeline color ("CI #2161 is red:", "goes
// red on a loaded machine"). It is removed before redStepRE looks.
var redColorRE = regexp.MustCompile(`\b(is|are|was|were|goes|went|go|turned|turns|shows|show|stays|stay|still)\s+red\b`)

// redStepTestRE says the failing thing is a test: a test-first report
// names one, or says it passes now.
var redStepTestRE = regexp.MustCompile(`\b(tests?|suites?|specs?|pytest|node|jest|vitest|playwright|red)\b|\bpass(es|ed)?\s+now\b|\bthen\s+pass(es|ed)?\b`)

// redStepCauseRE is a cause stated outright beside the red step: "failed
// first because the migration …" is a finding, not the plan.
var redStepCauseRE = regexp.MustCompile(`\bbecause\b`)
