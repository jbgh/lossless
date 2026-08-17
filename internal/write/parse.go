package write

import (
	"encoding/json"
	"strings"
)

type Message struct {
	Role   string
	Text   string
	Error  bool
	Skip   bool // system, reasoning, synthetic, own ask I/O — raw only
	Offset int64
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

func stripHarnessChrome(text string) string {
	low := strings.ToLower(text)
	for _, tag := range []string{"system-reminder", "user_info"} {
		open, close := "<"+tag+">", "</"+tag+">"
		for {
			low = strings.ToLower(text)
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
	return strings.TrimSpace(text)
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
	case "session", "model_change", "thinking_level_change",
		"compaction", "branch_summary", "custom", "label",
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
	m.Text = stripHarnessChrome(m.Text)
	if stripped := stripEmbeddedOwnPayload(m.Text); stripped != m.Text {
		m.Text = stripped
	}
	if strings.TrimSpace(m.Text) == "" || looksLikeOwnPayload(m.Text) {
		m.Skip = true
		m.Text = ""
		return m, true
	}
	m.Text = clip(m.Text)
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
			id, _ := m["tool_use_id"].(string)
			body := flatten(m["content"])
			if ownIDs[id] || looksLikeOwnPayload(body) {
				continue
			}
			if body != "" {
				b.WriteString(body)
				b.WriteByte('\n')
			}
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
	switch role {
	case "user", "human":
		return "user"
	case "assistant", "model":
		return "assistant"
	case "tool", "tool_result", "toolResult", "bashExecution":
		return "tool"
	default:
		if role == "" {
			return "other"
		}
		return role
	}
}

func normalizeCodex(o map[string]any, offset int64, ownIDs map[string]bool) (Message, bool) {
	typ, _ := o["type"].(string)
	switch typ {
	case "session_meta", "SessionMeta", "turn_context", "TurnContext":
		return Message{Skip: true, Offset: offset}, true
	case "event_msg", "EventMsg":
		return normalizeCodexEvent(o, offset), true
	case "response_item", "ResponseItem":
		return normalizeCodexItem(o, offset, ownIDs), true
	default:
		return Message{}, false
	}
}

func normalizeCodexEvent(o map[string]any, offset int64) Message {
	p, _ := o["payload"].(map[string]any)
	if p == nil {
		p = o
	}
	ptype, _ := p["type"].(string)
	switch ptype {
	case "user_message":
		return Message{Role: "user", Text: clip(codexText(p)), Offset: offset}
	case "agent_message":
		return Message{Role: "assistant", Text: clip(codexText(p)), Offset: offset}
	default:
		return Message{Skip: true, Offset: offset}
	}
}

func normalizeCodexItem(o map[string]any, offset int64, ownIDs map[string]bool) Message {
	p, _ := o["payload"].(map[string]any)
	if p == nil {
		p = o
	}
	ptype, _ := p["type"].(string)
	switch ptype {
	case "reasoning", "web_search_call":
		return Message{Skip: true, Offset: offset}
	case "function_call", "FunctionCall":
		markOwn(p, ownIDs)
		return Message{Skip: true, Offset: offset}
	case "function_call_output", "FunctionCallOutput":
		id, _ := asString(p["call_id"])
		if id == "" {
			id, _ = asString(p["id"])
		}
		body := codexText(p)
		if body == "" {
			body = flatten(p["output"])
		}
		if ownIDs[id] || looksLikeOwnPayload(body) {
			return Message{Skip: true, Role: "tool", Offset: offset}
		}
		if strings.TrimSpace(body) == "" {
			return Message{Skip: true, Offset: offset}
		}
		return Message{Role: "tool", Text: clip(body), Offset: offset}
	case "message":
		role, _ := asString(p["role"])
		text := flattenSkippingOwn(p["content"], ownIDs)
		if text == "" {
			text = codexText(p)
		}
		if strings.TrimSpace(text) == "" {
			return Message{Skip: true, Offset: offset}
		}
		return Message{Role: mapRole(role), Text: clip(text), Offset: offset}
	default:
		return Message{Skip: true, Offset: offset}
	}
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func codexText(p map[string]any) string {
	for _, k := range []string{"message", "text", "output", "content"} {
		if s := flatten(p[k]); strings.TrimSpace(s) != "" {
			return s
		}
	}
	if parts, ok := p["content"].([]any); ok {
		var b strings.Builder
		for _, part := range parts {
			m, _ := part.(map[string]any)
			if m == nil {
				continue
			}
			if s, _ := m["text"].(string); s != "" {
				b.WriteString(s)
				b.WriteByte('\n')
				continue
			}
			if s, _ := m["input_text"].(string); s != "" {
				b.WriteString(s)
				b.WriteByte('\n')
			}
			if s, _ := m["output_text"].(string); s != "" {
				b.WriteString(s)
				b.WriteByte('\n')
			}
		}
		return b.String()
	}
	return ""
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
				if s, ok := x["text"].(string); ok {
					b.WriteString(s)
				}
			}
			b.WriteByte('\n')
		}
		return b.String()
	default:
		return ""
	}
}

func clip(text string) string {
	if len(text) <= 2000 {
		return text
	}
	return text[:400] + "\n…\n" + text[len(text)-400:]
}
