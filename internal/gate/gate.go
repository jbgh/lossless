// Package gate is the shared write/retrieve skip list for claim prose.
// Extract uses it so junk never lands; ask uses it so already-stored
// junk does not pack. Phrase lists live here once.
package gate

import (
	"regexp"
	"strings"
)

// Fold lowercases and maps curly apostrophes to ASCII so "don’t"
// matches the same lists as "don't".
func Fold(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "\u2019", "'")
	s = strings.ReplaceAll(s, "\u2018", "'")
	return s
}

func containsAny(s string, phrases []string) bool {
	s = Fold(s)
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func hasPrefixFold(s string, prefixes []string) bool {
	s = Fold(strings.TrimSpace(s))
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func Planning(s string) bool {
	if containsAny(s, planning) {
		return true
	}
	return planningVerb(s, []string{"merge", "clear", "measure", "search", "map", "open", "call", "shrink", "point", "dispatch", "re-dispatch", "wait"})
}

// planningVerb is I'll / I will + verb not as a prefix of a longer word
// (I'll clear it skips; I'll clearly / I'll cleartext still store).
func planningVerb(s string, verbs []string) bool {
	low := Fold(s)
	for _, v := range verbs {
		for _, p := range []string{"i'll " + v, "i will " + v} {
			if planningVerbAt(low, p) {
				return true
			}
		}
	}
	return false
}

func planningVerbAt(low, p string) bool {
	for i := 0; i <= len(low); {
		j := strings.Index(low[i:], p)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(p)
		if end == len(low) {
			return true
		}
		switch low[end] {
		case ' ', '.', ';', ',', ':', '!', '?':
			return true
		}
		i = j + 1
	}
	return false
}

// packEcho is the agent talking about a packed failed, not a new failure.
func packEcho(low string) bool {
	if strings.Contains(low, "the failed ") && strings.Contains(low, " record is unrelated") {
		return true
	}
	if strings.HasPrefix(low, "prior failure was another") {
		return true
	}
	if strings.HasPrefix(low, "the earlier failed ") {
		return true
	}
	if strings.HasPrefix(low, "that failed ") &&
		(strings.Contains(low, "`") || strings.Contains(low, "already fixed") ||
			strings.Contains(low, "already superseded") || strings.Contains(low, " was the ")) {
		return true
	}
	return false
}

// JSONFragment is a chopped workflow/ask JSON shard, not a claim.
func JSONFragment(s string) bool {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, `","`) || strings.HasPrefix(t, `", "`) {
		return true
	}
	t = strings.TrimLeft(t, "\"'`")
	return strings.HasPrefix(t, `severity":`) || strings.HasPrefix(t, `evidence":`) ||
		strings.HasPrefix(t, `issue":`)
}

func SessionOp(s string) bool {
	return containsAny(s, sessionOp)
}

func NarrativeDecision(s string) bool {
	return containsAny(s, narrative)
}

func AgentPrompt(s string) bool {
	low := Fold(s)
	if strings.Contains(low, "why don't you") || strings.Contains(low, "why do not you") ||
		strings.Contains(low, "why dont you") {
		return true
	}
	return strings.HasPrefix(low, "can you ") || strings.HasPrefix(low, "could you ")
}

// InstructionChrome is Claude Task/reviewer prompt text, not a product claim.
func InstructionChrome(s string) bool {
	low := Fold(strings.TrimSpace(s))
	low = strings.TrimPrefix(low, "- ")
	low = strings.TrimLeft(low, "\"“”'`*# ")
	if strings.HasPrefix(low, "read-only") {
		return true
	}
	if strings.Contains(low, "(read-only") {
		return true
	}
	if strings.HasPrefix(low, "now i understand the failure") {
		return true
	}
	if strings.HasPrefix(low, "rank any findings by severity") {
		return true
	}
	if strings.HasPrefix(low, "for each:") && strings.Contains(low, "severity") {
		return true
	}
	if strings.HasPrefix(low, "return ") &&
		(strings.Contains(low, "approve or request") || strings.Contains(low, "ranked findings")) {
		return true
	}
	return false
}

func StatusFailed(s string) bool {
	return containsAny(s, statusFailed)
}

func FailedAsObject(s string) bool {
	return containsAny(s, failedObject)
}

func MetaFailedTalk(s string) bool {
	if containsAny(s, metaFailed) {
		return true
	}
	low := Fold(strings.TrimSpace(s))
	low = strings.TrimPrefix(low, "- ")
	if strings.HasPrefix(low, "lossless flags") || strings.HasPrefix(low, "lossless returned") ||
		strings.HasPrefix(low, "lossless flagged") || strings.HasPrefix(low, "lossless will") ||
		strings.HasPrefix(low, "lossless ask returned") {
		return true
	}
	if packEcho(low) {
		return true
	}
	if goFirstMash(low) || stillExtractsNoObject(low) {
		return true
	}
	return InspectStatus(s) || theyFoundReviewList(s) || theyFoundHyphenList(s)
}

// stillExtractsNoObject is extract-meta punctuation ("still extracts;" /
// "still stores.") not "still extracts JWTs" and not "We still store: the
// session JSONL" (colon after store is a real claim).
func stillExtractsNoObject(low string) bool {
	for _, p := range []string{
		"still extracts.", "still extracts;", "still extract.", "still extract;",
		"still stores.", "still stores;", "still store.", "still store;",
	} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// goFirstMash is the recap list "go-first, 4.0, …". A
// concurrent_test.go-first failed does not contain "go-first,".
func goFirstMash(low string) bool {
	return strings.Contains(low, "go-first,")
}

func ProcessState(s string) bool {
	if containsAny(s, processState) {
		return true
	}
	low := Fold(strings.TrimSpace(s))
	return strings.HasSuffix(low, " next.") || strings.HasSuffix(low, " next")
}

// ConstraintFragment is a continuation clause, not a standing rule.
func ConstraintFragment(s string) bool {
	low := Fold(strings.TrimSpace(s))
	return strings.HasPrefix(low, "so ") || strings.HasPrefix(low, "and ") ||
		strings.HasPrefix(low, "but ")
}

func RememberedProse(s string) bool {
	return hasPrefixFold(s, []string{"remembered:", "remembered "})
}

func NextI(s string) bool {
	return hasPrefixFold(s, []string{"next i ", "next i'", "next i'll"})
}

func YAMLClaimChrome(s string) bool {
	low := Fold(strings.TrimSpace(s))
	low = strings.TrimPrefix(low, "- ")
	for _, p := range yamlChrome {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

func QuotedAttribution(s string) bool {
	if !strings.Contains(Fold(s), "said") {
		return false
	}
	return strings.Contains(Fold(s), "said:") ||
		strings.Contains(s, `"`) || strings.Contains(s, "“") || strings.Contains(s, "”")
}

func Truncated(s string) bool {
	s = strings.TrimSpace(s)
	// A sentence that merely ends in a code span ("… failed in `AuthTests`.")
	// is whole. The old splitter cut at a dot inside a span and left an
	// unbalanced tick; the odd-count rule below still catches those rows.
	// " ." is what such a cut left once the dangling tick was trimmed
	// ("Never touched .").
	if strings.HasSuffix(s, "(") || strings.HasSuffix(s, " .") || strings.HasSuffix(s, "do not") {
		return true
	}
	if strings.HasSuffix(s, "path (`.") || strings.Contains(s, "path (`.") {
		return true
	}
	if strings.Contains(s, "…)") || strings.Contains(s, "...)") {
		return true
	}
	if strings.HasSuffix(s, "|") {
		return true
	}
	if f := strings.Fields(s); len(f) > 0 && strings.Contains(f[0], "`") {
		return true
	}
	if strings.Count(s, "`")%2 == 1 && strings.HasSuffix(s, ".") {
		return true
	}
	low := Fold(s)
	if ConstraintFragment(s) && (strings.Contains(s, "`") || strings.Contains(s, "|") || strings.Contains(low, "…")) {
		return true
	}
	if leadingFileFragment(s) || yamlTreeDump(s) || trailingShortVersion(s) || leadingWordChop(s) {
		return true
	}
	if strings.Count(s, "(") > strings.Count(s, ")") {
		return true
	}
	return false
}

// shortWords are one- and two-letter tokens that open real sentences
// (words, and the short package / dir names agents type in lowercase).
var shortWords = map[string]bool{
	"a": true, "i": true, "an": true, "as": true, "at": true, "be": true, "by": true,
	"do": true, "go": true, "he": true, "if": true, "in": true, "is": true, "it": true,
	"me": true, "my": true, "no": true, "of": true, "ok": true, "on": true, "or": true,
	"so": true, "to": true, "up": true, "us": true, "we": true, "hi": true, "oh": true,
	"ah": true, "um": true, "eg": true, "ie": true, "vs": true, "tl": true, "re": true,
	"os": true, "io": true, "ui": true, "ux": true, "db": true, "js": true, "ts": true,
	"py": true, "rb": true, "cd": true, "ci": true, "qa": true, "pr": true, "id": true,
	"ip": true, "vm": true, "fs": true, "ls": true, "rm": true, "cp": true, "mv": true,
	"sh": true,
}

func lowerLetters(w string) bool {
	if w == "" {
		return false
	}
	for i := 0; i < len(w); i++ {
		if w[i] < 'a' || w[i] > 'z' {
			return false
		}
	}
	return true
}

// leadingWordChop is a mid-word cut at the start of a clipped turn:
// "tially but restrict", "e verbObjRE's", "n LLM process", "ve origin/main`",
// "al/bench_test.go 0.95 floor", "or `sharedCodeIdent` (…". Real lowercase
// openers stay: "env exists; do not print secrets", "qa-fix-loop …",
// "os/exec failed", "ok so redis failed".
func leadingWordChop(s string) bool {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) < 2 {
		return false
	}
	raw := strings.Trim(fields[0], "\"“”'`.,;:()[]")
	if raw == "" || raw[0] < 'a' || raw[0] > 'z' {
		return false
	}
	// A path whose first segment is not a word: "al/bench_test.go".
	if i := strings.IndexByte(raw, '/'); i > 0 && i <= 2 {
		seg := raw[:i]
		return lowerLetters(seg) && !shortWords[seg]
	}
	if strings.ContainsAny(raw, "/._-#") || len(raw) > 8 {
		return false
	}
	if shortWords[raw] {
		// "or `sharedCodeIdent` (…" — a lowercase conjunction opening on a
		// tick is the remainder of a cut sentence.
		switch raw {
		case "or", "and", "but", "nor":
			return strings.HasPrefix(fields[1], "`")
		}
		return false
	}
	if len(raw) <= 2 && lowerLetters(raw) {
		return true
	}
	switch Fold(strings.Trim(fields[1], "\"“”'`.,;:()[]")) {
	case "but", "and", "or", "so", "then":
		return true
	}
	return false
}

var (
	tagWrappedRE = regexp.MustCompile(`^<([a-z][a-z0-9-]*)>[\s\S]*</([a-z][a-z0-9-]*)>$`)
	runnerOutput = []*regexp.Regexp{
		regexp.MustCompile(`^Executed \d+ tests?, with \d+ failures?`),
		regexp.MustCompile(`^Test (Suite|Case) '.*' (failed|passed|started)`),
		regexp.MustCompile(`^--- (FAIL|PASS|SKIP): `),
		regexp.MustCompile(`^(FAIL|ok|PASS)\s+\S+\s+[\d.]+s$`),
		regexp.MustCompile(`^npm (ERR!|WARN) `),
	}
)

var (
	zeroFailRE  = regexp.MustCompile(`(?i)\b(?:0|zero|no|none|nothing)\s+(?:(?:tests?|rows?|cases?|suites?)\s+)?(?:failed|failures?|failing)\b`)
	otherFailRE = regexp.MustCompile(`(?i)\b(?:fail(?:ed|ure|ures|ing|s)?|rejected|threw|dead end|didn't work|did not work|doesn't work|does not work|won't compile|doesn't compile)\b`)
)

// StripZeroFail blanks the failure words of a clean run ("0 failed",
// "with no failures", "nothing failed") so they do not read as a failure.
func StripZeroFail(s string) string {
	return zeroFailRE.ReplaceAllString(s, " ")
}

// SuccessReport is a run summary whose only failure words count zero:
// "1,794 passed / 0 failed / 11 skipped". "2 passed, 0 failed, then the
// deploy threw" is not this shape.
func SuccessReport(s string) bool {
	return zeroFailRE.MatchString(s) && !otherFailRE.MatchString(StripZeroFail(s))
}

// LeadIn is a sentence that ends in a colon (after closing emphasis or
// quotes). It introduces the lines that follow and says nothing alone.
func LeadIn(s string) bool {
	t := strings.TrimRight(strings.TrimSpace(s), "*_\"'”’)` ")
	return strings.HasSuffix(t, ":")
}

// InvestigationNarration is an agent announcing what it is about to look
// at ("Looking into the failed invite."). Nothing has been found yet. A
// finding that merely opens with Looking at … is not this shape.
func InvestigationNarration(s string) bool {
	t := strings.TrimLeft(strings.TrimSpace(s), "\"“”'`*_- ")
	return hasPrefixFold(t, []string{"looking into ", "diagnosing ", "investigating ", "digging into "})
}

// TagWrapped is a whole sentence inside one harness tag
// (<summary>Background command … failed with exit code 1</summary>).
func TagWrapped(s string) bool {
	m := tagWrappedRE.FindStringSubmatch(strings.TrimSpace(s))
	return m != nil && m[1] == m[2]
}

// RunnerOutput is a test-runner or package-manager status line that
// reached prose (XCTest, go test, npm). A sentence about a failed test
// is not this shape.
func RunnerOutput(s string) bool {
	t := strings.TrimSpace(s)
	for _, re := range runnerOutput {
		if re.MatchString(t) {
			return true
		}
	}
	return false
}

// leadingFileFragment is a chopped test path at the start of a sentence
// (e_test.go …). A real concurrent_test.go-first failed is not this shape.
func leadingFileFragment(s string) bool {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return false
	}
	first := strings.Trim(fields[0], "\"“”'`.,;:()[]")
	if first == "" || strings.ContainsAny(first, "/\\") {
		return false
	}
	i := strings.LastIndex(first, ".")
	if i <= 0 || i == len(first)-1 {
		return false
	}
	ext := first[i+1:]
	if len(ext) < 1 || len(ext) > 4 {
		return false
	}
	for _, r := range ext {
		if r < 'A' || (r > 'Z' && r < 'a') || r > 'z' {
			return false
		}
	}
	base := strings.ToLower(first[:i])
	idx := strings.LastIndex(base, "_test")
	if idx < 0 {
		return false
	}
	return idx <= 1
}

func yamlTreeDump(s string) bool {
	low := Fold(strings.TrimSpace(s))
	low = strings.TrimPrefix(low, "- ")
	return strings.HasPrefix(low, "tree:")
}

var (
	trailingChoppedZeroVersion = regexp.MustCompile(`(?:^|[^0-9.])0\.\d+\.$`)
	trailingThreePartVersion   = regexp.MustCompile(`\d+\.\d+\.\d+\.$`)
)

// trailingShortVersion is a chopped 0.1.x (0.1.) not a complete 0.1.7.
// or a standing two-part weight (4.0. / 2.5.).
func trailingShortVersion(s string) bool {
	s = strings.TrimSpace(s)
	if trailingThreePartVersion.MatchString(s) {
		return false
	}
	return trailingChoppedZeroVersion.MatchString(s)
}

var (
	inspectClock    = regexp.MustCompile(`\b\d{1,2}:\d{2}\b`)
	liveRecentNAre  = regexp.MustCompile(`^live recent \d+ are\b`)
	inspectRecentOn = regexp.MustCompile(`^inspect recent on\b`)
)

// InspectStatus is inspect-window recap, not a product failed.
// recent_noise= dumps, "Live recent N are" / "Inspect recent on" leads,
// clock-time recap ids, recap-faileds in the recent window.
// "They found Redis token bucket failed in src/middleware/auth.ts" is not this.
func InspectStatus(s string) bool {
	low := Fold(strings.TrimSpace(s))
	low = strings.TrimPrefix(low, "- ")
	if strings.Contains(low, "recent_noise=") || strings.Contains(low, "`recent_noise") {
		return true
	}
	if liveRecentNAre.MatchString(low) || inspectRecentOn.MatchString(low) {
		return true
	}
	if inspectClock.MatchString(s) && recapFailedTalk(low) {
		return true
	}
	if recapFailedTalk(low) && (strings.Contains(low, "recent window") ||
		strings.Contains(low, "live store") || strings.Contains(low, "live export")) {
		return true
	}
	if strings.Contains(low, "remaining active failed") && strings.Contains(low, "recap") {
		return true
	}
	return false
}

func recapFailedTalk(low string) bool {
	return strings.Contains(low, "recap-failed") || strings.Contains(low, "recap failed") ||
		strings.Contains(low, "recap-as-failed") || strings.Contains(low, "packed failed")
}

// theyFoundHyphenList is "They-found Redis, named-lock, JWT next, Tests
// failed to, …" lock-list recap, not "They found Redis token bucket
// failed in src/middleware/auth.ts".
func theyFoundHyphenList(s string) bool {
	low := Fold(strings.TrimSpace(s))
	low = strings.TrimPrefix(low, "- ")
	if !strings.HasPrefix(low, "they-found") {
		return false
	}
	return strings.Contains(low, ",") && strings.Contains(low, " and ")
}

// theyFoundReviewList is "They found X: a, b, and c" review recap, not
// "They found Redis token bucket failed in src/middleware/auth.ts".
func theyFoundReviewList(s string) bool {
	low := Fold(s)
	i := strings.Index(low, "they found")
	if i < 0 {
		return false
	}
	rest := low[i+len("they found"):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return false
	}
	after := rest[colon+1:]
	return strings.Contains(after, ",") && strings.Contains(after, " and ")
}

func FixtureTalk(s string) bool {
	low := Fold(s)
	if strings.Contains(low, "quoted the") || strings.Contains(low, "quoting the") {
		return true
	}
	if !strings.Contains(low, "fixture") {
		return false
	}
	// A repo whose tests use real fixtures names the artifact: a tick, a
	// dotted file with a real extension, or a deep path. "e.g." and
	// "read/write" are not artifacts; self-talk stays gated.
	if strings.Contains(s, "`") || fixtureFileRE.MatchString(s) || deepPathRE.MatchString(s) {
		return false
	}
	return true
}

var (
	fixtureFileRE = regexp.MustCompile(`\b[\w-]+\.[A-Za-z][A-Za-z0-9]{1,6}\b`)
	deepPathRE    = regexp.MustCompile(`\b[\w.-]+/[\w.-]+/[\w.-]+`)
)

func ChromePrefix(s string) bool {
	s = strings.TrimSpace(s)
	for _, p := range chromePrefix {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

var numberedItem = regexp.MustCompile(`^\d+[.)]\s+`)

// ListMarker reports a markdown / numbered list item and the rest of the line.
func ListMarker(s string) (rest string, ok bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") {
		return strings.TrimSpace(s[2:]), true
	}
	if numberedItem.MatchString(s) {
		return strings.TrimSpace(numberedItem.ReplaceAllString(s, "")), true
	}
	return "", false
}

// ListChrome is extract/retrieve list noise: bold/tick/quote bullets, or a
// short item. shortNoPath is true when the caller wants every short list
// dropped (retrieve) or only pathless short lists (extract).
func ListChrome(s string, shortNoPath bool) bool {
	rest, ok := ListMarker(s)
	if !ok {
		return false
	}
	if strings.Contains(s, "**") || strings.HasPrefix(rest, "`") || strings.HasPrefix(rest, ">") {
		return true
	}
	// Pathless bullets are recap chrome. Pathful list items can still be claims.
	return shortNoPath
}

// SkipProse is the path-agnostic half of extract skipSentence: headings,
// tables, planning, session ops, status faileds, yaml/toml dumps, etc.
func SkipProse(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	if exampleDrop(t) {
		return true
	}
	if JSONFragment(t) {
		return true
	}
	if InstructionChrome(t) || TagWrapped(t) || RunnerOutput(t) || LeadIn(t) || InvestigationNarration(t) {
		return true
	}
	t = strings.TrimLeft(t, "\"“”'`")
	if t == "" {
		return true
	}
	if ChromePrefix(t) || strings.Contains(t, " | ") {
		return true
	}
	if strings.HasPrefix(t, "**") && strings.HasSuffix(t, "**") && !strings.Contains(t, ".") {
		return true
	}
	if strings.HasPrefix(t, "**") && strings.Count(t, "**")%2 == 1 {
		return true
	}
	if hasPrefixFold(t, []string{"**what was wrong", "**what you do next"}) {
		return true
	}
	if FixtureTalk(t) || NextI(t) {
		return true
	}
	if MetaFailedTalk(t) || SessionOp(t) || AgentPrompt(t) || Planning(t) ||
		NarrativeDecision(t) || StatusFailed(t) || FailedAsObject(t) || QuotedAttribution(t) {
		return true
	}
	if RememberedProse(t) || YAMLClaimChrome(t) || Truncated(t) || SkillTalk(t) || ProductCopy(t) {
		return true
	}
	// ProcessState is type-scoped in extract and extractNoise. A They-found
	// Redis failed that contains "in this session" or ends " next." still stores.
	return false
}

// exampleDrop is inspect-recap chrome ("Example drop: …") after list-marker
// and quote trim. Unprefixed They-found Redis is not this shape.
func exampleDrop(s string) bool {
	t := strings.TrimSpace(s)
	if rest, ok := ListMarker(t); ok {
		t = rest
	}
	t = strings.TrimLeft(t, "\"“”'`")
	return hasPrefixFold(t, []string{"example drop:"})
}

func ProductCopy(s string) bool {
	return containsAny(s, productCopy)
}

func SkillTalk(s string) bool {
	return containsAny(s, skillTalk)
}

var (
	planning = []string{
		"i'll check", "i will check", "i'll look",
		"i'll read", "i'll audit",
		"i'll fix", "i'll start", "i'll add",
		"i'll inspect", "i'll pull",
		"i'll go with", "i will go with",
		"let's go with", "lets go with", "i'll switch",
		"i'll try",
		"i'll match", "i will match",
		"i'll slow", "i will slow",
		"i'll verify", "i will verify",
		"i'll implement", "i will implement",
		"i'll replace", "i'll swap",
		"i'll rewrite", "i will rewrite",
		"i'll migrate", "i'll refactor",
		"i'll patch",
		"i'll ask", "i will ask",
		"i'll load", "i will load",
		"i'll run", "i will run", "i'll-run",
		"i'll rerun", "i will rerun",
		"the next hour", "we will use the next", "we'll use the next",
		"let me check", "let me get", "let me look", "let me read",
		"let me also", "let me focus", "let me investigate", "let me abort",
		"let me find", "let me search", "let me decode", "let me wait",
		"let me see", "let me inspect", "let me call", "let me take",
		"let me start", "let me wrap",
	}
	sessionOp = []string{
		"don't ask", "do not ask", "don't change source", "don't delete data",
		"do not open a pr", "do not redo", "don't flag", "do not start",
		"never mind", "don't push yet", "do not push yet",
		"don't merge yet", "do not merge yet",
		"don't commit yet", "do not commit yet",
		"don't wait", "do not wait",
		"don't have time", "do not have time", "we don't have time", "we do not have time",
		"must be a bug", "must be a typo",
	}
	narrative = []string{
		"chose the wrong", "picked the wrong", "wrong approach",
		"chose poorly", "picked poorly",
		"almost picked", "almost chose", "almost going",
	}
	statusFailed = []string{
		"ci unit-test", "unit-test failure", "unit test failure",
		"background notification", "checking #", "pr #", "pr-size-check",
		"which of those", "re-pushing", "exit 0",
		"github actions", "actions workflow", "actions job",
		"failed to tap", "no matches found for",
		"assertion failed", "--- fail:",
	}
	failedObject = []string{
		"failed items", "re-queues failed", "pre-failed skip",
		"failure reason", "failure-reason", "retryable failure",
		"real failure", "the failure at", "fix the failure",
	}
	metaFailed = []string{
		"failed-overlap", "classified as", "type-cap", "packtype",
		"extract noise", "ask pack", "in context", "blocking warning",
		"failure mode", "failed eviction",
		"off-topic", "ranking/topic",
		"failed/decision", "classify now",
		"forced failed", "counts as a",
		"stand-in",
		"extract residue", "ask-would-drop",
		"remaining recent residue",
		"no i'll-ask", "no failed-work-first",
		"intended gap:", "intended-gap", "shipped channel is still",
		"same-failure", "same-failure-twice", "pauses as no-progress",
		"in skipprose",
		"live residue", "dump an ask json",
		"still store and pack",
		"still stores and pack",
		"lock the recap row",
		"recap-as-failed",
		// talk about ask's own warning text
		"prior attempt failed", "prior attempt at this goal",
	}
	processState = []string{
		"in this session", "the next stop", "next test that matters",
		"not another fixture", "that row is always there", "i'll inspect",
		"right next step", "right-next-step",
		// turn-scoped waits: the agent's scheduling, not project state
		"nothing independent to request", "arrives by notification", "depends on a notification",
	}
	skillTalk = []string{
		"ignore a skill", "can ignore a skill",
		"one sentence on a tool", "not a guarantee",
	}
	productCopy = []string{
		"compact thinning", "failed approaches become",
		"library choice becomes", "picked something",
		"session log is the memory", "compaction is lossy",
		"stay abstract in the readme", "over a long project that happens",
		"failed work first, then", "then what already shipped",
		"the next product is:", "before it retries the failed work",
		"harness holes beyond",
		"never lose memory", "never lose your memory",
		"switch between them",
	}
	yamlChrome = []string{
		"text: ", "text = ", "text=", "type: failed", "type: decision", "type: constraint",
		"type: state", "type: thread", "type = ", "type=", "warnings:", "context:", "tokens:",
		"[[context]]", "[context]",
	}
	chromePrefix = []string{
		"#", ">", "|", "{", "<!--", "---", "+++", "<<<<<<", ">>>>>>", "======",
	}
)

// ArrowChrome reports diagram or instruction shape: any rune from the
// Unicode arrow or box-drawing blocks. Real rename talk uses words or
// ASCII "->" and stays; the caller spares sentences anchored by a path,
// tick, or bold span.
func ArrowChrome(s string) bool {
	for _, r := range s {
		if (r >= 0x2190 && r <= 0x21FF) || // arrows
			r == 0x2794 || (r >= 0x2798 && r <= 0x27AF) || (r >= 0x27B1 && r <= 0x27BE) || // dingbat arrows (not ➕➖➗➰➿)
			(r >= 0x27F0 && r <= 0x27FF) || // long arrows
			(r >= 0x2B00 && r <= 0x2B11) || // misc arrows (not ⬛⬜⭐⭕)
			(r >= 0x2B30 && r <= 0x2B4F) ||
			(r >= 0x2B60 && r <= 0x2BB8) ||
			(r >= 0x2500 && r <= 0x257F) { // box drawing
			return true
		}
	}
	return false
}
