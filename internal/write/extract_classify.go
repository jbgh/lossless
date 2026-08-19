package write

import (
	"regexp"
	"strings"

	"lossless/internal/gate"
	"lossless/internal/redact"
)

var (
	hardFailedRE = regexp.MustCompile(`(?i)\b(we rejected|was rejected|didn't work|did not work|doesn't work|doesn't compile|won't compile|failed|failure|didn't compile|does not work|doesn't pass|does not pass|don't pass|do not pass|won't pass|will not pass|threw|dead end)\b`)
	softFailedRE = regexp.MustCompile(`(?i)\b(revert|abort)\b`)
	exceptionTo  = regexp.MustCompile(`(?i)exception to`)
	constraintRE = regexp.MustCompile(`(?i)\b(always|never|don't|do not|must|we use|we don't)\b`)
	hedgeRE      = regexp.MustCompile(`(?i)\b(i don't think|i do not think|not sure|maybe|probably|might|should we|could we|can we|do we)\b`)
	questionRE   = regexp.MustCompile(`(?i)^\s*(should|could|can|may|do|did|is|are|will)\b`)
	stateRE      = regexp.MustCompile(`(?i)\b(working on|current plan|next step|now implementing)\b`)
	decisionRE   = regexp.MustCompile(`(?i)\b(decided|going with|we'll use|we will use|picked \w+ over|chose|instead of|prefer \S+ over|stick with)\b`)
	useNotRE     = regexp.MustCompile(`(?i)\buse\s+([A-Za-z][\w./@+-]*),?\s+not\s+([A-Za-z][\w./@+-]*)`)
	backtickSpan = regexp.MustCompile("`[^`]*`")
	typeTalkRE   = regexp.MustCompile(`(?i)\b(failed-overlap|shipped-overlap|type-cap|packtypecap|classified as|claim type|as a failed|as failed)\b`)
)

func classify(sentence string, msg Message) string {
	if isQuestion(sentence) {
		return ""
	}
	folded := gate.Fold(sentence)
	probe := stripFailedNoise(stripPaths(folded))
	hard := hardFailedRE.MatchString(probe)
	soft := softFailedRE.MatchString(probe)
	if hard && !gate.MetaFailedTalk(sentence) {
		return "failed"
	}
	if isDecision(folded) && !gate.Planning(sentence) && !gate.NarrativeDecision(sentence) {
		return "decision"
	}
	if (msg.Error || (soft && !gate.MetaFailedTalk(sentence))) && !(isDecision(folded) && !gate.Planning(sentence)) {
		return "failed"
	}
	if constraintRE.MatchString(folded) && msg.Role == "user" && !hedgeRE.MatchString(folded) && !gate.SessionOp(sentence) && !gate.AgentPrompt(sentence) && !gate.ConstraintFragment(sentence) {
		return "constraint"
	}
	if stateRE.MatchString(folded) {
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
	return GroundedFailed(s, paths)
}

// GroundedFailed is true when a failed has a path, tick, or a real
// identifier. The word Failed itself is not an identifier, so
// "Failed work first…" does not count as grounded.
func GroundedFailed(s string, paths []string) bool {
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
		if failWord(w) {
			if i+1 < len(fields) {
				next := strings.ToLower(strings.Trim(fields[i+1], ".,;:()[]\"'"))
				if next == "to" || next == "during" || next == "again" || next == "on" {
					return true
				}
			}
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

func failWord(w string) bool {
	switch strings.ToLower(w) {
	case "failed", "failure", "failing", "fail":
		return true
	default:
		return false
	}
}

var sentenceStarter = map[string]bool{
	"The": true, "This": true, "That": true, "These": true, "Those": true,
	"So": true, "If": true, "We": true, "On": true, "A": true, "An": true,
	"It": true, "I": true, "After": true, "Before": true, "When": true,
	"While": true, "Then": true, "Also": true, "Just": true, "Checking": true,
	"Same": true, "Tests": true, "Live": true, "Ask": true,
	"They": true, "One": true, "Bench": true,
}

func allUpper(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

func isDecision(folded string) bool {
	if decisionRE.MatchString(folded) {
		return true
	}
	m := useNotRE.FindStringSubmatch(folded)
	if m == nil {
		return false
	}
	return !useNotStop[strings.ToLower(m[1])]
}

var useNotStop = map[string]bool{
	"the": true, "a": true, "an": true, "this": true, "that": true,
	"our": true, "my": true, "any": true, "it": true,
}

func isQuestion(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "?") {
		return true
	}
	low := gate.Fold(s)
	if strings.Contains(low, "why don't you") || strings.Contains(low, "why dont you") {
		return true
	}
	return questionRE.MatchString(low)
}
