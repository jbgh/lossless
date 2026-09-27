package inspect

import (
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func TestPruneSupersedesStatusReports(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := seed("a1", "The failure is the known worktree-only bundled-font test in export.spec.ts.", "failed")
	first.CreatedAt = "2026-09-24T00:00:00Z"
	writes := []claim.Record{
		// the first report of a known failure: kept, nothing before it
		// names the test
		first,
		// a later self-labeled report of the same test: superseded, the
		// first report already captured it
		seed("a2", "Playwright: 26 passed, 1 failed; the known export.spec.ts bundled-font one is the one failure.", "failed"),
		// a tally naming a first-time failing test: kept, no other record mentions it
		seed("a3", "vitest 2 passed/1 failed (auth.spec.ts, the token refresh breaks).", "failed"),
		// a real failure: kept by both prune rules
		seed("a4", "Three tests fail, and the commit went through anyway (the pipe hid the failure).", "failed"),
		// not a failed: untouched
		seed("a5", "CI is pending (not failed) on all open PRs.", "state"),
	}
	for _, r := range writes {
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Prune(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.SupersededStatusReports != 1 {
		t.Fatalf("superseded %d status reports, want 1: %+v", out.SupersededStatusReports, out)
	}
	active := map[string]string{}
	for _, r := range mustListActive(t, st) {
		active[r.Text] = r.Type
	}
	if len(active) != 4 {
		t.Fatalf("active rows = %d, want 4: %v", len(active), active)
	}
	for _, want := range []string{
		"The failure is the known worktree-only bundled-font test in export.spec.ts.",
		"vitest 2 passed/1 failed (auth.spec.ts, the token refresh breaks).",
		"Three tests fail, and the commit went through anyway (the pipe hid the failure).",
		"CI is pending (not failed) on all open PRs.",
	} {
		if _, ok := active[want]; !ok {
			t.Errorf("kept record missing: %q", want)
		}
	}
}

// The sweep supersedes, never deletes: the row and its text survive.
func TestPruneStatusReportsNeverDeletes(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const text = "CI is pending (not failed) on all open PRs."
	id := seed("b1", "CI is pending (not failed) on all open PRs.", "failed")
	if _, err := st.WriteClaim(id); err != nil {
		t.Fatal(err)
	}
	if _, err := Prune(st, ""); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get(id.ID)
	if !ok {
		t.Fatal("superseded record was deleted")
	}
	if got.Text != text {
		t.Fatalf("record text lost: %q", got.Text)
	}
	if got.Status != "superseded" {
		t.Fatalf("status = %q, want superseded", got.Status)
	}
}

// A status report naming a test with no other failure report survives:
// the sweep must not erase a brand-new failure whose only report is
// worded as a status report.
func TestPruneKeepsFirstTimeTally(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const text = "vitest 2 passed/1 failed (auth.spec.ts, the token refresh breaks)."
	if _, err := st.WriteClaim(seed("c1", text, "failed")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(seed("c2", "the failure is the known bundled-font test in export.spec.ts.", "failed")); err != nil {
		t.Fatal(err)
	}
	out, err := Prune(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.SupersededStatusReports != 0 {
		t.Fatalf("first-time report superseded: %+v", out)
	}
	if len(mustListActive(t, st)) != 2 {
		t.Fatal("first-time reports were removed")
	}
}

func seed(id, text, typ string) claim.Record {
	return claim.Record{
		ID: id, Type: typ, ProjectKey: "acme/api", Harness: "grok", SessionID: "s1",
		CreatedAt: "2026-09-25T00:00:00Z", Status: "active", Source: "append",
		Text: text, ClaimHash: claim.Hash("acme/api", typ, text),
	}
}

func mustListActive(t *testing.T, st *store.Store) []claim.Record {
	t.Helper()
	recs, err := st.ListAllActive()
	if err != nil {
		t.Fatal(err)
	}
	return recs
}
