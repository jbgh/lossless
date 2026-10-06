package write

import (
	"encoding/json"
	"regexp"
	"strings"

	"lossless/internal/gate"
)

// stripNonProse removes pasted data from a turn before it is split into
// sentences: unified-diff hunks, fenced code, JSON, and single lines of
// code. A diff in a turn (an automated review prompt carries the whole
// change) is code: its comment lines say "never" and "must" and were
// stored as the user's constraints. A JSON string value is data the same
// way, and the splitter would cut it into sentences that no longer look
// like JSON.
//
// A hunk is consumed by its header's line counts, so context lines (which
// look like prose once trimmed) go with it. It also ends at the first
// line that cannot be a diff line, when the side a line belongs to is
// used up, and at a blank line followed by a bullet, so a cited header or
// a truncated hunk does not eat the prose after it. Lines are tested
// whole: only the first sentence of "// Never do X. Always do Y." carries
// the comment marker.
func stripNonProse(text string) string {
	lines := strings.Split(text, "\n")
	drop, any := nonProseLines(lines)
	if !any {
		return text
	}
	out := lines[:0:0]
	for i, line := range lines {
		if !drop[i] {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// clipProse is clip for the text extract reads: the same head and tail of
// the turn, without the pasted data in them. Which lines are data is
// decided on the whole turn, because past the clip a diff's tail has lost
// its hunk header, a fence its opener, and a JSON value its opening
// quote, and what is left reads as prose. The window itself does not
// move, so extract sees nothing of a long turn that it did not see before.
func clipProse(text string) string {
	lines := strings.Split(text, "\n")
	drop, any := nonProseLines(lines)
	if !any {
		return clip(text)
	}
	headEnd, tailStart := len(text), len(text)
	if len(text) > clipWhole {
		headEnd = sentenceEndBefore(text, clipHead)
		tailStart = sentenceStartAfter(text, len(text)-clipTail)
	}
	var head, tail strings.Builder
	off := 0
	for i, line := range lines {
		a, b := off, off+len(line)
		off = b + 1
		if drop[i] {
			continue
		}
		if a < headEnd {
			head.WriteString(text[a:min(b, headEnd)])
			head.WriteByte('\n')
		}
		if b > tailStart {
			tail.WriteString(text[max(a, tailStart):b])
			tail.WriteByte('\n')
		}
	}
	if len(text) <= clipWhole {
		return strings.TrimSuffix(head.String(), "\n")
	}
	return head.String() + "…\n" + strings.TrimSuffix(tail.String(), "\n")
}

// nonProseLines marks the lines of a turn that are pasted data.
func nonProseLines(lines []string) (drop []bool, any bool) {
	drop = make([]bool, len(lines))
	mark := func(from, to int) {
		for k := from; k <= to; k++ {
			drop[k] = true
		}
		any = true
	}
	for i := 0; i < len(lines); i++ {
		if codeFence(lines[i]) {
			// An unclosed fence is left alone: it would swallow the rest
			// of the turn.
			if j := closingFence(lines, i+1); j > 0 {
				mark(i, j)
				i = j
				continue
			}
		}
		if _, _, ok := gate.HunkHeader(lines[i]); ok {
			j := hunkEnd(lines, i)
			mark(i, j)
			i = j
		}
	}
	// Whole JSON values before single lines: dropping member lines one by
	// one would leave an array of strings behind, no longer valid JSON.
	if dropJSONBlocks(lines, drop) {
		any = true
	}
	for i, line := range lines {
		if !drop[i] && (gate.CodeLine(line) || gate.CodeStatement(line)) {
			drop[i], any = true, true
		}
	}
	return drop, any
}

// hunkEnd is the index of the last line of the hunk whose header is at
// lines[i] (i itself when nothing under the header belongs to it).
func hunkEnd(lines []string, i int) int {
	oldN, newN, _ := gate.HunkHeader(lines[i])
	// Bullets straight under a header are the writer's own list.
	if i+1 < len(lines) && bulletRE.MatchString(lines[i+1]) {
		return i
	}
	for (oldN > 0 || newN > 0) && i+1 < len(lines) {
		next := strings.TrimSuffix(lines[i+1], "\r")
		stop := false
		switch {
		case next == "":
			stop = oldN <= 0 || newN <= 0 || (i+2 < len(lines) && bulletRE.MatchString(lines[i+2]))
			oldN, newN = oldN-1, newN-1
		case next[0] == ' ' || next[0] == '\t':
			stop = oldN <= 0 || newN <= 0
			oldN, newN = oldN-1, newN-1
		case next[0] == '+':
			stop = newN <= 0
			newN--
		case next[0] == '-':
			stop = oldN <= 0
			oldN--
		case next[0] == '\\': // "\ No newline at end of file"
		default:
			stop = true
		}
		if stop {
			break
		}
		i++
	}
	return i
}

// bulletRE: a list item at the margin, as opposed to a removed or added
// line. An indented bullet is a context line of a diffed document, and
// "- * text" is a removed comment line.
var bulletRE = regexp.MustCompile("^[-+*] (?:[A-Za-z0-9\"'`\\[]|\\*\\*)")

// plainFence names the fence tags that do not mark source: prose, and
// logs and shell sessions, where a failure may be reported. A bare fence
// is ambiguous and stays with them.
var plainFence = map[string]bool{
	"": true, "text": true, "txt": true, "plain": true, "plaintext": true, "md": true, "markdown": true,
	"log": true, "logs": true, "console": true, "output": true, "stdout": true, "stderr": true,
	"terminal": true, "session": true, "shell-session": true, "sh-session": true, "shellsession": true,
	"sh": true, "bash": true, "zsh": true, "fish": true, "shell": true,
	"powershell": true, "pwsh": true, "ps1": true, "cmd": true, "bat": true,
}

// codeFence reports the opening line of a fenced block that names a
// source or data language: ```go, ```diff title="a.ts". A sentence that
// opens with a tag ("```go fences are dropped") is not an opener: what
// follows the language must look like attributes.
func codeFence(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") || strings.Contains(t[3:], "```") {
		return false
	}
	f := strings.Fields(t[3:])
	if len(f) == 0 || plainFence[strings.ToLower(f[0])] {
		return false
	}
	for _, attr := range f[1:] {
		if !strings.ContainsAny(attr, `={}:."/`) {
			return false
		}
	}
	return true
}

// closingFence is the next bare fence line. Another opener first means
// the block was never closed. A hunk inside the block is stepped over by
// its counts, so a fence that is one of its context lines does not close
// the block.
func closingFence(lines []string, from int) int {
	for j := from; j < len(lines); j++ {
		if _, _, ok := gate.HunkHeader(lines[j]); ok {
			j = hunkEnd(lines, j)
			continue
		}
		t := strings.TrimSpace(lines[j])
		if !strings.HasPrefix(t, "```") {
			continue
		}
		if strings.Trim(t, "`") == "" {
			return j
		}
		return -1
	}
	return -1
}

// dropJSONBlocks marks JSON objects and arrays that stand on their own
// lines (compact or pretty-printed), reading only the lines still kept.
// JSON inside a sentence stays: it is part of what the sentence says.
//
// One pass: a value opens at the start of a line and is followed bracket
// by bracket until it closes. A blank line, a dropped line, or a string
// still open at the end of a line (none of which valid JSON contains)
// abandons it, so a turn full of unclosed braces costs one look per line.
func dropJSONBlocks(lines []string, drop []bool) bool {
	any := false
	start, depth := -1, 0
	var span strings.Builder
	reset := func() {
		start, depth = -1, 0
		span.Reset()
	}
	for i, line := range lines {
		if drop[i] || strings.TrimSpace(line) == "" {
			reset()
			continue
		}
		from := 0
		if start < 0 {
			from = len(line) - len(strings.TrimLeft(line, " \t"))
			if line[from] != '{' && line[from] != '[' {
				continue
			}
			start = i
		}
		end, open := jsonScanLine(line, from, &depth)
		switch {
		case open: // a string ran off the line
			reset()
		case end < 0: // the value continues on the next line
			span.WriteString(line[from:])
			span.WriteByte('\n')
		default:
			span.WriteString(line[from:end])
			if strings.Trim(line[end:], " \t\r,") == "" && strings.Contains(span.String(), `"`) && json.Valid([]byte(span.String())) {
				for k := start; k <= i; k++ {
					drop[k] = true
				}
				any = true
			}
			reset()
		}
	}
	return any
}

// jsonScanLine follows brackets from line[from:], updating depth. It
// returns the index just past the bracket that brings depth back to zero
// (or -1), and whether a string was still open at the end of the line.
func jsonScanLine(line string, from int, depth *int) (end int, openString bool) {
	inStr, esc := false, false
	for i := from; i < len(line); i++ {
		c := line[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			*depth++
		case '}', ']':
			*depth--
			if *depth <= 0 {
				return i + 1, false
			}
		}
	}
	return -1, inStr
}
