package write

import (
	"regexp"
	"strings"

	"lossless/internal/gate"
)

// backtickSpanRE matches a code span and its content. Only single-token
// spans name a test: `hud.spec` is a failing test, `npm test --watch auth`
// is prose about a command.
var backtickSpanRE = regexp.MustCompile("`([^`]+)`")

// tallyNames returns the lowercased, unique names a failing tally
// mentions: paths and file stems the sentence names, plus single-token
// code spans. It is the same identifier surface recurrence clusters on
// (paths plus backticked names), read at capture time.
func tallyNames(sentence string) []string {
	found := findPaths(sentence)
	for _, m := range backtickSpanRE.FindAllStringSubmatch(sentence, -1) {
		if len(m) < 2 {
			continue
		}
		span := strings.TrimSpace(m[1])
		if span == "" || strings.ContainsAny(span, " \t") {
			continue
		}
		found = append(found, span)
	}
	out := make([]string, 0, len(found))
	for _, n := range found {
		n = strings.ToLower(n)
		if !testName(n) {
			continue
		}
		out = append(out, n)
	}
	return uniq(out)
}

// testName drops what the path and stem extractors lift out of prose
// but that names no test: abbreviations ("e.g"), short words ("ask"), and
// NAME=value assignments ("PW_PORT=5360"). A short name also matches too
// much of the store to be evidence that a test has failed before.
func testName(n string) bool {
	if len([]rune(n)) < 4 || strings.Contains(n, "=") {
		return false
	}
	switch strings.TrimSuffix(n, ".") {
	case "e.g", "i.e", "etc", "vs", "cf", "n.b":
		return false
	}
	return true
}

// citedDocRE is a name the sentence cites rather than one that can fail:
// plan and notes docs, data fixtures, screenshots, logs.
var citedDocRE = regexp.MustCompile(`\.(md|mdx|txt|json|jsonl|ya?ml|csv|log|png|jpe?g|gif|svg|html?)(:\d+)?$`)

// guardNames drops cited docs when the sentence names anything else: a
// gate summary that points at "plan-notes.md" or "scene-base.json" beside
// the failing test is still a repeat when the test has failed before.
// Source and script files stay: "src/billing/invoice.go now panics" next
// to a known flaky test is a new failure, and the guard must see it.
func guardNames(names []string) []string {
	var keep []string
	for _, n := range names {
		if !citedDocRE.MatchString(n) {
			keep = append(keep, n)
		}
	}
	if len(keep) > 0 {
		return keep
	}
	return names
}

// StatusReportSkip reports whether a sentence that classified as failed
// is really a status report, and the skip reason to trace. seen answers
// "has this failing test name failed before?"; a nil seen (inspect
// --jsonl replay has no store) keeps any shape that names something.
//
// The guard is the same for every status-report shape: the sentence is
// dropped only when it names nothing at all, or when every name it
// mentions has failed before in this project. One exception: a bare
// failing tally ("playwright has 18 failures") that names nothing and
// carries no known/flaky label stores. A count alone cannot say the
// failure is known, and the first report of a new failure is often
// exactly that line. A known flake is already
// captured by its first real report, so the label repeats are what go;
// the first report of a brand-new failure stores even when it is worded
// as a status report, which is the only reliable way to tell "the
// failure is the known worktree-only bundled-font check" (drop) from
// "The known flaky suite aside, AuthService.refresh failed with a nil
// pointer" (keep).
//
// This guard-for-all-shapes rule is a deliberate deviation from the
// design doc's unconditional-drop wording for non-tally shapes; the
// integration step updates the design doc.
func StatusReportSkip(sentence string, seen func(name string) bool) (string, bool) {
	var reason string
	switch {
	case gate.StatusReport(sentence):
		reason = "status-report"
	case gate.StatusTallyFailing(sentence):
		reason = "status-tally"
	default:
		return "", false
	}
	names := guardNames(tallyNames(sentence))
	if len(names) == 0 {
		if reason == "status-tally" {
			return "", false
		}
		return reason, true
	}
	if seen == nil {
		return "", false
	}
	for _, name := range names {
		if !seen(name) {
			return "", false
		}
	}
	return reason, true
}
