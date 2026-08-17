package eval

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/embed"
	"lossless/internal/retrieve"
	"lossless/internal/store"
)

func TestActionTapeWalkthrough(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	eng := retrieve.Engine{Store: st, Now: func() time.Time { return now }}
	for _, r := range []claim.Record{
		{ID: "Y-JOSE", Type: "decision", Text: "Picked jose over jsonwebtoken on the Edge runtime.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2025-08-20T18:00:00Z"},
		{ID: "Y-REDIS", Type: "failed", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2025-11-03T16:00:00Z"},
		{ID: "Y-AUTHZ", Type: "constraint", Text: "Never log Authorization headers in src/middleware/auth.ts.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-02-14T12:00:00Z"},
		{ID: "Y-PG", Type: "decision", Text: "We decided to use postgres, not mysql, in src/db/client.ts.", Paths: []string{"src/db/client.ts"}, CreatedAt: "2026-05-01T10:00:00Z"},
		{ID: "Y-BILL", Type: "failed", Text: "Warehouse query failed in src/billing/export.ts because the cursor timed out.", Paths: []string{"src/billing/export.ts"}, CreatedAt: "2026-08-10T09:00:00Z"},
		{ID: "Y-RATE", Type: "decision", Text: "Rate limiter lives in src/middleware/auth.ts as an in-process token bucket.", Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-07-22T09:00:00Z"},
	} {
		r.ProjectKey, r.Harness, r.SessionID, r.Source, r.Status = "acme/api", "grok", "gold", "import", "active"
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}

	ask := func(title string, req retrieve.Request) retrieve.Response {
		t.Helper()
		req.Project = "acme/api"
		start := time.Now()
		out, err := eng.Ask(req)
		if err != nil {
			t.Fatal(title, err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "\n=== %s  (%s) ===\n", title, time.Since(start).Round(time.Microsecond))
		fmt.Fprintf(&b, "q=%q goal=%q paths=%v session=%s\n", req.Question, req.Goal, req.Paths, req.SessionID)
		for i, h := range out.Context {
			fmt.Fprintf(&b, "  %d. %-10s %s\n      %s\n", i+1, h.Type, h.ID, trim(h.Text, 88))
		}
		for _, w := range out.Warnings {
			fmt.Fprintf(&b, "  WARN %s\n", trim(w, 96))
		}
		t.Log(b.String())
		return out
	}

	jwt := ask("1 JWT rich, no path", retrieve.Request{
		Question: "JWT library choice", Goal: "pick a JWT library", SessionID: "claude-1",
	})
	if !containsText(jwt, "jose") {
		t.Fatal("jwt missed jose")
	}
	thin := ask("2 thin continue, same session", retrieve.Request{SessionID: "claude-1"})
	if !containsText(thin, "jose") {
		t.Fatal("thin missed jose")
	}
	if containsText(thin, "Warehouse") {
		t.Fatal("thin leaked warehouse")
	}
	fresh := ask("3 thin on a brand-new session", retrieve.Request{SessionID: "brand-new"})
	if containsText(fresh, "jose") && len(fresh.Context) > 0 && fresh.Context[0].ID == "Y-JOSE" {
		t.Log("new session happened to rank jose first via HEAD — ok")
	}

	ask("4 rate limit on auth.ts", retrieve.Request{
		Goal: "add rate limiting", Paths: []string{"src/middleware/auth.ts"}, SessionID: "grok-auth",
	})
	st.RecordDwell("acme/api", "grok-auth", "Y-REDIS")
	dwell := ask("5 after GET redis: what were we looking at", retrieve.Request{
		Question: "what were we looking at", SessionID: "grok-auth",
	})
	if !containsText(dwell, "Redis") {
		t.Fatal("dwell missed redis")
	}
	bill := ask("6 topic switch to invoices", retrieve.Request{
		Goal: "export invoices", Paths: []string{"src/billing/export.ts"}, SessionID: "grok-auth",
	})
	if len(bill.Context) > 0 && strings.Contains(bill.Context[0].Text, "Redis") {
		t.Fatalf("redis stuck as #1: %+v", bill.Context)
	}
	if !containsText(bill, "Warehouse") {
		t.Fatal("billing missed warehouse")
	}
	if containsText(bill, "Redis") {
		t.Fatalf("redis rode into billing pack: %+v", bill.Context)
	}
	for _, w := range bill.Warnings {
		if strings.Contains(w, "Y-REDIS") {
			t.Fatalf("redis warn after topic switch: %v", bill.Warnings)
		}
	}

	none := ask("7 pathless throttling, no embedder", retrieve.Request{
		Goal: "add throttling", SessionID: "thr-none",
	})
	st.Embedder = embed.NewCluster(
		[]string{"throttl", "token bucket", "cache idea", "redis pool"},
		[]string{"warehouse", "invoice", "billing export"},
	)
	if _, err := st.BackfillVectors(); err != nil {
		t.Fatal(err)
	}
	vec := ask("8 pathless throttling, cluster embedder", retrieve.Request{
		Goal: "add throttling", SessionID: "thr-vec",
	})
	cache := ask("9 the cache idea we tried", retrieve.Request{
		Question: "the cache idea we tried", SessionID: "thr-vec",
	})

	acts, err := st.RecentActions("acme/api", "claude-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	var tape strings.Builder
	fmt.Fprintf(&tape, "\n=== claude-1 tape (%d) ===\n", len(acts))
	for _, a := range acts {
		fmt.Fprintf(&tape, "  %-8s claim=%-8s toks=%v\n", a.Kind, a.ClaimID, a.Tokens)
	}
	t.Log(tape.String())

	t.Logf("no-embedder throttling pack: %s", summarizePacket(none))
	t.Logf("embedder throttling pack:   %s", summarizePacket(vec))
	t.Logf("cache-idea pack:            %s", summarizePacket(cache))
}
