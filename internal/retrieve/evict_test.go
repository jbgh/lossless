package retrieve

import (
	"testing"

	"lossless/internal/claim"
)

// A decision that no longer warns still holds its slot against the
// constraint rescue: on a replay of real asks, letting the rescue evict it
// traded a remembered decision about the task for a looser rule.
func TestConstraintRescueKeepsStrongDecisions(t *testing.T) {
	var packed []scored
	for _, id := range []string{"D1", "D2", "D3", "D4", "D5"} {
		packed = append(packed, scored{rec: claim.Record{ID: id, Type: "decision", Source: "remember"}, shippedOverlap: 1, score: 1})
	}
	con := scored{rec: claim.Record{ID: "C1", Type: "constraint", Source: "remember"}, shippedOverlap: 1, score: 5}
	got := evictConstraint(packed, append(append([]scored{}, packed...), con), 100000, 0)
	for _, p := range got {
		if p.rec.ID == "C1" {
			t.Fatalf("the rescue evicted a strong decision: %v", got)
		}
	}
}
