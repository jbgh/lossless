package main

import (
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// A stale constraint is retired by an explicit supersede: status flips to
// superseded, the row and its text survive (delete never), and the reason is
// kept as a linked decision record rather than a new records column.
func TestSupersedeRetiresAndKeepsRow(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "")
	t.Setenv("LOSSLESS_SIDECAR", "off")
	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(mustClaim(t, st, "2026091304482500000000000000aa", "constraint",
		"Since 2026-09-13T04:29:03Z api-prod's Tailscale node key is EXPIRED; do not merge until re-auth.")); err != nil {
		t.Fatal(err)
	}

	if runSupersede([]string{"--home", home, "-bogus"}) != 2 {
		t.Fatal("parse")
	}
	if runSupersede([]string{"--home", home}) != 2 {
		t.Fatal("missing id must exit 2")
	}
	if runSupersede([]string{"--home", home, "nosuchid1234"}) != 1 {
		t.Fatal("unknown id must exit 1")
	}
	if code := runSupersede([]string{"--home", home, "2026091304482500000000000000aa",
		"prod", "key", "renewed", "2026-09-14"}); code != 0 {
		t.Fatalf("supersede exit %d", code)
	}

	rec, ok := st.Get("2026091304482500000000000000aa")
	if !ok {
		t.Fatal("row must survive (delete never)")
	}
	if rec.Status != "superseded" {
		t.Fatalf("status = %q", rec.Status)
	}
	if !strings.Contains(rec.Text, "Tailscale node key is EXPIRED") {
		t.Fatalf("text must survive: %q", rec.Text)
	}

	// The reason is a decision record whose text names the retired id and
	// whose Supersedes field points at it, so the pair is greppable and
	// machine-linked without a schema change.
	active, err := st.ListAllActive()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range active {
		if r.Type == "decision" && strings.Contains(r.Text, "2026091304482500000000000000aa") {
			found = true
			if r.Supersedes != "2026091304482500000000000000aa" {
				t.Fatalf("linked reason must point at the retired id, got %q", r.Supersedes)
			}
			if !strings.Contains(r.Text, "renewed") {
				t.Fatalf("reason text must carry the reason: %q", r.Text)
			}
		}
	}
	if !found {
		t.Fatal("no linked decision record for the reason")
	}
	_ = st.Close()
}

// Without a reason there is no decision record: supersede is still a
// pure status flip that keeps the row.
func TestSupersedeNoReasonWritesNothing(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "")
	t.Setenv("LOSSLESS_SIDECAR", "off")
	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(mustClaim(t, st, "PLAINID1", "constraint", "Never force-push main.")); err != nil {
		t.Fatal(err)
	}
	if code := runSupersede([]string{"--home", home, "PLAINID1"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	active, err := st.ListAllActive()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range active {
		if r.Type == "decision" {
			t.Fatalf("no reason means no decision record, got %q", r.Text)
		}
	}
	if rec, ok := st.Get("PLAINID1"); !ok || rec.Status != "superseded" {
		t.Fatalf("row must survive and be superseded: %+v ok=%v", rec, ok)
	}
	_ = st.Close()
}

func mustClaim(t *testing.T, st *store.Store, id, typ, text string) claim.Record {
	t.Helper()
	return claim.Record{
		ID: id, Type: typ, ProjectKey: "acme/api", Text: text,
		CreatedAt: "2026-09-13T04:48:25Z", SessionID: "sess1", Status: "active",
		Source: "import", Harness: "grok", Paths: []string{"deploy/TAILSCALE_ACCESS.md"},
	}
}

// A reason that looks like a secret is refused before anything changes,
// and a second supersede of the same record is a no-op.
func TestSupersedeRefusesSecretReasonAndRepeats(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "")
	t.Setenv("LOSSLESS_SIDECAR", "off")
	home := t.TempDir()
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	const id = "2026091304482500000000000000ab"
	if _, err := st.WriteClaim(mustClaim(t, st, id, "constraint",
		"Since 2026-09-13 the api-prod deploy key is EXPIRED; do not merge.")); err != nil {
		t.Fatal(err)
	}
	if code := runSupersede([]string{"--home", home, id, "rotated", "to", "AKIAIOSFODNN7EXAMPLE"}); code != 1 {
		t.Fatalf("secret reason exit %d, want 1", code)
	}
	if rec, _ := st.Get(id); rec.Status != "active" {
		t.Fatalf("refused reason still retired the record: %q", rec.Status)
	}
	if code := runSupersede([]string{"--home", home, id, "key", "renewed"}); code != 0 {
		t.Fatalf("supersede exit %d", code)
	}
	if code := runSupersede([]string{"--home", home, id, "key", "renewed"}); code != 0 {
		t.Fatalf("repeat supersede exit %d", code)
	}
	active, err := st.ListAllActive()
	if err != nil {
		t.Fatal(err)
	}
	reasons := 0
	for _, r := range active {
		if r.Type == "decision" && r.Supersedes == id {
			reasons++
		}
	}
	if reasons != 1 {
		t.Fatalf("reason records linked to %s: %d, want 1", id, reasons)
	}
}
