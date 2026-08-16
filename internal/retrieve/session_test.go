package retrieve

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
)

func TestAuthRateLimitNotBuriedByBillingRateLimit(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "BILLRL", Type: "failed",
		Text:      "Stripe rate limit failed in src/billing/export.ts after 429s.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-15T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "AUTHRL", Type: "failed",
		Text:      "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-01T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "Redis") {
		t.Fatalf("auth failed missed: %+v", out)
	}
	if len(out.Context) > 0 && strings.Contains(out.Context[0].Text, "Stripe") {
		t.Fatalf("billing rate-limit ranked above auth: %+v", out.Context)
	}
}

func TestNewerDecisionBeatsOlderConflictOnSamePath(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "OLDREDIS", Type: "decision",
		Text:      "We decided to use Redis as the token bucket store in src/middleware/auth.ts.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-11-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "NEWREDIS", Type: "decision",
		Text:      "We decided not to use Redis for rate limits in src/middleware/auth.ts.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "not to use Redis") {
		t.Fatalf("missing newer decision: %+v", out)
	}
	for _, h := range out.Context {
		if h.ID == "OLDREDIS" {
			t.Fatalf("older conflicting decision still packed: %+v", out.Context)
		}
	}
}

func TestTokenBucketSnakeAndCamelMatch(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JBUCKET", Type: "decision",
		Text:      "Picked tokenBucket over a raw counter in src/middleware/auth.ts.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-20T11:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "why token_bucket in auth",
		Goal:     "implement token_bucket",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "tokenBucket") {
		t.Fatalf("camel/snake miss: %+v", out)
	}
}

func TestJWTParaphraseFindsJoseWithoutPath(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JJOSE", Type: "decision",
		Text:      "Picked jose over jsonwebtoken on the Edge runtime.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-20T11:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "JWT library choice",
		Goal:     "pick a JWT library",
	})
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("paraphrase missed jose: %+v", out)
	}
}

func TestOldJoseSurvivesRecentBillingFailed(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JJOSEOLD", Type: "decision",
		Text:      "Use jose, not jsonwebtoken, for Edge.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-12-14T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JBILLF", Type: "failed",
		Text:      "Invoice export timed out against the warehouse API.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-12T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "why not a JWT library",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("C3 buried jose: %+v", out)
	}
}

func TestHeadTypeCapKeepsOldConstraint(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JAUTHZOLD", Type: "constraint",
		Text:      "Never log Authorization headers.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-10-01T00:00:00Z",
	})
	for i := 0; i < 30; i++ {
		writeRec(t, st, claim.Record{
			ID:        fmt.Sprintf("01JDEC%02d", i),
			Type:      "failed",
			Text:      fmt.Sprintf("Decoy failure number %d in src/decoy/file%d.ts exploded.", i, i),
			Paths:     []string{fmt.Sprintf("src/decoy/file%d.ts", i)},
			CreatedAt: fmt.Sprintf("2026-08-%02dT00:00:00Z", 1+i%13),
		})
	}
	out := askAt(t, st, Request{Project: "acme/api"})
	if !strings.Contains(textsOf(out), "Never log Authorization") {
		t.Fatalf("C4 missing constraint: %+v", out)
	}
}

func TestOldFailedStillPacked(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JFAIL200", Type: "failed",
		Text:      "Redis token bucket failed in staging; connection pool exhausted.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-01-26T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what do we know about rate limiting on auth?",
		Goal:     "add rate limiting",
		Paths:    []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "Redis token bucket failed") {
		t.Fatalf("C5: %+v", out)
	}
	if !hasWarn(out, "failed") {
		t.Fatalf("C5 warn: %v", out.Warnings)
	}
	age := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC).Sub(time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC)).Hours() / 24
	if age < 180 {
		t.Fatalf("fixture not old enough: %.0f days", age)
	}
}

func TestHeadMixesTypesAndDropsJoseRestatement(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JA", Type: "decision",
		Text: "Use jose, not jsonwebtoken, for Edge.", CreatedAt: "2026-07-20T11:00:00Z",
		Paths: []string{"src/middleware/auth.ts"},
	})
	writeRec(t, st, claim.Record{
		ID: "01JB", Type: "decision",
		Text: "Use jose not jsonwebtoken for Edge runtime.", CreatedAt: "2026-07-21T11:00:00Z",
		Paths: []string{"src/middleware/auth.ts"},
	})
	writeRec(t, st, claim.Record{
		ID: "01JC", Type: "constraint",
		Text: "Never log Authorization headers.", CreatedAt: "2026-06-01T00:00:00Z",
		Paths: []string{"src/middleware/auth.ts"},
	})
	writeRec(t, st, claim.Record{
		ID: "01JF", Type: "failed",
		Text: "Redis token bucket failed in staging; connection pool exhausted.",
		CreatedAt: "2026-08-01T18:12:00Z", Paths: []string{"src/middleware/auth.ts"},
	})
	out := askAt(t, st, Request{Project: "acme/api"})
	nJose := 0
	types := map[string]bool{}
	for _, h := range out.Context {
		types[h.Type] = true
		if strings.Contains(h.Text, "jose") {
			nJose++
		}
	}
	if nJose > 1 {
		t.Fatalf("C7 packed %d jose: %+v", nJose, out.Context)
	}
	if !types["failed"] || !types["constraint"] {
		t.Fatalf("C7 type mix: %+v", out.Context)
	}
}

func TestTypeRecency(t *testing.T) {
	if typeRecency("constraint", 400) != 1 {
		t.Fatal("constraint")
	}
	if typeRecency("decision", 180) >= 0.6 {
		t.Fatal("decision half-life")
	}
	if typeRecency("failed", 14) >= 0.6 {
		t.Fatal("failed half-life")
	}
	if typeRecency("state", 7) >= 0.6 {
		t.Fatal("state")
	}
}

func TestCoverSimHeadSameType(t *testing.T) {
	a := scored{rec: claim.Record{Type: "failed", Text: "alpha", Paths: []string{"a.ts"}}}
	b := scored{rec: claim.Record{Type: "failed", Text: "beta other words", Paths: []string{"b.ts"}}}
	if coverSim(a, b, true) != 1 {
		t.Fatal("head same type")
	}
	if coverSim(a, b, false) == 1 {
		t.Fatal("hot different paths")
	}
	c := scored{rec: claim.Record{Type: "failed", Text: "gamma", Paths: []string{"src/a.ts"}}}
	if !sharePathCluster(a, c) {
		t.Fatal("basename cluster")
	}
}

func TestAuthorizationConstraintWarns(t *testing.T) {
	st := seed(t)
	out := askAt(t, st, Request{Project: "acme/api", Question: "Authorization headers"})
	if !hasWarn(out, "constraint") && !hasWarn(out, "standing") {
		t.Fatalf("expected constraint warning: %v", out.Warnings)
	}
}
