package write

import (
	"encoding/json"
	"strings"
)

// splitWorkflowMessage pulls findings[].issue from a child-loop JSON
// body that has asked + severity. Leftover prose is still extracted.
// Not lossless ask JSON.
func splitWorkflowMessage(text string) (issues []string, rest string, ok bool) {
	s := strings.TrimSpace(text)
	if inner, after, fenced := splitLeadingFence(s); fenced {
		if issues, ok = findingsFromObject(inner); ok {
			return issues, strings.TrimSpace(after), true
		}
	}
	rest = s
	spans := jsonObjectSpans(s)
	for i := len(spans) - 1; i >= 0; i-- {
		a, b := spans[i][0], spans[i][1]
		iss, hit := findingsFromObject(s[a:b])
		if !hit {
			continue
		}
		ok = true
		issues = append(iss, issues...)
		rest = strings.TrimSpace(rest[:a] + " " + rest[b:])
	}
	return issues, rest, ok
}

func workflowFindings(text string) (issues []string, ok bool) {
	issues, _, ok = splitWorkflowMessage(text)
	return issues, ok
}

func stripCodeFence(s string) string {
	inner, _, ok := splitLeadingFence(s)
	if !ok {
		return s
	}
	return inner
}

func splitLeadingFence(s string) (inner, after string, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s, "", false
	}
	body := s
	if i := strings.Index(s, "\n"); i >= 0 {
		body = s[i+1:]
	} else {
		body = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s, "```json"), "```JSON"), "```")
	}
	close := strings.Index(body, "```")
	if close < 0 {
		return strings.TrimSpace(body), "", true
	}
	return strings.TrimSpace(body[:close]), strings.TrimSpace(body[close+3:]), true
}

func findingsFromObject(text string) ([]string, bool) {
	var o map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &o) != nil {
		return nil, false
	}
	asked, hasAsked := o["asked"]
	if !hasAsked || !askedTrue(asked) {
		return nil, false
	}
	raw, ok := o["findings"]
	if !ok {
		return nil, false
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	var issues []string
	sawSev := false
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if _, ok := m["severity"]; ok {
			sawSev = true
		}
		issue, _ := m["issue"].(string)
		issue = strings.TrimSpace(issue)
		if issue != "" {
			issues = append(issues, issue)
		}
	}
	if !sawSev {
		return nil, false
	}
	return issues, true
}

// compactWorkflowText keeps leftover prose plus a small findings JSON so
// a 32KB prefix cannot cut the issues. Non-loop bodies still clip.
func compactWorkflowText(text string) string {
	issues, rest, ok := splitWorkflowMessage(text)
	if !ok {
		return clip(text)
	}
	findings := make([]map[string]string, 0, len(issues))
	for _, issue := range issues {
		findings = append(findings, map[string]string{"issue": issue, "severity": "high"})
	}
	payload, err := json.Marshal(map[string]any{"asked": true, "findings": findings})
	if err != nil {
		return clip(text)
	}
	body := string(payload)
	rest = strings.TrimSpace(rest)
	budget := 32<<10 - len(body) - 1
	if budget < 0 {
		if len(body) > 32<<10 {
			return body[:32<<10]
		}
		return body
	}
	if len(rest) > budget {
		rest = rest[:budget]
	}
	if rest == "" {
		return body
	}
	return rest + "\n" + body
}

func askedTrue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "yes" || s == "1"
	case float64:
		return t != 0
	default:
		return false
	}
}
