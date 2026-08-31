package gate

import "testing"

// Instruction chrome and diagrams carry unicode arrows and box drawing.
// Real claims about renames use words or ASCII; anchored sentences are
// the caller's job to spare.
func TestArrowChrome(t *testing.T) {
	yes := []string{
		"Production code → test exists and failed first",
		"turns ─── compact ─── thinner window",
		"├── internal/gate",
		"P_fail ⇒ failed_overlap",
		"flow ← cache",
		"retries ↔ backoff table",
		"╭─ panel ─╮",
	}
	for _, s := range yes {
		if !ArrowChrome(s) {
			t.Errorf("ArrowChrome(%q) = false, want true", s)
		}
	}
	no := []string{
		"Renamed the handler and the test failed to compile.",
		"a -> b rename broke the import",
		"Redis token bucket failed in staging.",
	}
	for _, s := range no {
		if ArrowChrome(s) {
			t.Errorf("ArrowChrome(%q) = true, want false", s)
		}
	}
}

// FixtureTalk still blocks lossless self-talk; only sentences naming a
// real fixture artifact (dotted file with a 2+ char extension, deep
// path, or tick) are spared.
func TestFixtureTalkSelfTalkStillBlocked(t *testing.T) {
	yes := []string{
		"Don't re-ingest the fixture sessions, e.g. after compact",
		"The fixture logic handles read/write races",
		"The fixture data quoted the ask packet",
	}
	for _, s := range yes {
		if !FixtureTalk(s) {
			t.Errorf("FixtureTalk(%q) = false, want true", s)
		}
	}
	no := []string{
		"Cypress fixture products.json failed to load in cypress/e2e/cart.cy.ts after the price schema change.",
		"Updated the fixture in `cart.cy.ts`",
	}
	for _, s := range no {
		if FixtureTalk(s) {
			t.Errorf("FixtureTalk(%q) = true, want false", s)
		}
	}
}
