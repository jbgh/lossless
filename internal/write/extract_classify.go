package write

import (
	"regexp"
	"strings"

	"lossless/internal/gate"
	"lossless/internal/redact"
)

var (
	hardFailedRE = regexp.MustCompile(`(?i)\b(we rejected|was rejected|didn't work|did not work|doesn't work|doesn’t work|doesn't compile|doesn’t compile|won't compile|won’t compile|failed|failure|didn't compile|does not work|threw)\b`)
	softFailedRE = regexp.MustCompile(`(?i)\b(revert|abort)\b`)
	exceptionTo  = regexp.MustCompile(`(?i)exception to`)
	constraintRE = regexp.MustCompile(`(?i)\b(always|never|don't|do not|must|we use|we don't)\b`)
	hedgeRE      = regexp.MustCompile(`(?i)\b(i don't think|i do not think|not sure|maybe|probably|might|should we|could we|can we|do we)\b`)
	questionRE   = regexp.MustCompile(`(?i)^\s*(should|could|can|may|do|did|is|are|will)\b`)
	stateRE      = regexp.MustCompile(`(?i)\b(working on|current plan|next step|now implementing)\b`)
	decisionRE   = regexp.MustCompile(`(?i)\b(decided|going with|we'll use|we will use|picked \w+ over|chose|instead of)\b`)
	backtickSpan = regexp.MustCompile("`[^`]*`")
	typeTalkRE   = regexp.MustCompile(`(?i)\b(failed-overlap|shipped-overlap|type-cap|packtypecap|classified as|claim type|as a failed|as failed)\b`)
)

func classify(sentence string, msg Message) string {
	if isQuestion(sentence) {
		return ""
	}
	probe := stripFailedNoise(stripPaths(sentence))
	hard := hardFailedRE.MatchString(probe)
	soft := softFailedRE.MatchString(probe)
	if hard && !gate.MetaFailedTalk(sentence) {
		return "failed"
	}
	if decisionRE.MatchString(sentence) && !gate.Planning(sentence) && !gate.NarrativeDecision(sentence) {
		return "decision"
	}
	if (msg.Error || (soft && !gate.MetaFailedTalk(sentence))) && !(decisionRE.MatchString(sentence) && !gate.Planning(sentence)) {
		return "failed"
	}
	if constraintRE.MatchString(sentence) && msg.Role == "user" && !hedgeRE.MatchString(sentence) && !gate.SessionOp(sentence) && !gate.AgentPrompt(sentence) {
		return "constraint"
	}
	if stateRE.MatchString(sentence) {
		return "state"
	}
	return ""
}

func skipSentence(s string) bool {
	if gate.SkipProse(s) {
		return true
	}
	t := strings.TrimSpace(s)
	t = strings.TrimLeft(t, "\"“”'`")
	return gate.ListChrome(t, len(findPaths(t)) == 0)
}

func stripTypeTalk(s string) string {
	s = backtickSpan.ReplaceAllString(s, " ")
	s = typeTalkRE.ReplaceAllString(s, " ")
	return s
}

func stripFailedNoise(s string) string {
	s = stripTypeTalk(s)
	s = exceptionTo.ReplaceAllString(s, " ")
	return s
}

func groundedFailed(s string, paths []string) bool {
	if len(paths) > 0 {
		return true
	}
	if raw := findPaths(s); len(redact.FilterPaths(raw)) > 0 {
		return true
	}
	if gate.StatusFailed(s) || gate.FailedAsObject(s) {
		return false
	}
	if strings.Contains(s, "`") || strings.Contains(s, "**") {
		return true
	}
	fields := strings.Fields(s)
	for i, w := range fields {
		w = strings.Trim(w, ".,;:()[]\"'")
		if w == "" {
			continue
		}
		if i == 0 && sentenceStarter[w] {
			continue
		}
		if len(w) >= 3 && w[0] >= 'A' && w[0] <= 'Z' && !allUpper(w) {
			return true
		}
		if strings.Contains(w, "-") && w[0] >= 'A' && w[0] <= 'Z' {
			return true
		}
	}
	return false
}

var sentenceStarter = map[string]bool{
	"The": true, "This": true, "That": true, "These": true, "Those": true,
	"So": true, "If": true, "We": true, "On": true, "A": true, "An": true,
	"It": true, "I": true, "After": true, "Before": true, "When": true,
	"While": true, "Then": true, "Also": true, "Just": true, "Checking": true,
}

func allUpper(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

func isQuestion(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "?") {
		return true
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "why don't you") || strings.Contains(low, "why dont you") {
		return true
	}
	return questionRE.MatchString(s)
}

// Wrappers so existing extract tests keep calling package-local names.
func planningDecision(s string) bool      { return gate.Planning(s) }
func sessionOpConstraint(s string) bool   { return gate.SessionOp(s) }
func agentPromptConstraint(s string) bool { return gate.AgentPrompt(s) }
func narrativeDecision(s string) bool     { return gate.NarrativeDecision(s) }
func statusFailed(s string) bool          { return gate.StatusFailed(s) }
func failedAsObject(s string) bool        { return gate.FailedAsObject(s) }
func metaFailedTalk(s string) bool        { return gate.MetaFailedTalk(s) }
func quotedAttribution(s string) bool     { return gate.QuotedAttribution(s) }
