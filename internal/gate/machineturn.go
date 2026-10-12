package gate

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// machineLead opens a user-role turn that no person typed: a subagent
// brief or a harness notice. Only what a harness writes for everyone
// belongs here. One project's own templates are caught by repetition
// (the opening already stored from two other sessions), not by name.
var machineLead = []string{
	"task:", "your task is", "your task:", "[request interrupted", "caveat:",
	"this session is being continued", "your claude.ai usage limit",
	"stop hook", "base directory for this skill",
	"continue if you have next steps",
}

// BriefLen is how long a turn is before the marks of a brief count. A
// person does write "do NOT touch /Users/me/app/config.yaml"; briefs run
// to paragraphs.
const BriefLen = 200

var (
	// "/compact", "/loop 5m /check" — not "/Users/me/notes.md", and not
	// "/tmp is full again, clean it up".
	slashCommandRE = regexp.MustCompile(`^/[a-z][\w:-]*(?:\s+\S+){0,2}\s*$`)
	briefYouAreRE  = regexp.MustCompile(`^you are (?:a|an|one of|reviewing|implementing|auditing)\b`)
	stoppedRE      = regexp.MustCompile(`^\d+ background agents? (?:was|were) stopped by the user`)
)

// The four marks of a brief one agent writes for another. People typing
// to their own agent rarely use two of them in one turn.
var briefSignals = []*regexp.Regexp{
	// an absolute path into a checkout or a scratch directory
	regexp.MustCompile(`(?:^|[\s` + "`" + `('"])/(?:Users|home|tmp|private|var|opt|workspace)/`),
	// directives in capitals
	regexp.MustCompile(`\b(?:DO NOT|Do NOT|MUST|READ-ONLY|NEVER|ONLY|NOT)\b`),
	// headings, bold labels, a numbered or bulleted list in one turn
	regexp.MustCompile(`(?:^|\s)(?:#{1,4} |\*\*[A-Z][^*]{2,40}\*\*:?|1\. .* 2\. |- .* - .* - )`),
	// what to hand back, and what not to touch
	regexp.MustCompile(`(?i)\b(?:report (?:back|evidence|findings|branch|the)|return (?:a|the|your|only)|your (?:final )?(?:report|output|response) (?:should|must)|do not (?:implement|fix|edit|modify|push|merge|commit))\b`),
}

// BriefSignals counts the marks of an agent-written brief in a turn.
func BriefSignals(s string) int {
	n := 0
	for _, re := range briefSignals {
		if re.MatchString(s) {
			n++
		}
	}
	return n
}

// MachineTurn is a whole user-role turn that a person did not type: a
// brief one agent wrote for another, a slash command, a harness notice,
// a prompt passed in quotes on a command line. What the user said is
// memory; what a tool said in the user's place is not.
func MachineTurn(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	// A turn that opens with a box or block glyph is a tool's status
	// line ("▣ pruned 3.2K tokens"), not typing.
	if r, _ := utf8.DecodeRuneInString(t); r >= 0x2500 && r <= 0x25FF {
		return true
	}
	low := Fold(t)
	for _, p := range machineLead {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	if slashCommandRE.MatchString(low) || briefYouAreRE.MatchString(low) || stoppedRE.MatchString(low) {
		return true
	}
	// A prompt passed whole in quotes on a command line. `"Save" should
	// read "Save as"` is a person quoting two labels.
	if len(t) >= 2 && t[0] == '"' && t[len(t)-1] == '"' && strings.Count(t, `"`) == 2 {
		return true
	}
	return len(t) >= BriefLen && BriefSignals(t) >= 2
}
