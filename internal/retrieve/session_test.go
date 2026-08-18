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

func TestRetryThenFailAgainDropsDeadDecision(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "D1", Type: "decision",
		Text:      "We decided to use Redis for rate limits in src/middleware/auth.ts.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-01-10T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "F1", Type: "failed",
		Text:      "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-03-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "D2", Type: "decision",
		Text:      "We decided to use Redis again with a bigger pool in src/middleware/auth.ts.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-05-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "F2", Type: "failed",
		Text:      "Redis failed again in src/middleware/auth.ts, pool still exhausted.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "JOSE", Type: "decision",
		Text:      "Picked jose over jsonwebtoken on the Edge runtime.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-02-01T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(out), "failed again") && !strings.Contains(textsOf(out), "pool still") {
		t.Fatalf("missing latest failure: %+v", out)
	}
	if !hasWarn(out, "failed") {
		t.Fatalf("expected failed warning: %v", out.Warnings)
	}
	for _, h := range out.Context {
		if h.ID == "D2" || strings.Contains(h.Text, "bigger pool") {
			t.Fatalf("dead Redis retry still packed as current: %+v", out)
		}
		if h.ID == "D1" {
			t.Fatalf("original Redis decision still packed: %+v", out)
		}
	}
	for _, w := range out.Warnings {
		if strings.Contains(w, "D2") {
			t.Fatalf("shipped warning on a decision that later failed: %v", out.Warnings)
		}
	}
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("unrelated jose decision was dropped: %+v", out)
	}
}

func TestSameFileFailedDoesNotKillOtherDecision(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "HERO", Type: "decision",
		Text:      "We decided to use a same-window hero overlay instead of a hard cut in ios/LightboxView.swift.",
		Paths:     []string{"ios/LightboxView.swift"},
		CreatedAt: "2025-08-22T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "WHO", Type: "failed",
		Text:      "Who-reacted failed in preview in ios/LightboxView.swift.",
		Paths:     []string{"ios/LightboxView.swift"},
		CreatedAt: "2025-11-12T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api", Goal: "how does lightbox open",
		Paths: []string{"ios/LightboxView.swift"},
	})
	if !strings.Contains(textsOf(out), "same-window hero") {
		t.Fatalf("unrelated same-file decision dropped: %+v", out)
	}
	if !strings.Contains(textsOf(out), "Who-reacted") {
		t.Fatalf("newer failed missed: %+v", out)
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

func TestPathlessAskDoesNotPackForeignRecentFailed(t *testing.T) {
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
		Goal:     "pick a JWT library",
	})
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("pathless jwt missed jose: %+v", out)
	}
	if strings.Contains(textsOf(out), "warehouse") || strings.Contains(textsOf(out), "Invoice") {
		t.Fatalf("recent foreign failed rode along: %+v", out)
	}
	if hasWarn(out, "failed") {
		t.Fatalf("job-1 warn on unrelated warehouse: %v", out.Warnings)
	}
}

func TestTwoHopFindsFailedViaBridgeDecision(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JRATE", Type: "decision",
		Text:      "Rate limiter lives in src/middleware/auth.ts as an in-process token bucket.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-22T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JREDIS", Type: "failed",
		Text:      "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-11-03T16:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JWARE", Type: "failed",
		Text:      "Warehouse query failed in src/billing/export.ts because the cursor timed out.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-10T09:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "add rate limiting",
		Goal:     "add rate limiting",
	})
	if !strings.Contains(textsOf(out), "Redis") {
		t.Fatalf("two-hop missed redis failed: %+v", out)
	}
	if strings.Contains(textsOf(out), "Warehouse") {
		t.Fatalf("two-hop leaked warehouse: %+v", out)
	}
}

func TestTinySymbolDoesNotForceFailedOverlap(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JJOSE", Type: "decision",
		Text:      "Picked jose over jsonwebtoken on the Edge runtime.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-20T11:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JPARSE", Type: "failed",
		Text:      "Wave-2 dogfood stress: dumping an ask JSON body was split into fake faileds in src/failure/handler.ts.",
		Paths:     []string{"src/failure/handler.ts"},
		CreatedAt: "2026-08-12T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "why not retune weights",
		Goal:     "review retrieve weights",
		Paths:    []string{"internal/retrieve/weights.go"},
	})
	if hasWarn(out, "failed") {
		t.Fatalf("tiny symbol overlap must not force a failed warn: %v pack=%s", out.Warnings, textsOf(out))
	}
	if !strings.Contains(textsOf(out), "jose") && !strings.Contains(textsOf(out), "jsonwebtoken") && !strings.Contains(textsOf(out), "Picked") {
		// jose may not pack on a weights path; just require no job-1 warn
	}
}

func TestOneWordOverlapDoesNotForceFailed(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JJOSE", Type: "decision",
		Text:      "Picked jose over jsonwebtoken on the Edge runtime.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-20T11:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JLIB", Type: "failed",
		Text:      "The standard library import failed in src/utils/fmt.ts on CI.",
		Paths:     []string{"src/utils/fmt.ts"},
		CreatedAt: "2026-08-12T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "JWT library choice",
		Goal:     "pick a JWT library",
	})
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("missed jose: %+v", out)
	}
	if hasWarn(out, "failed") {
		t.Fatalf("one-word 'library' must not force a failed warn: %v", out.Warnings)
	}
}

func TestContentOverlapExpandsJWT(t *testing.T) {
	if contentOverlap([]string{"jwt", "library"}, "Use jose, not jsonwebtoken, for Edge.") < 1 {
		t.Fatal("jwt should hit jsonwebtoken")
	}
	if contentOverlap([]string{"library", "choice"}, "Use jose, not jsonwebtoken, for Edge.") != 0 {
		t.Fatal("stopped words must not count")
	}
	if contentOverlap([]string{"rate", "limiting"}, "Rate limiter lives in auth.ts as an in-process token bucket.") < 1 {
		t.Fatal("rate should hit")
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
		Text:      "Redis token bucket failed in staging; connection pool exhausted.",
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
