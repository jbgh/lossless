package write

import (
	"strings"
	"testing"
)

func claudeUser(text string, extra string) string {
	return `{"type":"user","isSidechain":false` + extra + `,"message":{"role":"user","content":[{"type":"text","text":` + jsonQuote(text) + `}]}}` + "\n"
}

func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

// Claude Code injects skill bodies and slash-command output as plain
// user text. Skill docs are not the user's constraints; command stdout
// is not the assistant's failure.
func TestParseSkipsSkillAndCommandChrome(t *testing.T) {
	body := claudeUser("Base directory for this skill: /Users/x/.claude/skills/ship\n\n## Rules\nNever post vague replies. Always commit failing tests first.", "") +
		claudeUser("<command-name>/ship</command-name><command-message>ship</command-message><local-command-stdout>Redis token bucket failed in src/middleware/auth.ts staging.</local-command-stdout>", "") +
		claudeUser("Never log Authorization headers in src/middleware/auth.ts.", "")
	msgs, _ := ParseJSONL(body, 0)
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", Harness: "claude", SessionID: "s", Source: "turn"})
	realConstraint := false
	for _, r := range got {
		switch {
		case strings.Contains(r.Text, "vague replies"), strings.Contains(r.Text, "failing tests first"):
			t.Fatalf("skill text stored as claim: %+v", r)
		case strings.Contains(r.Text, "Redis token bucket"):
			t.Fatalf("command stdout stored as claim: %+v", r)
		case strings.Contains(r.Text, "Authorization headers"):
			realConstraint = r.Type == "constraint"
		}
	}
	if !realConstraint {
		t.Fatalf("real user constraint lost: %+v", got)
	}
}

// isMeta is shell output, isCompactSummary is the harness's own summary,
// isSidechain user turns are a parent's prompt to a subagent.
func TestParseHonorsClaudeFlags(t *testing.T) {
	body := claudeUser("Never log Authorization headers in src/meta.ts.", `,"isMeta":true`) +
		claudeUser("Summary: we decided to use jose, not jsonwebtoken, for Edge.", `,"isCompactSummary":true`) +
		strings.Replace(claudeUser("Always use jose in src/side.ts.", ""), `"isSidechain":false`, `"isSidechain":true`, 1)
	msgs, _ := ParseJSONL(body, 0)
	sawCompact := false
	for _, m := range msgs {
		if m.Compact {
			sawCompact = true
		}
	}
	if !sawCompact {
		t.Fatal("isCompactSummary line did not mark Compact")
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", Harness: "claude", SessionID: "s", Source: "turn"})
	for _, r := range got {
		if strings.Contains(r.Text, "src/meta.ts") || strings.Contains(r.Text, "jsonwebtoken") || strings.Contains(r.Text, "src/side.ts") {
			t.Fatalf("flagged line extracted: %+v", r)
		}
	}
}

// A user-typed constraint with an arrow is memory, not chrome.
func TestExtractKeepsUserArrowConstraint(t *testing.T) {
	got := Extract([]Message{{Role: "user", Offset: 1, Text: "Never rename queue → jobs without a migration."}},
		ExtractOpts{ProjectKey: "acme/api", Harness: "grok", SessionID: "s", Source: "turn"})
	for _, r := range got {
		if r.Type == "constraint" && strings.Contains(r.Text, "queue → jobs") {
			return
		}
	}
	t.Fatalf("user arrow constraint dropped: %+v", got)
}
