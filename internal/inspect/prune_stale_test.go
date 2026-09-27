package inspect

import (
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
)

// staleConstraintText is the live repro's shape: a start anchor plus incident
// vocabulary, so `inspect --prune` should list it as a candidate. The incident
// date is deliberately old, because Prune derives "now" from the clock and a
// fixture anchored at the repro's real date would not be stale yet today.
const staleConstraintText = "Since 2020-01-05T00:00:00Z api-prod's Tailscale node key is EXPIRED; do not merge until re-auth."

// The prune stale listing is a prompt, not a sweep: a decayed, still-active
// constraint is named with its reason and anchor, and nothing is superseded or
// deleted by listing it.
func TestPruneListsStaleWithoutSuperseding(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Seed a decayed incident constraint. Prune derives "now" from the clock,
	// so the created_at is far enough in the past to be stale under any window.
	if _, err := st.WriteClaim(claim.Record{
		ID: "2026091304482500000000000000aa", Type: "constraint", ProjectKey: "acme/ops",
		Text: staleConstraintText, CreatedAt: "2020-01-01T00:00:00Z", SessionID: "s",
		Status: "active", Source: "import", Harness: "grok",
		Paths: []string{"deploy/TAILSCALE_ACCESS.md"},
	}); err != nil {
		t.Fatal(err)
	}

	res, err := Prune(st, "acme/ops")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.StaleConstraints) != 1 || res.StaleConstraints[0].ID != "2026091304482500000000000000aa" {
		t.Fatalf("stale candidates: %+v", res.StaleConstraints)
	}
	// Listed, not retired.
	if rec, ok := st.Get("2026091304482500000000000000aa"); !ok || rec.Status != "active" {
		t.Fatalf("prune listing must not supersede: %+v ok=%v", rec, ok)
	}
	// With no --project, prune covers every project; the listing used to
	// pass the empty key through and list nothing.
	all, err := Prune(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.StaleConstraints) != 1 || all.StaleConstraints[0].Project != "acme/ops" {
		t.Fatalf("all-project stale candidates: %+v", all.StaleConstraints)
	}
}

func TestStaleCandidatesExcludesReconfirmedAndAnchorsFree(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// A decayed incident constraint with no newer match: listed.
	if _, err := st.WriteClaim(claim.Record{
		ID: "STALE1", Type: "constraint", ProjectKey: "acme/api",
		Text:      "Since 2026-01-05T00:00:00Z the prod queue is down; block merges.",
		CreatedAt: "2026-01-05T00:00:00Z", SessionID: "s", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	// Standing rule with a "since" anchor and no incident vocabulary: never decays.
	if _, err := st.WriteClaim(claim.Record{
		ID: "RULE1", Type: "constraint", ProjectKey: "acme/api",
		Text:      "Since 2026-01, CI requires Go 1.23.",
		CreatedAt: "2026-01-05T00:00:00Z", SessionID: "s", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	// A decayed incident re-stated by a newer active constraint sharing two
	// rare identifiers (woodpecker-cd, deploy-backend): not listed, the trap
	// is live again.
	if _, err := st.WriteClaim(claim.Record{
		ID: "CONFIRMED1", Type: "constraint", ProjectKey: "acme/api",
		Text:      "Since 2026-01-10T00:00:00Z the woodpecker-cd runner is down; deploy-backend merges are blocked.",
		CreatedAt: "2026-01-10T00:00:00Z", SessionID: "s", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "NEWER1", Type: "constraint", ProjectKey: "acme/api",
		Text:      "Since 2026-02-20T00:00:00Z the woodpecker-cd runner is down again; deploy-backend still blocked.",
		CreatedAt: "2026-02-20T00:00:00Z", SessionID: "s", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := retrieve.StaleCandidates(st, "acme/api", now, retrieve.DefaultStaleWindow)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	// STALE1 decays with nothing newer. CONFIRMED1 decays but is re-confirmed
	// by NEWER1. RULE1 is a standing rule. NEWER1 is inside its own window.
	if len(ids) != 1 || ids[0] != "STALE1" {
		t.Fatalf("expected only STALE1, got %v", ids)
	}
	if !strings.Contains(got[0].Anchor, "2026-01") {
		t.Fatalf("candidate must carry the visible anchor: %+v", got[0])
	}
}
