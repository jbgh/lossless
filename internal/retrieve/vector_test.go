package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/embed"
)

func throttleEmbedder() embed.Embedder {
	return embed.NewCluster(
		[]string{"throttl", "token bucket", "cache idea", "redis pool"},
		[]string{"warehouse", "invoice", "billing export"},
	)
}

func TestVectorParaphraseFindsFailedWithoutSharedTokens(t *testing.T) {
	st := tmpStore(t)
	st.Embedder = throttleEmbedder()
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
	for i := 0; i < 10; i++ {
		writeRec(t, st, claim.Record{
			ID:        "DECOY" + string(rune('A'+i)),
			Type:      "failed",
			Text:      "Playwright flake failed in src/tests/e2e.ts on pass " + string(rune('0'+i%10)) + ".",
			Paths:     []string{"src/tests/e2e.ts"},
			CreatedAt: "2026-08-1" + string(rune('0'+i%6)) + "T00:00:00Z",
		})
	}
	if st.VectorCount() < 2 {
		t.Fatalf("vectors not written: %d", st.VectorCount())
	}

	// No embedder: paraphrase cannot see Redis (no shared token, newer warehouse
	// would be the recent-failed safety net if the union is empty).
	st.Embedder = nil
	cold := askAt(t, st, Request{Project: "acme/api", Goal: "add throttling"})
	if strings.Contains(textsOf(cold), "Redis") {
		t.Fatalf("lexical path should miss redis: %+v", cold)
	}

	st.Embedder = throttleEmbedder()
	out := askAt(t, st, Request{Project: "acme/api", Goal: "add throttling"})
	if !strings.Contains(textsOf(out), "Redis") {
		t.Fatalf("vector paraphrase missed redis: %+v", out)
	}
	if len(out.Context) == 0 || out.Context[0].ID != "01JREDIS" {
		t.Fatalf("redis should rank first under paraphrase: %+v", out.Context)
	}
	if !hasWarn(out, "01JREDIS") {
		t.Fatalf("vector gate should fire job-1 warn: %v", out.Warnings)
	}
	for _, w := range out.Warnings {
		if strings.Contains(w, "01JWARE") {
			t.Fatalf("warehouse must not get job-1 warn: %v", out.Warnings)
		}
	}

	cache := askAt(t, st, Request{Project: "acme/api", Question: "the cache idea we tried"})
	if !strings.Contains(textsOf(cache), "Redis") {
		t.Fatalf("cache-idea paraphrase missed: %+v", cache)
	}
}

func TestVectorCannotBeatFailedOnPath(t *testing.T) {
	st := tmpStore(t)
	st.Embedder = throttleEmbedder()
	writeRec(t, st, claim.Record{
		ID: "ONPATH", Type: "failed",
		Text:      "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-11-03T16:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "COSINE", Type: "decision",
		Text:      "We decided to add throttling notes only in the billing runbook.",
		Paths:     []string{"docs/billing.md"},
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal:    "add throttling",
		Paths:   []string{"src/middleware/auth.ts"},
	})
	if len(out.Context) == 0 || out.Context[0].ID != "ONPATH" {
		t.Fatalf("on-path failed must beat high cosine: %+v", out.Context)
	}
}

func TestMiniLMIntegrationSkippedWithoutEmbedder(t *testing.T) {
	t.Setenv("LOSSLESS_EMBED_CMD", "")
	t.Setenv("LOSSLESS_EMBED_MODEL", "")
	if embed.Open(t.TempDir()) != nil {
		t.Fatal("expected no default embedder")
	}
}

func TestHeadSkipsVectors(t *testing.T) {
	st := tmpStore(t)
	st.Embedder = throttleEmbedder()
	writeRec(t, st, claim.Record{
		ID: "01JREDIS", Type: "failed",
		Text:      "Redis token bucket failed in staging; connection pool exhausted.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2025-11-03T16:00:00Z",
	})
	// HEAD has no lookup tokens, so vectorHits is a no-op even with an embedder.
	out := askAt(t, st, Request{Project: "acme/api"})
	if len(out.Context) == 0 {
		t.Fatal("empty head")
	}
}
