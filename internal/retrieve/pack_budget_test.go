package retrieve

import (
	"testing"

	"lossless/internal/claim"
)

// evictFailed must not blow limit_tokens: a job-1 failed it re-adds
// still counts against the budget the packer already enforced.
func TestEvictFailedRespectsTokenBudget(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JPAYA", Type: "failed",
		Text: "Stripe webhook replay in src/pay.ts failed under concurrent capture; " +
			"the idempotency key table deadlocked and refunds double-posted until " +
			"the queue drained, so the retry worker had to be stopped by hand.",
		Paths:     []string{"src/pay.ts"},
		CreatedAt: "2026-08-10T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JPAYB", Type: "failed",
		Text: "Moving capture into the checkout transaction failed too; src/pay.ts " +
			"timed out against the gateway sandbox and left carts locked, and the " +
			"cleanup migration burned an afternoon before we rolled it back.",
		Paths:     []string{"src/pay.ts"},
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:     "acme/api",
		Goal:        "rework payment capture",
		Paths:       []string{"src/pay.ts"},
		LimitTokens: 120,
	})
	if len(out.Context) != 1 {
		t.Fatalf("budget fits one record, packed %d", len(out.Context))
	}
}
