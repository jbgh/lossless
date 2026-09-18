package watch

import (
	"strings"
	"testing"

	"lossless/internal/write"
)

// A turn hook gets 400ms; past that it spools and the daemon, which kept
// working, has usually moved the cursor by the time the tick replays the
// job. 103 of 193 serve.log lines were that no-op, with no timestamp.
func TestReplayLogLineOnlyWhenSomethingHappened(t *testing.T) {
	if got := replayLogLine(write.EnsureResult{Replayed: 1}); got != "" {
		t.Fatalf("a replay that moved no tape must not log: %q", got)
	}
	got := replayLogLine(write.EnsureResult{Replayed: 2, Moved: 1})
	if !strings.Contains(got, "replayed 2 spooled catch-ups") || !strings.Contains(got, "1 moved tape") {
		t.Fatalf("%q", got)
	}
	if got := replayLogLine(write.EnsureResult{Failed: 1, Errors: []string{"x.json: boom"}}); !strings.Contains(got, "1 failed") || !strings.Contains(got, "boom") {
		t.Fatalf("a failed replay must log its error: %q", got)
	}
}
