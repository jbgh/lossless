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
	fenced := stripCodeFence(s)
	if issues, ok = findingsFromObject(fenced); ok {
		return issues, "", true
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
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func findingsFromObject(text string) ([]string, bool) {
	var o map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &o) != nil {
		return nil, false
	}
	if _, asked := o["asked"]; !asked {
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
