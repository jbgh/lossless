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
	for _, tag := range []string{"system-reminder", "user_info", "agent-reminder", "claude-user-context",
		"command-name", "command-message", "command-args", "local-command-stdout", "local-command-caveat"} {
		open, close := "<"+tag+">", "</"+tag+">"
		for {
			low := asciiLower(text)
			i := strings.Index(low, open)
			if i < 0 {
				break
			}
			if j := strings.Index(low[i:], close); j >= 0 {
				text = text[:i] + text[i+j+len(close):]
				continue
			}
			text = text[:i]
			break
		}
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
