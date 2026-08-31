package write

import (
	"regexp"
	"strings"

	"lossless/internal/claim"
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
	"They": true, "One": true,
	"Search": true, "Home": true, "Albums": true, "Upcoming": true,
	"Local": true, "Failed": true,
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

var pickedOverRE = regexp.MustCompile(`(?i)\b(?:picked|prefer(?:red)?)\s+\S+\s+over\s+\S+`)

var insteadOfRE = regexp.MustCompile(`(?i)\b(\S+)\s+instead\s+of\s+(\S+)`)

// verbObjRE: a classifying decision verb with a direct object names its
// referent even in lowercase ("we'll use postgres next").
var verbObjRE = regexp.MustCompile(`(?i)\b(?:we(?:'|’)?ll use|we will use|going with|go with|decided on|chose|picked)\s+([a-z][\w.+-]*)`)

// GroundedDecision is true when a decision names a referent: a path, a
// tick or bold span, a code-shaped token, a mid-sentence proper noun, an
// acronym, or a use-X-not-Y / picked-X-over-Y shape. "I'll stick with
// keep." has a commitment verb and no object: narration, not memory.
func GroundedDecision(s string, paths []string) bool {
	if len(paths) > 0 {
		return true
	}
	if raw := findPaths(s); len(redact.FilterPaths(raw)) > 0 {
		return true
	}
	if strings.Contains(s, "`") || strings.Contains(s, "**") {
		return true
	}
	folded := gate.Fold(s)
	if m := useNotRE.FindStringSubmatch(folded); m != nil && !useNotStop[strings.ToLower(m[1])] {
		return true
	}
	if pickedOverRE.MatchString(folded) {
		return true
	}
	// "X instead of Y" names two alternatives only when both sides are
	// real objects; "that way instead of the other" is narration.
	if m := insteadOfRE.FindStringSubmatch(folded); m != nil &&
		!useNotStop[strings.Trim(m[1], ".,;:")] && !useNotStop[strings.Trim(m[2], ".,;:")] {
		return true
	}
	if m := verbObjRE.FindStringSubmatch(folded); m != nil && !useNotStop[m[1]] {
		return true
	}
	for i, w := range strings.Fields(s) {
		w = strings.Trim(w, ".,;:()[]\"'*")
		if w == "" {
			continue
		}
		// Bare quantities and prose abbreviations read as code shapes
		// but name nothing.
		if pureDigits(w) || proseAbbrev[strings.ToLower(w)] {
			continue
		}
		if claim.CodeShaped(w) {
			return true
		}
		if acronym(w) {
			return true
		}
		if i > 0 && len(w) >= 3 && w[0] >= 'A' && w[0] <= 'Z' && !allUpper(w) && lettersOnly(w) {
			return true
		}
	}
	return false
}

// lettersOnly excludes contractions: "I'm" is not a proper noun.
func lettersOnly(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

func pureDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var proseAbbrev = map[string]bool{
	"e.g": true, "i.e": true, "eg": true, "ie": true,
	"etc": true, "vs": true, "cf": true,
}

// acronym is 2-8 letters, all capitals: JWT, REST, SQL. Not "I".
func acronym(s string) bool {
	if len(s) < 2 || len(s) > 8 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
