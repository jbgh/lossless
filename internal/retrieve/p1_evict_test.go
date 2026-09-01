package retrieve

import (
	"testing"

	"lossless/internal/claim"
)

// evictFailed must not re-add a job-1 failed the packer skipped for
// diversity, and must not mutate the caller's slice (Explain traces it).
func TestEvictFailedRespectsDiversityAndDoesNotAlias(t *testing.T) {
	mk := func(id, typ, text string, fail float64) scored {
		return scored{rec: claim.Record{ID: id, Type: typ, Text: text, Paths: []string{"src/limiter.ts"}},
			failedOverlap: fail, path: 1, score: 3}
	}
	f1 := mk("F1", "failed", "Token bucket refill failed under burst load in src/limiter.ts after the cap change.", 1)
	f2 := mk("F2", "failed", "Token bucket refill failed under burst load in src/limiter.ts after the cap changes.", 1)
	d1 := mk("D1", "decision", "Limiter lives in src/limiter.ts as an in-process token bucket.", 0)
	packed := []scored{f1, d1}
	before := append([]scored(nil), packed...)
	out := evictFailed(packed, []scored{f1, f2, d1}, DefaultLimit)
	for _, c := range out {
		if c.rec.ID == "F2" {
			t.Fatalf("near-duplicate failed re-added past the diversity gate: %v", scoredIDs(out))
		}
	}
	for i := range before {
		if packed[i].rec.ID != before[i].rec.ID {
			t.Fatalf("caller slice mutated: %v -> %v", scoredIDs(before), scoredIDs(packed))
		}
	}
}

func scoredIDs(cs []scored) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.rec.ID)
	}
	return out
}
