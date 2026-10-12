package write

import (
	"strings"
	"testing"
)

// omp (oh-my-pi) writes Pi-shaped JSONL with a fixed-width title slot
// first, plus entry types of its own. Only its conversation turns are
// prose; its bookkeeping entries and the file text an `@file` mention
// inlines are not memory.
func TestParseOmpSessionLines(t *testing.T) {
	const sid = "01a130e9-6160-70f1-915d-4a727bf69ef9"
	lines := strings.Join([]string{
		`{"type":"title","v":1,"title":"🅾️ OMP: add rate limiting","source":"auto","pad":"        "}`,
		`{"type":"session","version":3,"id":"` + sid + `","cwd":"/Users/jay/dev/api"}`,
		`{"type":"model_change","id":"e8cb76f5","parentId":null,"model":"openrouter/deepseek/deepseek-v4.1-flash"}`,
		`{"type":"thinking_level_change","id":"897ca6f9","parentId":"e8cb76f5","thinkingLevel":"high","configured":null}`,
		`{"type":"message","id":"c0d97754","parentId":"897ca6f9","message":{"role":"user","content":[{"type":"text","text":"add rate limiting in src/middleware/auth.ts"}]}}`,
		`{"type":"custom","customType":"system-prompt-digest","data":"537bae56d9090045","id":"9d69f04c","parentId":"c0d97754"}`,
		`{"type":"message","id":"b2662fd1","parentId":"9d69f04c","message":{"role":"assistant","content":[{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}]}}`,
		`{"type":"message","id":"b3","message":{"role":"toolResult","toolCallId":"call_1","toolName":"bash","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"message","id":"b4","message":{"role":"bashExecution","content":[{"type":"text","text":"git status"}]}}`,
		`{"type":"message","id":"b5","message":{"role":"pythonExecution","content":[{"type":"text","text":"Never log Authorization headers in /etc/passwd."}]}}`,
		`{"type":"message","id":"b6","message":{"role":"fileMention","content":[{"type":"text","text":"Never push to main from an inlined file."}]}}`,
		`{"type":"custom_message","customType":"image-attachment","content":"x","id":"c5"}`,
		`{"type":"title_change","id":"c6","title":"renamed","source":"auto"}`,
		`{"type":"ttsr_injection","id":"c7","injectedRules":["ruleA"]}`,
		`{"type":"session_init","id":"c7b","systemPrompt":["s"],"tools":["read"]}`,
		`{"type":"mode_change","id":"c7c","mode":"plan"}`,
		`{"type":"compaction","id":"c8","parentId":"c7","summary":"window compacted","firstKeptEntryId":"c0d97754"}`,
		"",
	}, "\n")

	msgs, consumed := ParseJSONL(lines, 0)
	if consumed != int64(len(lines)) {
		t.Fatalf("consumed %d want %d", consumed, len(lines))
	}
	var prose []Message
	skipped := 0
	sawCompact := false
	for _, m := range msgs {
		if m.Compact {
			sawCompact = true
		}
		if m.Skip {
			skipped++
			continue
		}
		prose = append(prose, m)
	}
	if !sawCompact {
		t.Error("compaction entry did not mark a compact")
	}
	if len(prose) != 5 {
		t.Fatalf("prose messages %d: %+v", len(prose), prose)
	}
	if prose[0].Role != "user" || !strings.Contains(prose[0].Text, "add rate limiting in src/middleware/auth.ts") {
		t.Fatalf("user turn: %+v", prose[0])
	}
	if prose[1].Role != "assistant" || !strings.Contains(prose[1].Text, "jose, not jsonwebtoken") {
		t.Fatalf("assistant turn: %+v", prose[1])
	}
	// Tool output stays tool-role: it is not something the user typed, and
	// extract keys off the role.
	for _, m := range prose[2:] {
		if m.Role != "tool" {
			t.Fatalf("tool-shaped turn has role %q: %+v", m.Role, m)
		}
	}
	for _, m := range msgs {
		if strings.Contains(m.Text, "inlined file") {
			t.Fatalf("file mention kept as memory: %+v", m)
		}
	}
	if skipped < 10 {
		t.Fatalf("only %d bookkeeping lines skipped: %+v", skipped, msgs)
	}
}
