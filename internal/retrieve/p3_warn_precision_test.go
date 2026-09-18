package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
)

func warned(out Response, id string) bool {
	for _, w := range out.Warnings {
		if strings.Contains(w, id) {
			return true
		}
	}
	return false
}

// The word-overlap route to a job-1 warning asked for two shared content
// words no matter how long the goal was. A thirty-word goal shares two
// ordinary words with almost any record: on the live store 45 of 130
// September warnings came from pairs like [review look] and [wave count].
// The bar now rises with the goal: two words up to sixteen content tokens,
// then one more per eight.
func TestOverlapNeedScalesWithGoalSize(t *testing.T) {
	for _, c := range []struct{ n, want int }{{0, 2}, {5, 2}, {16, 2}, {17, 3}, {24, 3}, {28, 4}, {55, 7}} {
		if got := overlapNeed(c.n); got != c.want {
			t.Fatalf("overlapNeed(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestLongGoalSharingTwoOrdinaryWordsDoesNotWarn(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JREVIEW", Type: "failed",
		Text:      "Staging restart failed because the daemon raced the migration step.",
		Paths:     []string{"ops/deploy.sh"},
		CreatedAt: "2026-08-18T23:22:40Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal: "Review the object storage branch, merge it to main, ship a new build with the reboot convention; " +
			"then survey the tape of recent sessions, confirm the local daemon is healthy since the restart, " +
			"and note improvements to the pack rendering for the roadmap",
	})
	if warned(out, "01JREVIEW") {
		t.Fatalf("two ordinary shared words on a long goal must stay weak: %v", out.Warnings)
	}
}

// The same two-word overlap on a short goal is a real topic match.
func TestShortGoalTwoSharedWordsStillWarns(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JLIMIT", Type: "failed",
		Text:      "Redis limiter blew the p95 budget under burst load.",
		Paths:     []string{"src/limiter.ts"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "add a redis limiter to the gateway"})
	if !warned(out, "01JLIMIT") {
		t.Fatalf("short goal naming the failed topic must warn: %v pack=%+v", out.Warnings, out.Context)
	}
}

// The project's own name is in every goal and half the records of that
// project ([lossless mcp], [only lossless] on the live store): it is no
// evidence that a record is about this goal.
func TestProjectNameIsNotOverlapEvidence(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JAPI", Type: "failed",
		Text:      "The api rollout on staging failed twice before the cache warmed.",
		Paths:     []string{"ops/rollout.sh"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "tidy the api notes for staging"})
	if warned(out, "01JAPI") {
		t.Fatalf("the project name must not count toward overlap: %v", out.Warnings)
	}
}

// after / only / every / never are function words, not topics.
func TestFunctionWordsAreNotOverlapEvidence(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JSIGN", Type: "failed",
		Text:      "Sign-out failed only after every retry was exhausted.",
		Paths:     []string{"web/session.ts"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "render the album grid only after every thumbnail loads"})
	if warned(out, "01JSIGN") {
		t.Fatalf("function words must not make a job-1 warning: %v", out.Warnings)
	}
}
