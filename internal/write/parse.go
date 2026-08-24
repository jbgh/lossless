package write

import (
	"encoding/json"
	"strings"
)

type Message struct {
	Role    string
	Text    string
	Error   bool
	Skip    bool // system, reasoning, synthetic, own ask I/O — raw only
	Compact bool // session compact event; not a claim
	Offset  int64
}

func ParseJSONL(chunk string, base int64) (msgs []Message, consumed int64) {
	if chunk == "" {
		return nil, 0
	}
	complete := chunk
	if !strings.HasSuffix(chunk, "\n") {
		if i := strings.LastIndex(chunk, "\n"); i >= 0 {
			complete = chunk[:i+1]
		} else {
			return nil, 0
		}
	}
	consumed = int64(len(complete))
	off := base
	ownIDs := map[string]bool{}
	lines := strings.Split(strings.TrimSuffix(complete, "\n"), "\n")
	for _, line := range lines {
		lineLen := int64(len(line) + 1)
		trim := strings.TrimPrefix(strings.TrimSpace(line), "\uFEFF")
		if trim == "" {
			off += lineLen
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(trim), &raw); err != nil {
			off += lineLen
			continue
		}
		if _, red := raw["_redacted"]; red {
			off += lineLen
			continue
		}
		noteOwnTools(raw, ownIDs)
		if m, ok := normalize(raw, off, ownIDs); ok {
			msgs = append(msgs, m)
		}
		off += lineLen
	}
	return msgs, consumed
}

func noteOwnTools(o map[string]any, ownIDs map[string]bool) {
	if tcs, ok := o["tool_calls"].([]any); ok {
		for _, tc := range tcs {
			m, _ := tc.(map[string]any)
			markOwn(m, ownIDs)
		}
	}
	noteOwnInValue(o["content"], ownIDs)
	if msg, ok := o["message"].(map[string]any); ok {
		noteOwnInValue(msg["content"], ownIDs)
	}
	if p, ok := o["payload"].(map[string]any); ok {
		markOwn(p, ownIDs)
	}
	if item, ok := o["item"].(map[string]any); ok {
		markOwn(item, ownIDs)
		if p, ok := item["payload"].(map[string]any); ok {
			markOwn(p, ownIDs)
		}
	}
}

func noteOwnInValue(v any, ownIDs map[string]bool) {
	parts, ok := v.([]any)
	if !ok {
		return
	}
	for _, p := range parts {
		m, _ := p.(map[string]any)
		markOwn(m, ownIDs)
	}
}

func markOwn(m map[string]any, ownIDs map[string]bool) {
	if m == nil {
		return
	}
	name, _ := m["name"].(string)
	id, _ := m["id"].(string)
	if id == "" {
		id, _ = m["tool_use_id"].(string)
	}
	if id == "" {
		id, _ = m["call_id"].(string)
	}
	if isOwnTool(name) && id != "" {
		ownIDs[id] = true
	}
}

func normalize(o map[string]any, offset int64, ownIDs map[string]bool) (Message, bool) {
	if item, ok := o["item"].(map[string]any); ok && o["payload"] == nil {
		if _, has := o["type"].(string); !has || o["type"] == "item" {
			merged := map[string]any{}
			for k, v := range item {
				merged[k] = v
			}
			if _, ok := merged["type"]; !ok {
				merged["type"], _ = item["type"].(string)
			}
			o = merged
		}
	}
	if m, ok := normalizeCodex(o, offset, ownIDs); ok {
		return finishMessage(m)
	}
	typ, _ := o["type"].(string)
	switch typ {
	case "compaction":
		return Message{Skip: true, Compact: true, Offset: offset}, true
	case "session", "model_change", "thinking_level_change",
		"branch_summary", "custom", "label",
		"session_info", "custom_message",
		"last-prompt", "mode", "permission-mode", "attachment",
		"ai-title", "file-history-snapshot", "file-history-delta":
		return Message{Skip: true, Offset: offset}, true
	}
	if typ == "system" || typ == "reasoning" || typ == "backend_tool_call" {
		return Message{Skip: true, Offset: offset}, true
	}
	if _, ok := o["synthetic_reason"]; ok {
		return Message{Skip: true, Offset: offset}, true
	}

	if typ == "tool_result" {
		id, _ := o["tool_call_id"].(string)
		body := flatten(o["content"])
		if body == "" {
			body = flatten(o["text"])
		}
		if ownIDs[id] || looksLikeOwnPayload(body) {
			return Message{Skip: true, Role: "tool", Offset: offset}, true
		}
	}

	role, text := "", ""
	if r, ok := o["role"].(string); ok {
		role = r
		text = flattenSkippingOwn(o["content"], ownIDs)
		if text == "" {
			text = flatten(o["text"])
		}
		if text == "" {
			text = flatten(o["output"])
		}
	} else if msg, ok := o["message"].(map[string]any); ok {
		role, _ = msg["role"].(string)
		text = flattenSkippingOwn(msg["content"], ownIDs)
		if text == "" {
			text = flatten(msg["output"])
		}
		if text == "" {
			text = flatten(msg["summary"])
		}
		if errv, _ := msg["isError"].(bool); errv {
			o["error"] = true
		}
	} else {
		role = typ
		text = flattenSkippingOwn(o["content"], ownIDs)
		if text == "" {
			text = flatten(o["text"])
		}
	}
	if text == "" {
		if onlyOwnTools(o) {
			return Message{Skip: true, Offset: offset}, true
		}
		return Message{}, false
	}
	m := Message{
		Role:   mapRole(role),
		Text:   text,
		Offset: offset,
	}
	if errv, _ := o["error"].(bool); errv {
		m.Error = true
	}
	if typ == "tool_result" {
		m.Role = "tool"
	}
	return finishMessage(m)
}

