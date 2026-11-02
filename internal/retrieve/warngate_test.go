package retrieve

import (
	"testing"

	"lossless/internal/claim"
)

// A packed failed warns only when it is deliberate memory or shares a file
// with the ask; a turn-extracted failed that matched on words alone still
// packs, silently. A packed decision never warns.
func TestFailedWarnsOnlyWhenVouched(t *testing.T) {
	text := "The `TokenBucket` rate limiter failed under burst load on staging."
	goal := "add a token bucket rate limiter for burst load"
	for _, tc := range []struct {
		name, source string
		paths, ask   []string
		warn         bool
	}{
		{name: "turn narration", source: "turn", warn: false},
		{name: "remembered", source: "remember", warn: true},
		{name: "turn on the asked file", source: "turn", paths: []string{"src/limit.go"}, ask: []string{"src/limit.go"}, warn: true},
	} {
		st := tmpStore(t)
		writeRec(t, st, claim.Record{
			ID: "F1", Type: "failed", Source: tc.source, Text: text,
			Paths: tc.paths, CreatedAt: "2026-08-10T00:00:00Z",
		})
		out := askAt(t, st, Request{Project: "acme/api", Goal: goal, Paths: tc.ask})
		packed := false
		for _, h := range out.Context {
			packed = packed || h.ID == "F1"
		}
		if !packed {
			t.Fatalf("%s: the failed must still pack: %+v", tc.name, out.Context)
		}
		if got := hasWarn(out, "F1"); got != tc.warn {
			t.Fatalf("%s: warned=%v, want %v: %v", tc.name, got, tc.warn, out.Warnings)
		}
	}
}

func TestWarnKindOf(t *testing.T) {
	dec := scored{rec: claim.Record{Type: "decision", Source: "remember"}, shippedOverlap: 1, path: 1}
	if k := warnKindOf(dec); k != -1 {
		t.Fatalf("a decision warned: kind %d", k)
	}
	inherited := scored{rec: claim.Record{Type: "failed", Source: "turn"}, failedOverlap: 1, symbol: 0.5}
	if k := warnKindOf(inherited); k != -1 {
		t.Fatalf("a symbol inherited from the tape vouched for a turn failed: kind %d", k)
	}
	own := inherited
	own.ownSymbol = true
	if k := warnKindOf(own); k != warnFailed {
		t.Fatalf("the ask's own identifier did not vouch: kind %d", k)
	}
	con := scored{rec: claim.Record{Type: "constraint", Source: "turn"}, shippedOverlap: 1}
	if k := warnKindOf(con); k != warnConstraint {
		t.Fatalf("a turn constraint lost its warning: kind %d", k)
	}
}
