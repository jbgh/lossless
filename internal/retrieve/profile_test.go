package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
)

func TestSelectProfile(t *testing.T) {
	head, err := normalize(Request{Project: "acme/api"})
	if err != nil || selectProfile(head) != ProfileHead {
		t.Fatalf("head: %+v %v", head, err)
	}
	path, err := normalize(Request{Project: "acme/api", Paths: []string{"src/middleware/auth.ts"}})
	if err != nil || selectProfile(path) != ProfilePath {
		t.Fatalf("path: %+v %v", path, err)
	}
	ident, err := normalize(Request{Project: "acme/api", Question: "JWT library choice", Goal: "pick a JWT library"})
	if err != nil || selectProfile(ident) != ProfileIdent {
		t.Fatalf("ident: %+v %v prof=%s", ident, err, selectProfile(ident))
	}
	prose, err := normalize(Request{Project: "acme/api", Question: "what do we already know", Goal: "add rate limiting"})
	if err != nil || selectProfile(prose) != ProfileProse {
		t.Fatalf("prose: %+v %v prof=%s", prose, err, selectProfile(prose))
	}
}

func TestPathProfileBeatsLexicalDecoy(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "ONPATH", Type: "decision",
		Text:      "Keep the in-process limiter in src/middleware/auth.ts.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-06-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "LEXDECOY", Type: "decision",
		Text:      "Add rate limiting notes in the billing runbook only.",
		Paths:     []string{"docs/billing.md"},
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal:    "add rate limiting",
		Paths:   []string{"src/middleware/auth.ts"},
	})
	if len(out.Context) == 0 || out.Context[0].ID != "ONPATH" {
		t.Fatalf("path profile should rank on-path first: %+v", out.Context)
	}
	if strings.Contains(textsOf(out), "billing runbook") && out.Context[0].ID == "LEXDECOY" {
		t.Fatal("lexical decoy won")
	}
}

func TestIdentProfileStillFindsJose(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JJOSE", Type: "decision",
		Text:      "Picked jose over jsonwebtoken on the Edge runtime.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-20T11:00:00Z",
	})
	q, err := normalize(Request{Project: "acme/api", Question: "JWT library choice"})
	if err != nil || selectProfile(q) != ProfileIdent {
		t.Fatalf("want ident: %+v %v", q, err)
	}
	out := askAt(t, st, Request{Project: "acme/api", Question: "JWT library choice", Goal: "pick a JWT library"})
	if !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("ident profile missed jose: %+v", out)
	}
}

func TestOverlapWeightsStaySacred(t *testing.T) {
	if WFailedOverlap <= WShippedOverlap || WShippedOverlap <= 2 {
		t.Fatalf("overlap weights drifted: failed=%v shipped=%v", WFailedOverlap, WShippedOverlap)
	}
	if WFailedWeak >= WShippedOverlap || WFailedWeak >= WFailedOverlap {
		t.Fatalf("weak failed must not outrank sacred overlap: weak=%v", WFailedWeak)
	}
	onPath := scored{failedOverlap: 1, typeRank: 5, path: 1, agree: 1}
	highBM25 := scored{failedOverlap: 0, typeRank: 4, path: 0, bm25: 1, vector: 1, recency: 1, agree: 1}
	oneWord := scored{failedWeak: 1, typeRank: 5, bm25: 1, recency: 1}
	for _, p := range []Profile{ProfilePath, ProfileIdent, ProfileProse} {
		if onPath.preStale(p) <= highBM25.preStale(p) {
			t.Fatalf("%s: failed-on-path lost to lexical: %v vs %v", p, onPath.preStale(p), highBM25.preStale(p))
		}
		if onPath.preStale(p) <= oneWord.preStale(p) {
			t.Fatalf("%s: failed-on-path lost to one-word failed: %v vs %v", p, onPath.preStale(p), oneWord.preStale(p))
		}
	}
}
