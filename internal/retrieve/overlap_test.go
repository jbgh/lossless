package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
)

// docs/algorithm.md §7: "One shared word (`rate`) or a 0.05 Jaccard with no
// shared ident is weak, no warning." A long goal sharing one incidental
// English word with a failed claim must not force-pack or warn.
func TestSingleSharedPlainWordDoesNotWarn(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JSTAGE", Type: "failed",
		Text:      "Deploy to staging failed; cron job kept restarting.",
		Paths:     []string{"ops/deploy.sh"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal:    "wire the staging config into the release pipeline",
	})
	for _, w := range out.Warnings {
		if strings.Contains(w, "01JSTAGE") {
			t.Fatalf("single shared plain word must stay weak, got warning: %q", w)
		}
	}
}

// A shared camelCase identifier is a real symbol link: still strong, still warns.
func TestSharedCodeIdentStillWarns(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JBUCKET", Type: "failed",
		Text:      "tokenBucket refill failed; burst overflow past the cap.",
		Paths:     []string{"src/limiter.ts"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal:    "add jitter to the tokenBucket implementation",
	})
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "01JBUCKET") {
			found = true
		}
	}
	if !found {
		t.Fatalf("shared camelCase identifier must stay strong; warnings: %v", out.Warnings)
	}
}

// A targeted ask whose only content token names the thing ("why not
// jsonwebtoken") is a lookup of that identifier: still strong, still warns.
func TestTargetedSingleTokenAskStillWarns(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "why not jsonwebtoken",
	})
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "01JJOSE") {
			found = true
		}
	}
	if !found {
		t.Fatalf("targeted single-token ask must stay strong; warnings: %v", out.Warnings)
	}
}
