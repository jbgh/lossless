package gate

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// diffMarkRE: a diff line by what follows its +/- marker: a comment
	// opener, or code indentation. A bare "+ item" or "- item" is a bullet
	// (pro/con lists use both), "-   item" is how some formatters indent
	// one, "- # of retries" counts, and "-race" is a flag.
	diffMarkRE = regexp.MustCompile(`^[+-](?:\s*(?://\s|/\*|\*/|\*\s)|#\s|"""|'''|\s{4,}\S)`)
	// plusWordRE: an added line with no space after the plus (a word, or
	// an added bullet). plusFileRE spares a file name that starts with a
	// plus (+page.server.ts, +page.svelte/+page.ts) and reads on as prose;
	// "+1" and "+$100" never match.
	plusWordRE = regexp.MustCompile(`^\+(?:[A-Za-z_]|[-*] )`)
	plusFileRE = regexp.MustCompile(`^\+[\w.-]+\.[a-z]{1,6}(?:[,.:;'’/]|\s+[A-Za-z]|$)`)
	// trailingCommentRE: a statement with a line comment after it, C-style
	// or Python-style (two spaces before the hash).
	trailingCommentRE = regexp.MustCompile(`[;{}),\]][ \t]*//[ \t]|\S[ \t]{2,}#[ \t]`)
	// callStatementRE: a line that opens with a call and ends as a
	// statement or a block. proseRunRE vetoes it: four plain words in a
	// row outside quotes is a sentence ("store.Close() must always run
	// before we return (even on error);"), not a statement.
	callStatementRE = regexp.MustCompile(`^[A-Za-z_$][\w$.]*\(.*\)\s*[;{]$`)
	proseRunRE      = regexp.MustCompile(`(?:^|[\s(])[A-Za-z]+(?: [A-Za-z]+){3}(?:[\s.,;:)]|$)`)
	quotedRE        = regexp.MustCompile("\"[^\"]*\"|'[^']*'|`[^`]*`")
	hunkHeaderRE    = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)
	// jsonMemberRE: a pretty-printed JSON member ("fix": "Do not …"). The
	// value must run to the end of the line, so prose that opens with a
	// quoted key (`"strict": true must stay set`, `"Ship": "one commit"
	// is the rule`) is not one. A string value may be cut short: the
	// sentence splitter stops at its first period.
	jsonMemberRE = regexp.MustCompile(`^\s*"[^"]{1,80}":\s*(?:"(?:[^"\\]|\\.)*"?|[\[{].*|-?\d[\d.eE+-]*|true|false|null)\s*,?\s*$`)
	tickSpanRE   = regexp.MustCompile("`[^`]*`")
)

// HunkHeader reports a unified-diff hunk header and its old and new line
// counts ("@@ -138,6 +138,24 @@"); an omitted count is one.
func HunkHeader(line string) (oldN, newN int, ok bool) {
	if !strings.HasPrefix(line, "@@ -") {
		return 0, 0, false
	}
	m := hunkHeaderRE.FindStringSubmatch(line)
	if m == nil {
		return 0, 0, false
	}
	return hunkCount(m[1]), hunkCount(m[2]), true
}

func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// JSONMember reports a line that is one member of pretty-printed JSON.
func JSONMember(line string) bool {
	return jsonMemberRE.MatchString(line)
}

// CodeLine is a line of a diff, a comment, or data, not a sentence anyone
// said: a diff-marked comment, word, or indented line, a line that opens
// a comment or a docstring, a statement with a trailing comment, or a
// JSON member. The "never" in `// never null` is the code's, and a pasted
// diff is not a list of rules. A sentence that merely names code (a tick
// span, a call mid-sentence) is not this shape, and neither is text of
// more than one line.
func CodeLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || strings.Contains(t, "\n") {
		return false
	}
	// Every shape opens with one of these or carries a comment marker;
	// most lines of a turn do neither and skip the regexes.
	lead := strings.ContainsRune(`+-/*"'@`, rune(t[0]))
	if !lead && !strings.Contains(t, "//") && !strings.Contains(t, "# ") {
		return false
	}
	if lead && codeLead(t) {
		return true
	}
	// A statement quoted in a code span belongs to the sentence around it.
	return trailingCommentRE.MatchString(tickSpanRE.ReplaceAllString(t, "``"))
}

// CodeLead is the part of CodeLine that reads the start of the line: the
// diff, comment, and JSON shapes. Read time applies only this to stored
// failures, since a workflow finding (stored whatever its shape) can
// quote a statement with a trailing comment.
func CodeLead(s string) bool {
	t := strings.TrimSpace(s)
	return t != "" && !strings.Contains(t, "\n") && codeLead(t)
}

func codeLead(t string) bool {
	if _, _, ok := HunkHeader(t); ok || diffMarkRE.MatchString(t) || JSONMember(t) {
		return true
	}
	if plusWordRE.MatchString(t) && !plusFileRE.MatchString(t) {
		return true
	}
	// "//pkg:target" is a label, "//" alone opens nothing, and "*/5" is
	// a cron field.
	for _, p := range []string{"// ", "/*", `"""`, "'''"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return t == "*/" || strings.HasPrefix(t, "*/ ")
}

// CodeStatement is a bare call statement: `expect(x).toEqual([]);`. It is
// a weaker shape than CodeLine (a finding can open with a call and end in
// a semicolon), so read time does not apply it to stored failures.
func CodeStatement(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || (t[len(t)-1] != ';' && t[len(t)-1] != '{') || strings.Contains(t, "\n") {
		return false
	}
	return callStatementRE.MatchString(t) && !proseRunRE.MatchString(quotedRE.ReplaceAllString(t, ""))
}
