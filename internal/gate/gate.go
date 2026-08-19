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
	return containsAny(s, planning)
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
	if stillExtractsNoObject(Fold(s)) {
		return true
	}
	return InspectStatus(s) || theyFoundReviewList(s)
}

// stillExtractsNoObject is extract-meta ("still extracts;" / "still extracts.")
// not "still extracts JWTs".
func stillExtractsNoObject(low string) bool {
	for _, p := range []string{"still extracts.", "still extracts;", "still extract.", "still extract;"} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
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
	if strings.HasSuffix(s, "(") || strings.HasSuffix(s, "`.") || strings.HasSuffix(s, "do not") {
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
	if leadingFileFragment(s) || yamlTreeDump(s) || trailingShortVersion(s) {
		return true
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
	return strings.Contains(low, "fixture") || strings.Contains(low, "quoted the") ||
		strings.Contains(low, "quoting the")
}

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
	}
	failedObject = []string{
		"failed items", "re-queues failed", "pre-failed skip",
		"failure reason", "retryable failure",
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
	}
	processState = []string{
		"in this session", "the next stop", "next test that matters",
		"not another fixture", "that row is always there", "i'll inspect",
		"right next step", "right-next-step",
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
		"never lose memory", "never lose your memory", "switch between them",
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
