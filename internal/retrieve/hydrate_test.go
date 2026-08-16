package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
)

func TestThinAskAfterRichUsesTape(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JJOSE", Type: "decision",
		Text:      "Picked jose over jsonwebtoken on the Edge runtime.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-12-14T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JWARE", Type: "failed",
		Text:      "Warehouse query failed in src/billing/export.ts because the cursor timed out.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-10T09:00:00Z",
	})
	first := askAt(t, st, Request{
		Project: "acme/api", SessionID: "sess-jwt",
		Question: "JWT library choice", Goal: "pick a JWT library",
	})
	if !strings.Contains(textsOf(first), "jose") {
		t.Fatalf("setup: %+v", first)
	}
	thin := askAt(t, st, Request{Project: "acme/api", SessionID: "sess-jwt"})
	if !strings.Contains(textsOf(thin), "jose") {
		t.Fatalf("thin-after-rich missed jose: %+v", thin)
	}
	if strings.Contains(textsOf(thin), "Warehouse") {
		t.Fatalf("thin-after-rich leaked warehouse: %+v", thin)
	}
}

func TestDwellOpensTopic(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JREDIS", Type: "failed",
		Text:      "Redis token bucket failed in staging; connection pool exhausted.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-11-03T16:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JWARE", Type: "failed",
		Text:      "Warehouse query failed in src/billing/export.ts because the cursor timed out.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-10T09:00:00Z",
	})
	st.RecordDwell("acme/api", "sess-dwell", "01JREDIS")
	out := askAt(t, st, Request{
		Project: "acme/api", SessionID: "sess-dwell",
		Question: "what were we looking at",
	})
	if !strings.Contains(textsOf(out), "Redis") {
		t.Fatalf("dwell missed redis: %+v", out)
	}
	if !hasWarn(out, "01JREDIS") && !hasWarn(out, "failed") {
		t.Fatalf("expected failed warn: %v", out.Warnings)
	}
}

func TestServedDoesNotStickAcrossTopic(t *testing.T) {
	st := seed(t)
	auth := askAt(t, st, Request{
		Project: "acme/api", SessionID: "sess-switch",
		Goal: "add rate limiting", Paths: []string{"src/middleware/auth.ts"},
	})
	if !strings.Contains(textsOf(auth), "Redis") {
		t.Fatalf("setup: %+v", auth)
	}
	bill := askAt(t, st, Request{
		Project: "acme/api", SessionID: "sess-switch",
		Goal: "export invoices", Paths: []string{"src/billing/export.ts"},
	})
	if len(bill.Context) == 0 {
		t.Fatal("empty billing ask")
	}
	if strings.Contains(bill.Context[0].Text, "Redis") {
		t.Fatalf("served auth failed stuck as #1 on billing: %+v", bill.Context)
	}
}

func TestWarnClaimID(t *testing.T) {
	if warnClaimID("A prior attempt at this goal failed (see 01JFAIL). Do not repeat.") != "01JFAIL" {
		t.Fatal(warnClaimID("A prior attempt at this goal failed (see 01JFAIL). Do not repeat."))
	}
	if warnClaimID("no id") != "" {
		t.Fatal("empty")
	}
}
