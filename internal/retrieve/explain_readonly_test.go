package retrieve

import (
	"testing"
	"time"
)

// Explain powers `inspect --ask`, an operator view. Observing must not
// perturb the tape: no ask/warn rows, or the next real ask inherits the
// inspected pack as "last pack" and its tokens on continue.
func TestExplainDoesNotWriteActionTape(t *testing.T) {
	st := seed(t)
	e := Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	}}
	tr, err := e.Explain(Request{
		Project:   "acme/api",
		Question:  "what do we know about rate limiting on auth?",
		Paths:     []string{"src/middleware/auth.ts"},
		SessionID: "inspect-probe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Packed) == 0 {
		t.Fatal("expected a non-empty trace")
	}
	acts, err := st.RecentActions("acme/api", "inspect-probe", 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 0 {
		t.Fatalf("Explain wrote %d action rows; want 0 (first: %+v)", len(acts), acts[0])
	}
}
