package write

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The tail window of a clipped message must start on a sentence boundary.
// A mid-word cut ("al/bench_test.go 0.95 floor …") became a live claim.
func TestClipCutsAtSentenceBoundaries(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString("Sentence number ")
		b.WriteString(strings.Repeat("x", i%7+1))
		b.WriteString(" about eval/bench_test.go floors. ")
	}
	src := b.String()
	got := clip(src)
	if len(got) >= len(src) || !strings.Contains(got, "\n…\n") {
		t.Fatalf("not clipped: %d of %d", len(got), len(src))
	}
	parts := strings.SplitN(got, "\n…\n", 2)
	head, tail := parts[0], parts[1]
	if !strings.HasPrefix(src, head) || !strings.HasSuffix(src, tail) {
		t.Fatal("head/tail are not prefix/suffix of the source")
	}
	if !strings.HasSuffix(strings.TrimSpace(head), ".") {
		t.Fatalf("head ends mid-sentence: %q", head[len(head)-40:])
	}
	if !strings.HasPrefix(tail, "Sentence number ") {
		t.Fatalf("tail starts mid-sentence: %q", tail[:40])
	}
}

// A failure sentence in the middle of a 3KB assistant report still
// reaches extract. 400/400 dropped every middle.
func TestClipKeepsMiddleOfMediumMessage(t *testing.T) {
	filler := strings.Repeat("The overlay drops one extra frame later so the pager never paints black. ", 20)
	src := filler + "Redis token bucket failed in src/middleware/auth.ts staging. " + filler
	if len(src) < 2500 || len(src) > 4000 {
		t.Fatalf("fixture size %d", len(src))
	}
	if got := clip(src); !strings.Contains(got, "Redis token bucket failed") {
		t.Fatalf("middle lost: %d bytes kept of %d", len(got), len(src))
	}
}

// Claude Code queues a subagent / background-command result as a user
// turn wrapped in <task-notification>. The scaffolding (summary, note,
// ids) is harness chrome; the <result> body is prose worth extracting.
func TestParseTaskNotificationKeepsResultDropsChrome(t *testing.T) {
	content := "<task-notification>\\n<task-id>a36541c6c41d79951</task-id>\\n<tool-use-id>toolu_01WmmGsUXiReyprULjUxvCxP</tool-use-id>\\n<output-file>/private/tmp/x.output</output-file>\\n<status>completed</status>\\n<summary>Agent \\\"Angle B audit\\\" finished</summary>\\n<note>A task-notification fires each time this agent stops with no live background children of its own.</note>\\n<result>Audit complete. Redis token bucket failed in src/middleware/auth.ts staging.</result>\\n</task-notification>"
	line := `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-31T21:38:53.597Z","sessionId":"s","content":"` + content + `"}` + "\n"
	msgs, _ := ParseJSONL(line, 0)
	if len(msgs) != 1 || msgs[0].Skip {
		t.Fatalf("expected one kept message: %+v", msgs)
	}
	m := msgs[0]
	// A message that is nothing but the notification is the subagent's or
	// the command's output, not something the user typed: it extracts as
	// assistant text and cannot mint a user constraint.
	if m.Role != "assistant" {
		t.Fatalf("role %q", m.Role)
	}
	if !strings.Contains(m.Text, "Redis token bucket failed in src/middleware/auth.ts staging.") {
		t.Fatalf("result body lost: %q", m.Text)
	}
	for _, bad := range []string{"Angle B audit", "task-notification fires", "toolu_", "/private/tmp", "completed"} {
		if strings.Contains(m.Text, bad) {
			t.Fatalf("chrome kept %q in %q", bad, m.Text)
		}
	}
}

// A background-command notification has only a summary: nothing to keep.
func TestParseTaskNotificationSummaryOnlySkips(t *testing.T) {
	content := "<task-notification>\\n<task-id>b1</task-id>\\n<status>failed</status>\\n<summary>Background command \\\"Wait for main 4536; check the #3804 re-review\\\" failed with exit code 1</summary>\\n</task-notification>"
	line := `{"type":"queue-operation","operation":"enqueue","sessionId":"s","content":"` + content + `"}` + "\n"
	msgs, _ := ParseJSONL(line, 0)
	for _, m := range msgs {
		if !m.Skip && strings.TrimSpace(m.Text) != "" {
			t.Fatalf("summary-only notification should not yield text: %q", m.Text)
		}
	}
}

// A cut that has to fall back past every boundary must still not split a
// rune: the tail is what extract tokenizes.
func TestClipNeverSplitsARune(t *testing.T) {
	src := "a" + strings.Repeat("€", 4000)
	got := clip(src)
	if !utf8.ValidString(got) {
		t.Fatal("clip produced invalid UTF-8")
	}
	parts := strings.SplitN(got, "\n…\n", 2)
	if len(parts) != 2 || !utf8.ValidString(parts[0]) || !utf8.ValidString(parts[1]) {
		t.Fatalf("head/tail not valid UTF-8: %d parts", len(parts))
	}
}

// The last sentence of a turn is usually followed by a trailing newline.
// Skipping that whitespace must not run the tail off the end of the text.
func TestClipTailKeepsFinalSentence(t *testing.T) {
	src := strings.Repeat("word ", 900) + "Redis token bucket failed in src/auth.ts.\n"
	got := clip(src)
	parts := strings.SplitN(got, "\n…\n", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		t.Fatalf("empty tail: %q", got[len(got)-60:])
	}
	if !strings.Contains(parts[1], "Redis token bucket failed") {
		t.Fatalf("final sentence lost: %q", parts[1])
	}
}

// An unclosed <task-notification> (truncated block) keeps its result once
// and drops the summary; it must not re-append the scanned remainder.
func TestStripTaskNotificationUnclosedBlock(t *testing.T) {
	in := "<task-notification>\n<summary>Background command failed with exit code 1</summary>\n<result>partial output"
	got := stripTaskNotifications(in)
	if strings.Count(got, "partial output") != 1 || strings.Contains(got, "summary") || strings.Contains(got, "<result>") {
		t.Fatalf("%q", got)
	}
}
