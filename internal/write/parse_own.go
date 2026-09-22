package write

import (
	"encoding/json"
	"strings"
)

func isOwnTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "-", "_")
	for _, p := range []string{"mcp__lossless__", "mcp_lossless_", "lossless__", "lossless_"} {
		n = strings.TrimPrefix(n, p)
	}
	switch n {
	case "ask", "remember", "catch_up", "catchup", "get_record", "getrecord":
		return true
	default:
		return false
	}
}

func looksLikeOwnPayload(text string) bool {
	if ownPayloadObject(strings.TrimSpace(text)) {
		return true
	}
	for _, span := range jsonObjectSpans(text) {
		if ownPayloadObject(text[span[0]:span[1]]) {
			return true
		}
	}
	return false
}

func ownPayloadObject(text string) bool {
	var o map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &o) != nil {
		return false
	}
	if _, w := o["warnings"]; w {
		if _, c := o["context"]; c {
			return true
		}
	}
	if _, ok := o["extracted"]; ok {
		if _, ok := o["copied"]; ok {
			return true
		}
		if _, ok := o["ids"]; ok {
			return true
		}
	}
	typ, _ := o["type"].(string)
	_, hasText := o["text"]
	_, hasID := o["id"]
	switch typ {
	case "failed", "decision", "constraint", "state", "thread":
		if hasText && hasID {
			return true
		}
	}
	return false
}

func jsonObjectSpans(s string) [][2]int {
	var spans [][2]int
	depth, start := 0, -1
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				spans = append(spans, [2]int{start, i + 1})
			}
		}
	}
	return spans
}

func stripEmbeddedOwnPayload(text string) string {
	spans := jsonObjectSpans(text)
	for i := len(spans) - 1; i >= 0; i-- {
		a, b := spans[i][0], spans[i][1]
		if ownPayloadObject(text[a:b]) {
			text = strings.TrimSpace(text[:a] + " " + text[b:])
		}
	}
	return strings.TrimSpace(text)
}

// asciiLower folds only A-Z so byte offsets match the original text.
// strings.ToLower changes byte lengths for İ, K and friends, and an
// index from the folded copy then slices the original out of bounds.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func stripHarnessChrome(text string) string {
	text = stripTaskNotifications(text)
	for _, tag := range []string{"system-reminder", "user_info", "agent-reminder", "claude-user-context",
		"command-name", "command-message", "command-args", "local-command-stdout", "local-command-caveat",
		"skills", "skill"} {
		text = stripTagSpan(text, tag)
	}
	for {
		i := strings.Index(text, "<!--")
		if i < 0 {
			break
		}
		if j := strings.Index(text[i:], "-->"); j >= 0 {
			text = text[:i] + text[i+j+3:]
			continue
		}
		text = text[:i]
		break
	}
	if strings.Contains(text, "<<<<<<<") {
		text = dropConflictHEAD(text)
	}
	return strings.TrimSpace(text)
}

func dropConflictHEAD(s string) string {
	var b strings.Builder
	inHead := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "<<<<<<<"):
			inHead = true
			continue
		case strings.HasPrefix(t, "======="):
			inHead = false
			continue
		case strings.HasPrefix(t, ">>>>>>>"):
			inHead = false
			continue
		}
		if inHead {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// agentHandBack returns the report bodies of the <agent-message> blocks in
// a Claude Code hand-back, without the harness frame ("[Subagent
// hand-back] … The report follows:") or the text around the blocks
// ("Another Claude session sent a message:", the trailing caveat). The
// harness indents every report line by two spaces.
func agentHandBack(text string) (string, bool) {
	const open, close = "<agent-message", "</agent-message>"
	const follows = "The report follows:"
	low := asciiLower(text)
	if !strings.Contains(low, open) {
		return "", false
	}
	var out strings.Builder
	for pos := 0; pos < len(text); {
		i := strings.Index(low[pos:], open)
		if i < 0 {
			break
		}
		i += pos
		gt := strings.IndexByte(text[i:], '>')
		if gt < 0 {
			break
		}
		start := i + gt + 1
		end, next := len(text), len(text)
		if j := strings.Index(low[start:], close); j >= 0 {
			end = start + j
			next = end + len(close)
		}
		inner := text[start:end]
		if k := strings.Index(inner, follows); k >= 0 && strings.Contains(inner[:k], "[Subagent hand-back]") {
			inner = inner[k+len(follows):]
		}
		inner = strings.ReplaceAll(inner, "\n  ", "\n")
		out.WriteString(strings.TrimSpace(inner))
		out.WriteByte('\n')
		pos = next
	}
	return strings.TrimSpace(out.String()), true
}

// onlyTaskNotifications is true when the text is nothing but
// <task-notification> blocks: a queued subagent or background-command
// result, not something the user typed alongside one.
func onlyTaskNotifications(text string) bool {
	const open, close = "<task-notification>", "</task-notification>"
	low := asciiLower(text)
	if !strings.Contains(low, open) {
		return false
	}
	for {
		i := strings.Index(low, open)
		if i < 0 {
			break
		}
		end := len(text)
		if j := strings.Index(low[i:], close); j >= 0 {
			end = i + j + len(close)
		}
		text = text[:i] + text[end:]
		low = low[:i] + low[end:]
	}
	return strings.TrimSpace(text) == ""
}

// stripTaskNotifications reduces a Claude Code <task-notification> block
// to its <result> bodies. The summary ("Background command … failed with
// exit code 1"), note, ids, and status are harness chrome, not claims.
// stripTagSpan drops <tag …>…</tag> spans. The open tag may carry
// attributes: Pi injects skill bodies as
// <skill name=… location=…>…</skill>, and a skill's rules and examples
// are not the user's constraints. An unclosed span drops to the end of
// the text.
func stripTagSpan(text, tag string) string {
	open, close := "<"+tag, "</"+tag+">"
	for {
		low := asciiLower(text)
		i := tagOpen(low, open)
		if i < 0 {
			return text
		}
		next := len(text)
		if j := strings.Index(low[i:], close); j >= 0 {
			next = i + j + len(close)
		}
		text = text[:i] + text[next:]
	}
}

// tagOpen finds open only at a tag-name boundary: <skillful> is not
// <skill>.
func tagOpen(low, open string) int {
	for from := 0; ; {
		i := strings.Index(low[from:], open)
		if i < 0 {
			return -1
		}
		i += from
		j := i + len(open)
		if j >= len(low) || low[j] == '>' || low[j] == '/' || low[j] <= ' ' {
			return i
		}
		from = i + 1
	}
}

func stripTaskNotifications(text string) string {
	const open, close = "<task-notification>", "</task-notification>"
	for {
		low := asciiLower(text)
		i := strings.Index(low, open)
		if i < 0 {
			return text
		}
		end := len(text)
		next := len(text)
		if j := strings.Index(low[i:], close); j >= 0 {
			end = i + j
			next = end + len(close)
		}
		block := text[i:end]
		var keep strings.Builder
		blow := asciiLower(block)
		for {
			a := strings.Index(blow, "<result>")
			if a < 0 {
				break
			}
			b := strings.Index(blow[a:], "</result>")
			if b < 0 {
				keep.WriteString(block[a+len("<result>"):])
				break
			}
			keep.WriteString(block[a+len("<result>") : a+b])
			keep.WriteByte('\n')
			block = block[a+b+len("</result>"):]
			blow = blow[a+b+len("</result>"):]
		}
		text = text[:i] + keep.String() + text[next:]
	}
}
