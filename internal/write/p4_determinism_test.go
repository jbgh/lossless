package write

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// 2026-09-18 regression check: the same transcript gave five different
// kept sets in five runs. Extract deduped drafts through a map and ranged
// over it, so whenever a batch held more than twelve drafts the cap kept a
// random subset. Ties now keep transcript order.
func TestExtractIsDeterministicPastTheCap(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 8; i++ {
		fmt.Fprintf(&b, "The Redis limiter failed in src/m%d/auth.ts on staging run %d.\n", i, i)
		fmt.Fprintf(&b, "We decided to use jose%d, not jsonwebtoken, in src/d%d/token.ts.\n", i, i)
	}
	msgs := []Message{{Role: "assistant", Text: b.String(), Offset: 1}}
	var first []string
	for run := 0; run < 25; run++ {
		var got []string
		for _, r := range Extract(msgs, ExtractOpts{ProjectKey: "acme/api"}) {
			got = append(got, r.Type+":"+r.Text)
		}
		if run == 0 {
			first = got
			continue
		}
		if strings.Join(got, "|") != strings.Join(first, "|") {
			t.Fatalf("run %d differs from run 0:\n%v\n%v", run, got, first)
		}
	}
	if len(first) != 10 {
		t.Fatalf("want 5 faileds and 5 decisions past the cap, got %d: %v", len(first), first)
	}
	// Ties keep transcript order: the first five of each type survive.
	for _, r := range first {
		for i := 6; i <= 8; i++ {
			if strings.Contains(r, fmt.Sprintf("src/m%d/", i)) || strings.Contains(r, fmt.Sprintf("src/d%d/", i)) {
				t.Fatalf("a later draft displaced an earlier one of the same rank: %v", first)
			}
		}
	}
}

func TestClipSentNeverSplitsARune(t *testing.T) {
	s := strings.Repeat("a", 87) + "—and the rest of the sentence"
	got := clipSent(s, 88)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
		t.Fatalf("invalid or unmarked clip: %q", got)
	}
}

// Narration that reached packs as faileds or decisions on the live store.
func TestPlanningAndStatusNarrationSkips(t *testing.T) {
	for _, s := range []string{
		"When it clears I'll dispatch Task 10, the docs and live-test task in `docs/deploy.md`, carrying the doctor wording for a failed manual run.",
		"Batch 3 now has two ready PRs (#4336, #4337) with #4335 and #4338 following once their runs and the one-string revert in `strings.xml` land.",
		"**Queued with briefs written**, dispatched as slots free: exports including the photo-book PDF (#3765) and a failure-reason migration (#3845) in `backend/migrations`.",
		"Under your `CLAUDE.md` rule a false \"prior attempt failed\" warning is blocking.",
		"Restart, then say \"continue wave 2\" and I'll re-dispatch the three implementers labeled Opus 5 instead of 4.8.",
		"I’ll wait again (no Cancel), then drive dests with deep-links instead of another XCUITest.",
	} {
		if recs := extractAs("assistant", s); len(recs) != 0 {
			t.Fatalf("narration stored: %s", typesOf(recs))
		}
	}
	recs := extractAs("assistant", "We had to revert the Redis limiter in src/limiter.ts after the p95 regression.")
	if len(recs) != 1 || recs[0].Type != "failed" {
		t.Fatalf("a real revert must still store as failed: %s", typesOf(recs))
	}
}