func finishMessage(m Message) (Message, bool) {
	if m.Skip {
		return m, true
	}
	switch m.Role {
	case "system", "developer":
		m.Skip = true
		m.Text = ""
		return m, true
	}
	m.Text = stripHarnessChrome(m.Text)
	if stripped := stripEmbeddedOwnPayload(m.Text); stripped != m.Text {
		m.Text = stripped
	}
	if strings.TrimSpace(m.Text) == "" || looksLikeOwnPayload(m.Text) {
		m.Skip = true
		m.Text = ""
		return m, true
	}
	m.Text = compactWorkflowText(m.Text)
	return m, true
}

func onlyOwnTools(o map[string]any) bool {
	if tcs, ok := o["tool_calls"].([]any); ok && len(tcs) > 0 {
		for _, tc := range tcs {
			m, _ := tc.(map[string]any)
			name, _ := m["name"].(string)
			if !isOwnTool(name) {
				return false
			}
		}
		return flatten(o["content"]) == "" && flatten(o["text"]) == ""
	}
	if msg, ok := o["message"].(map[string]any); ok {
		return contentOnlyOwn(msg["content"])
	}
	return contentOnlyOwn(o["content"])
}

func contentOnlyOwn(v any) bool {
	parts, ok := v.([]any)
	if !ok || len(parts) == 0 {
		return false
	}
	saw := false
	for _, p := range parts {
		m, _ := p.(map[string]any)
		if m == nil {
			return false
		}
		switch m["type"] {
		case "tool_use":
			name, _ := m["name"].(string)
			if !isOwnTool(name) {
				return false
			}
			saw = true
		case "tool_result":
			if looksLikeOwnPayload(flatten(m["content"])) {
				saw = true
				continue
			}
			name, _ := m["name"].(string)
			if !isOwnTool(name) {
				return false
			}
			saw = true
		case "thinking":
			continue
		default:
			if s, ok := m["text"].(string); ok && strings.TrimSpace(s) != "" {
				return false
			}
		}
	}
	return saw
}

func flattenSkippingOwn(v any, ownIDs map[string]bool) string {
	parts, ok := v.([]any)
	if !ok {
		return flatten(v)
	}
	var b strings.Builder
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			if s, ok := p.(string); ok {
				b.WriteString(s)
				b.WriteByte('\n')
			}
			continue
		}
		typ, _ := m["type"].(string)
		if typ == "thinking" {
			continue
		}
		if typ == "tool_use" || typ == "toolCall" {
			name, _ := m["name"].(string)
			id, _ := m["id"].(string)
			if isOwnTool(name) && id != "" {
				ownIDs[id] = true
			}
			continue
		}
		if typ == "tool_result" {
			// Tool output stays on the tape; it is not claim prose.
			continue
		}
		if s, ok := m["text"].(string); ok {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func mapRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "human":
		return "user"
	case "assistant", "model":
		return "assistant"
	case "tool", "tool_result", "toolresult", "bashexecution":
		return "tool"
	default:
		if role == "" {
			return "other"
		}
		return strings.ToLower(role)
	}
}

func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, p := range t {
			switch x := p.(type) {
			case string:
				b.WriteString(x)
			case map[string]any:
				if s := flattenMapText(x); s != "" {
					b.WriteString(s)
				}
			}
			b.WriteByte('\n')
		}
		return b.String()
	case map[string]any:
		return flattenMapText(t)
	default:
		return ""
	}
}

func flattenMapText(m map[string]any) string {
	for _, k := range []string{"text", "content", "output_text", "input_text"} {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	if parts, ok := m["parts"].([]any); ok {
		return flatten(parts)
	}
	if parts, ok := m["content"].([]any); ok {
		return flatten(parts)
	}
	return ""
}

func clip(text string) string {
	if len(text) <= 2000 {
		return text
	}
	return text[:400] + "\n…\n" + text[len(text)-400:]
}
