package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
)

// Legacy turn-extracted decisions with no referent must stop packing;
// remember-sourced claims are explicit memory and stay regardless.
func TestAskDropsUngroundedTurnDecisionKeepsRemember(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JSTICK", Type: "decision", Source: "turn",
		Text:      "I'll stick with keep.",
		CreatedAt: "2026-08-13T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JREMEM", Type: "decision", Source: "remember",
		Text:      "Keep the daemon loopback only and manual for remote homes.",
		CreatedAt: "2026-08-12T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JARROW", Type: "failed", Source: "turn",
		Text:      "Production code → test exists and failed first.",
		CreatedAt: "2026-08-13T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what did we decide to stick with and keep for production code",
	})
	texts := textsOf(out)
	if strings.Contains(texts, "stick with keep") {
		t.Fatalf("ungrounded turn decision packed: %s", texts)
	}
	if strings.Contains(texts, "Production code") {
		t.Fatalf("arrow chrome packed: %s", texts)
	}
	if !strings.Contains(texts, "loopback") {
		t.Fatalf("remember-sourced decision must survive: %s", texts)
	}
}

// Explicit memory keeps packing whatever its shape: a remembered record
// with an arrow, and an imported prose decision.
func TestAskKeepsExplicitMemoryShapes(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JQARROW", Type: "decision", Source: "remember",
		Text:      "Rename queue → jobs table in the next migration.",
		CreatedAt: "2026-08-13T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JQIMP", Type: "decision", Source: "import",
		Text:      "Decided to keep the current rollout order for now.",
		CreatedAt: "2026-08-12T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project:  "acme/api",
		Question: "what did we decide about the jobs table rename and rollout order",
	})
	texts := textsOf(out)
	if !strings.Contains(texts, "jobs table") {
		t.Fatalf("remembered arrow decision dropped: %s", texts)
	}
	if !strings.Contains(texts, "rollout order") {
		t.Fatalf("imported prose decision dropped: %s", texts)
	}
}
