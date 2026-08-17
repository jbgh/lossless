package write

import "strings"

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
