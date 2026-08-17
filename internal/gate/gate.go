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
	return containsAny(s, metaFailed)
}

func ProcessState(s string) bool {
	return containsAny(s, processState)
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
	return strings.HasSuffix(s, "path (`.") || strings.Contains(s, "path (`.")
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
	return shortNoPath && len(s) < 80
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
	if FixtureTalk(t) || NextI(t) {
		return true
	}
	if MetaFailedTalk(t) || SessionOp(t) || AgentPrompt(t) || Planning(t) ||
		NarrativeDecision(t) || StatusFailed(t) || FailedAsObject(t) || QuotedAttribution(t) {
		return true
	}
	if RememberedProse(t) || YAMLClaimChrome(t) || Truncated(t) {
		return true
	}
	return false
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
	}
	metaFailed = []string{
		"failed-overlap", "classified as", "type-cap", "packtype",
		"extract noise", "ask pack", "in context", "blocking warning",
		"failure mode", "failed eviction",
	}
	processState = []string{
		"in this session", "the next stop", "next test that matters",
		"not another fixture", "that row is always there", "i'll inspect",
	}
	yamlChrome = []string{
		"text: ", "text = ", "text=", "type: failed", "type: decision", "type: constraint",
		"type: state", "type: thread", "type = ", "type=", "warnings:", "context:", "tokens:",
		"[[context]]", "[context]",
	}
	chromePrefix = []string{
		"#", ">", "|", "<!--", "---", "+++", "<<<<<<", ">>>>>>", "======",
	}
)
