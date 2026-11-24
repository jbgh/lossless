package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func hitIDs(out Response) string {
	var ids []string
	for _, h := range out.Context {
		ids = append(ids, h.ID)
	}
	return strings.Join(ids, " ")
}

// The action tape adds an earlier ask's words to the lookup when the two
// asks share a file. Those words find candidates; they must not rank
// them. Without that, a record about the earlier goal outranks the
// records about this one, and an ask is answered differently in a busy
// session than in a new one.
func TestV2RanksOnTheAsksOwnWordsNotTheTape(t *testing.T) {
	st := tmpStore(t)
	path := []string{"billing/webhook.go"}
	writeRec(t, st, claim.Record{
		ID: "OLDGOAL", Type: "decision", Source: "remember", Paths: path, CreatedAt: "2026-07-01T00:00:00Z",
		Text: "Checkout retries the payment webhook three times on a timeout before giving up on the export.",
	})
	writeRec(t, st, claim.Record{
		ID: "NEWGOAL1", Type: "decision", Source: "remember", Paths: path, CreatedAt: "2026-07-02T00:00:00Z",
		Text: "The invoice export writes one CSV row per line item with the columns in a fixed order.",
	})
	writeRec(t, st, claim.Record{
		ID: "NEWGOAL2", Type: "failed", Source: "remember", Paths: path, CreatedAt: "2026-07-03T00:00:00Z",
		Text: "Reordering the invoice CSV columns broke the accounting import.",
	})
	second := Request{Project: "acme/api", Goal: "change the invoice export CSV columns", Paths: path}

	fresh := second
	fresh.SessionID = "fresh"
	want := hitIDs(askAt(t, st, fresh))
	if !strings.HasPrefix(want, "NEWGOAL") {
		t.Fatalf("a new session should rank this goal's records first: %s", want)
	}

	askAt(t, st, Request{Project: "acme/api", SessionID: "busy", Goal: "retry the payment webhook on a timeout before giving up", Paths: path})
	busy := second
	busy.SessionID = "busy"
	if got := hitIDs(askAt(t, st, busy)); got != want {
		t.Fatalf("the same ask ranked differently after an earlier ask on the file:\n new session: %s\n busy session: %s", want, got)
	}
}

// A record the session just wrote matches its own text better than
// anything matches the goal. It must not set the bar the other records
// are cut against: the standing rule the goal touches still packs and
// still warns.
func TestV2RememberedRecordDoesNotCutTheRule(t *testing.T) {
	st := tmpStore(t)
	path := []string{"deploy/release.sh"}
	writeRec(t, st, claim.Record{
		// No file of its own: only its words can carry it past the cut.
		ID: "RULE", Type: "constraint", Source: "remember", CreatedAt: "2026-07-01T00:00:00Z",
		Text: "Never tag a release before the staging smoke test has passed.",
	})
	long := "The changelog generator groups commits by scope and writes the release notes file, skipping merge commits, " +
		"reverts, dependency bumps, formatting-only commits, generated files, vendored code, lockfile churn and anything " +
		"under the docs folder, then sorts the groups by the number of commits and prints the authors last."
	writeRec(t, st, claim.Record{
		ID: "JUSTWROTE", Type: "decision", Source: "remember", Paths: path, CreatedAt: "2026-08-14T11:00:00Z", Text: long,
	})
	req := Request{Project: "acme/api", SessionID: "s1", Goal: "tag the release", Paths: path}
	askAt(t, st, req)
	if err := st.AppendActions([]store.Action{{
		ProjectKey: "acme/api", SessionID: "s1", Kind: store.ActionRemember, ClaimID: "JUSTWROTE",
		Paths: path, At: "2026-08-14T11:59:00Z",
	}}); err != nil {
		t.Fatal(err)
	}
	out := askAt(t, st, req)
	if !strings.Contains(hitIDs(out), "RULE") {
		t.Fatalf("the rule the goal touches was left out: %s", hitIDs(out))
	}
	if len(out.Warnings) == 0 || !strings.Contains(strings.Join(out.Warnings, " "), "RULE") {
		t.Fatalf("the rule should still warn: %v", out.Warnings)
	}
}
