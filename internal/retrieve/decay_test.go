package retrieve

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// TailscaleExpired is the live repro, record 2026091304482500000000000000aa
// (acme/ops, active, type constraint), verbatim. It carries a start
// anchor ("Since 2026-09-13T04:29:03Z") plus incident vocabulary, and — the
// reason the start anchor has to govern — a subordinate end anchor about a
// different box: "(eu expires 2026-10-06T22:28:49Z)".
const TailscaleExpired = "Since 2026-09-13T04:29:03Z api-prod's Tailscale node key is EXPIRED (ticket #3837): every main deploy-backend dies at ssh-keyscan, so #3822 and #3824 are merged but NOT deployed (prod runs 262f96189c through #3823); merges of reviewed PRs #3828 #3825 #3830 #3834 are HELD until the owner re-authenticates Tailscale on prod (provider console) and disables key expiry on api-prod and api-eu (eu expires 2026-10-06T22:28:49Z). Do not restart pipelines or merge until a deploy-backend is green again."

// TailscaleState is the state record that closed the incident on 2026-09-14,
// 2026091417444400000000000000bb. It has no paths, which is why nothing at
// read time retires the constraint it closes.
const TailscaleState = "2026-09-14 17:5x UTC: #3837 CLOSED — prod Tailscale key renewed, key expiry disabled on api-prod + api-eu, Tailscale SSH OFF on prod (RunSSH false; CI deploys use host sshd + woodpecker-cd key), both boxes rebooted onto new kernels, main 4591 green, prod on 75da6da."

func recAt(id, typ, text, at string) claim.Record {
	return claim.Record{
		ID: id, Type: typ, ProjectKey: "acme/ops", Text: text,
		CreatedAt: at, SessionID: "sess", Status: "active", Source: "import",
		Harness: "grok", Paths: []string{"deploy/TAILSCALE_ACCESS.md"},
	}
}

func TestConstraintDecay(t *testing.T) {
	created := "2026-09-13T04:48:25Z"
	inside := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC) // 13d old
	outside := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		text     string
		now      time.Time
		window   time.Duration
		want     bool
		reason   string
		contains string
	}{
		{
			name: "tailscale repro decays at the default window", text: TailscaleExpired,
			now: outside, window: DefaultStaleWindow, want: true, reason: ReasonIncidentWindow,
			contains: "2026-09-13",
		},
		{
			name: "tailscale repro still warns inside the window", text: TailscaleExpired,
			now: inside, window: DefaultStaleWindow,
		},
		{
			name: "start anchor on a standing rule never decays", text: "Since 2026-01, CI requires Go 1.23.",
			now: outside, window: DefaultStaleWindow,
		},
		{
			name: "as of on a standing rule never decays", text: "As of 2025-06 the API is stable and the schema is frozen.",
			now: outside, window: DefaultStaleWindow,
		},
		{
			name: "no anchors are unaffected", text: "Never log Authorization headers in src/middleware/auth.ts.",
			now: outside, window: DefaultStaleWindow,
		},
		{
			name: "end anchor past its date stops warning", text: "Migrate off Python 3.8 until 2026-03-01.",
			now: outside, window: DefaultStaleWindow, want: true, reason: ReasonEndAnchor, contains: "2026-03-01",
		},
		{
			name: "until date in the future still warns", text: "Migrate off Python 3.8 until 2027-03-01.",
			now: outside, window: DefaultStaleWindow,
		},
		{
			name: "expires in the past stops warning", text: "The api-prod deploy token expires 2026-09-20.",
			now: outside, window: DefaultStaleWindow, want: true, reason: ReasonEndAnchor,
		},
		{
			name: "incident vocabulary alone without an anchor does not decay",
			text: "The prod ingress is down; page the on-call.",
			now:  outside, window: DefaultStaleWindow,
		},
		{
			name: "a longer window keeps the tailscale repro warning", text: TailscaleExpired,
			now: outside, window: 90 * 24 * time.Hour,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ConstraintDecay(recAt("X", "constraint", c.text, created), c.now, c.window)
			if got.Decayed != c.want {
				t.Fatalf("decayed=%v want %v (reason %q)", got.Decayed, c.want, got.Reason)
			}
			if c.want && got.Reason != c.reason {
				t.Fatalf("reason=%q want %q", got.Reason, c.reason)
			}
			if c.contains != "" && !strings.Contains(got.Anchor, c.contains) {
				t.Fatalf("anchor %q does not show %q", got.Anchor, c.contains)
			}
		})
	}
}

// A record filed long after the incident it describes must still warn for a
// full window: decay is measured from the later of the anchor and created_at,
// never from a back-dated anchor alone.
func TestConstraintDecayBackdatedImport(t *testing.T) {
	rec := recAt("X", "constraint", TailscaleExpired, "2026-09-26T00:00:00Z")
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	if got := ConstraintDecay(rec, now, DefaultStaleWindow); got.Decayed {
		t.Fatalf("freshly filed back-dated incident decayed: %+v", got)
	}
	later := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	if got := ConstraintDecay(rec, later, DefaultStaleWindow); !got.Decayed {
		t.Fatal("never decayed")
	}
}

func TestDecaySuppressesTailscaleWarningButStillPacks(t *testing.T) {
	st := tmpStore(t)
	// The constraint and the state record that closed the incident, as the
	// live store has them: the state row carries no paths.
	c := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, c)
	s := recAt("2026091417444400000000000000bb", "state", TailscaleState, "2026-09-14T17:44:44Z")
	s.Paths = []string{}
	writeRec(t, st, s)

	ask := Request{Project: "acme/ops", Goal: "re-authenticate Tailscale on prod and re-run the deploy-backend pipeline", Paths: []string{"deploy/TAILSCALE_ACCESS.md"}}

	inside, err := decayAskAt(t, st, ask, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !warnsAbout(inside, c.ID) {
		t.Fatalf("inside the window the standing-constraint warning must stand: %v", inside.Warnings)
	}

	out, err := decayAskAt(t, st, ask, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if warnsAbout(out, c.ID) {
		t.Fatalf("decayed constraint still warns: %v", out.Warnings)
	}
	if !packsWith(out, c.ID) {
		t.Fatalf("a decayed constraint still packs, with its visible date: %+v", out.Context)
	}
	var hit *Hit
	for i := range out.Context {
		if out.Context[i].ID == c.ID {
			hit = &out.Context[i]
		}
	}
	if hit == nil || !strings.Contains(hit.When, "2026-09-13") {
		t.Fatalf("packed hit lost its date: %+v", hit)
	}
}

func TestSupersedeStopsPacking(t *testing.T) {
	st := tmpStore(t)
	c := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, c)
	ask := Request{Project: "acme/ops", Goal: "re-authenticate Tailscale on prod and re-run the deploy-backend pipeline", Paths: []string{"deploy/TAILSCALE_ACCESS.md"}}
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	before, err := decayAskAt(t, st, ask, now)
	if err != nil {
		t.Fatal(err)
	}
	if !warnsAbout(before, c.ID) || !packsWith(before, c.ID) {
		t.Fatalf("precondition: %+v %v", before.Context, before.Warnings)
	}

	if err := st.Supersede(c.ID); err != nil {
		t.Fatal(err)
	}
	after, err := decayAskAt(t, st, ask, now)
	if err != nil {
		t.Fatal(err)
	}
	if warnsAbout(after, c.ID) {
		t.Fatalf("superseded record still warns: %v", after.Warnings)
	}
	if packsWith(after, c.ID) {
		t.Fatalf("superseded record still packs: %+v", after.Context)
	}
	// Delete never: the row and its text survive.
	got, ok := st.Get(c.ID)
	if !ok || got.Status != "superseded" || got.Text != TailscaleExpired {
		t.Fatalf("supersede must keep row and text: %+v ok=%v", got, ok)
	}
}

func TestReconfirmedBySameClaimHash(t *testing.T) {
	st := tmpStore(t)
	old := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, old)
	// Same claim_hash, newer, re-stated verbatim: the trap is live again.
	newer := recAt("2026092700000000000000abcd", "constraint", TailscaleExpired, "2026-09-27T00:00:00Z")
	writeRec(t, st, newer)

	if id, ok := Reconfirmed(st, old, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); !ok || id != newer.ID {
		t.Fatalf("reconfirmation by claim_hash: %q %v", id, ok)
	}
	// The trap keeps warning. A same-claim_hash restatement supersedes the
	// older row in the store (writeClaimOnce), so the surviving warning is
	// the newer record's — the trap is not silently retired.
	ask := Request{Project: "acme/ops", Goal: "re-authenticate Tailscale on prod and re-run the deploy-backend pipeline", Paths: []string{"deploy/TAILSCALE_ACCESS.md"}}
	out, err := decayAskAt(t, st, ask, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !warnsAbout(out, newer.ID) {
		t.Fatalf("a re-confirmed trap must keep warning: %v", out.Warnings)
	}
}

func TestReconfirmedBySameRareIdentifier(t *testing.T) {
	st := tmpStore(t)
	old := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, old)
	// Different wording, different claim_hash, same rare identifiers
	// (deploy-backend, ssh-keyscan) — the same signal recurrence uses.
	newer := recAt("2026092700000000000000abcd", "constraint",
		"api-prod deploy-backend is down again: every ssh-keyscan on main fails at the Tailscale hop.", "2026-09-27T00:00:00Z")
	writeRec(t, st, newer)
	if claim.Hash("acme/ops", "constraint", newer.Text) == old.ClaimHash {
		t.Fatal("fixture must not share a claim_hash; that is the other test")
	}
	if id, ok := Reconfirmed(st, old, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); !ok || id != newer.ID {
		t.Fatalf("reconfirmation by rare identifier set: %q %v", id, ok)
	}
}

func TestNotReconfirmed(t *testing.T) {
	st := tmpStore(t)
	old := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, old)
	// Older, not newer: no re-confirmation.
	older := recAt("2026090100000000000000aaaa", "constraint", TailscaleExpired, "2026-09-01T00:00:00Z")
	writeRec(t, st, older)
	// Newer, unrelated identifiers: no re-confirmation.
	other := recAt("2026092700000000000000abcd", "constraint",
		"Since 2026-09-27 the woodpecker-cd runner is down; the queue is blocked.", "2026-09-27T00:00:00Z")
	writeRec(t, st, other)
	// Newer, same ISO date fragment only: a date is not an identifier.
	sameDay := recAt("2026092700000000000000beef", "constraint",
		"As of 2026-09-13 the invoice exporter refuses the new tax id.", "2026-09-27T00:00:00Z")
	writeRec(t, st, sameDay)

	if id, ok := Reconfirmed(st, old, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); ok {
		t.Fatalf("re-confirmed by %q without a deterministic match", id)
	}
}

func TestStaleCandidatesListsDecayedNotSuperseded(t *testing.T) {
	st := tmpStore(t)
	stale := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, stale)
	live := recAt("LIVEJOSE", "constraint", "Never log Authorization headers in src/middleware/auth.ts.", "2026-09-01T00:00:00Z")
	writeRec(t, st, live)
	other := recAt("OTHERJOSE", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	other.ProjectKey = "acme/api"
	writeRec(t, st, other)

	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	got, err := StaleCandidates(st, "acme/ops", now, DefaultStaleWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != stale.ID {
		t.Fatalf("stale candidates: %+v", got)
	}
	if got[0].Reason != ReasonIncidentWindow || !strings.Contains(got[0].Anchor, "2026-09-13") {
		t.Fatalf("candidate must carry the decay rule and the visible anchor: %+v", got[0])
	}

	// Supersede never deletes; a superseded record simply leaves the listing.
	if err := st.Supersede(stale.ID); err != nil {
		t.Fatal(err)
	}
	got, err = StaleCandidates(st, "acme/ops", now, DefaultStaleWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("superseded record still listed: %+v", got)
	}
}

func TestStaleWindowDefaultAndEnv(t *testing.T) {
	if DefaultStaleWindow != 14*24*time.Hour {
		t.Fatalf("default window %v", DefaultStaleWindow)
	}
	if got := StaleWindow(); got != DefaultStaleWindow {
		t.Fatalf("unqualified env must not move the default: %v", got)
	}
	t.Setenv("LOSSLESS_STALE_WINDOW", "72h")
	if got := StaleWindow(); got != 3*24*time.Hour {
		t.Fatalf("env override: %v", got)
	}
	t.Setenv("LOSSLESS_STALE_WINDOW", "not-a-duration")
	if got := StaleWindow(); got != DefaultStaleWindow {
		t.Fatalf("garbage env must fall back: %v", got)
	}
	t.Setenv("LOSSLESS_STALE_WINDOW", "0s")
	if got := StaleWindow(); got != DefaultStaleWindow {
		t.Fatalf("non-positive window must fall back: %v", got)
	}
}

func TestEngineStaleWindowOverride(t *testing.T) {
	st := tmpStore(t)
	c := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, c)
	ask := Request{Project: "acme/ops", Goal: "re-authenticate Tailscale on prod and re-run the deploy-backend pipeline", Paths: []string{"deploy/TAILSCALE_ACCESS.md"}}
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	e := Engine{Store: st, Now: func() time.Time { return now }, StaleWindow: 90 * 24 * time.Hour}
	out, err := e.Ask(ask)
	if err != nil {
		t.Fatal(err)
	}
	if !warnsAbout(out, c.ID) {
		t.Fatalf("a widened window must keep the warning: %v", out.Warnings)
	}
}

func decayAskAt(t *testing.T, st *store.Store, req Request, now time.Time) (Response, error) {
	t.Helper()
	return Engine{Store: st, Now: func() time.Time { return now }}.Ask(req)
}

// packsWith reports whether a record made the pack. warnsAbout (the warning
// half) already lives in p1_continuity_test.go.
func packsWith(r Response, id string) bool {
	for _, h := range r.Context {
		if h.ID == id {
			return true
		}
	}
	return false
}

// A decayed constraint cannot be the constraint a recurrence cluster
// needs: the recurrence route used to run after decay and cite the same
// record under its own heading, bringing back the warning decay stopped.
func TestRecurrenceIgnoresDecayedConstraint(t *testing.T) {
	now := time.Date(2026, 9, 21, 21, 0, 0, 0, time.UTC)
	run := func(constraintAt string) Response {
		st := tmpStore(t)
		project := "acme/ops"
		for i, at := range []string{"2026-08-02T10:00:00Z", "2026-08-05T10:00:00Z"} {
			writeRecProject(t, st, project, claim.Record{
				ID: "20260800000000000000000000f" + string(rune('0'+i)), Type: "failed", Source: "turn",
				SessionID: "s" + string(rune('0'+i)), CreatedAt: at,
				Text: fmt.Sprintf("run %d: the deploy-backend job died at ssh-keyscan.", i),
			})
		}
		writeRecProject(t, st, project, claim.Record{
			ID: "20260800000000000000000000c0", Type: "constraint", Source: "turn",
			SessionID: "s9", CreatedAt: constraintAt,
			Text: "Since " + constraintAt[:10] + " the node key is EXPIRED: every deploy-backend run is blocked; do not merge.",
		})
		out, err := decayAskAt(t, st, Request{Project: project, Goal: "tidy the README wording"}, now)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	recurring := func(r Response) bool {
		for _, w := range r.Warnings {
			if strings.Contains(w, "Recurring failure") {
				return true
			}
		}
		return false
	}
	if !recurring(run("2026-09-15T10:00:00Z")) {
		t.Fatal("control: a live constraint's cluster must warn")
	}
	if out := run("2026-08-01T10:00:00Z"); recurring(out) {
		t.Fatalf("a decayed constraint anchored a recurrence warning: %v", out.Warnings)
	}
}

// With several end dates, the latest decides: the warning still has
// something to say until every termination it names has passed.
func TestEndAnchorLatestDecides(t *testing.T) {
	rec := recAt("x", "constraint",
		"The staging deploy key expires 2026-09-20 and the prod deploy key expires 2027-01-05; rotate both.", "2026-09-01T00:00:00Z")
	if d := ConstraintDecay(rec, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); d.Decayed {
		t.Fatalf("decayed on the first of two end dates: %+v", d)
	}
	if d := ConstraintDecay(rec, time.Date(2027, 1, 7, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); !d.Decayed || d.Reason != ReasonEndAnchor {
		t.Fatalf("still warning after every end date passed: %+v", d)
	}
}

// One shared identifier is not a re-confirmation: a later rule that only
// names the same host must not revive an unrelated incident.
func TestNotReconfirmedByOneSharedIdentifier(t *testing.T) {
	st := tmpStore(t)
	old := recAt("2026091304482500000000000000aa", "constraint", TailscaleExpired, "2026-09-13T04:48:25Z")
	writeRec(t, st, old)
	newer := recAt("2026092700000000000000abcd", "constraint",
		"Never restart the ssh-keyscan step by hand during business hours.", "2026-09-27T00:00:00Z")
	writeRec(t, st, newer)
	if id, ok := Reconfirmed(st, old, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); ok {
		t.Fatalf("re-confirmed by %q on one shared identifier", id)
	}
}

// A decayed constraint must not stand in for a live one: not as the
// cluster's first row (ref starts at rs[0]), and not as "already warned"
// when it packs, since its warning decayed and said nothing.
func TestRecurrenceLiveConstraintBesideDecayedOne(t *testing.T) {
	now := time.Date(2026, 9, 21, 21, 0, 0, 0, time.UTC)
	st := tmpStore(t)
	project := "acme/ops"
	// A decayed incident constraint, newer than the rule. The cluster's
	// anchor must not depend on the order the store returns rows in.
	writeRecProject(t, st, project, claim.Record{
		ID: "20260800000000000000000000c0", Type: "constraint", Source: "turn",
		SessionID: "s9", CreatedAt: "2026-08-20T10:00:00Z",
		Text: "Since 2026-08-20 the node key is EXPIRED: every deploy-backend run is blocked; do not merge.",
	})
	for i, at := range []string{"2026-08-02T10:00:00Z", "2026-08-05T10:00:00Z"} {
		writeRecProject(t, st, project, claim.Record{
			ID: fmt.Sprintf("20260800000000000000000000f%d", i), Type: "failed", Source: "turn",
			SessionID: fmt.Sprintf("s%d", i), CreatedAt: at,
			Text: fmt.Sprintf("run %d: the deploy-backend job died at ssh-keyscan.", i),
		})
	}
	// A live standing rule, older than the decayed incident above.
	writeRecProject(t, st, project, claim.Record{
		ID: "20260800000000000000000000c1", Type: "constraint", Source: "turn",
		SessionID: "s8", CreatedAt: "2026-08-03T10:00:00Z",
		Text: "Never run deploy-backend by hand; the pipeline owns it.",
	})
	clusters := recurrenceClusters(st, project, now, newDecayMemo(st, now, DefaultStaleWindow))
	found := false
	for _, c := range clusters {
		if c.ident == "deploy-backend" {
			found = true
			if c.ref.ID != "20260800000000000000000000c1" {
				t.Fatalf("cluster anchored on %s, want the live rule", c.ref.ID)
			}
		}
	}
	if !found {
		t.Fatalf("live rule's cluster dropped: %+v", clusters)
	}
}

// A chain of restatements decays link by link: a newer restatement that
// has itself decayed does not revive the oldest one.
func TestNotReconfirmedByDecayedRestatement(t *testing.T) {
	st := tmpStore(t)
	old := recAt("20260901000000000000000000aa", "constraint",
		"Since 2026-09-01 api-prod deploy-backend is down: ssh-keyscan fails; block merges.", "2026-09-01T00:00:00Z")
	writeRec(t, st, old)
	newer := recAt("20260903000000000000000000bb", "constraint",
		"Since 2026-09-03 api-prod deploy-backend still down, ssh-keyscan still failing; merges blocked.", "2026-09-03T00:00:00Z")
	writeRec(t, st, newer)
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	if id, ok := Reconfirmed(st, old, now, DefaultStaleWindow); ok {
		t.Fatalf("revived by decayed restatement %q", id)
	}
	// Inside the newer one's window it still re-confirms.
	if _, ok := Reconfirmed(st, old, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); !ok {
		t.Fatal("live restatement must re-confirm")
	}
}

func TestUntilFutureVetoesStartDecay(t *testing.T) {
	rec := recAt("x", "constraint", "Since 2026-09-13 deploys are blocked; do not merge until 2026-12-01.", "2026-09-13T00:00:00Z")
	if d := ConstraintDecay(rec, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); d.Decayed {
		t.Fatalf("decayed before its own until date: %+v", d)
	}
	if d := ConstraintDecay(rec, time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); !d.Decayed {
		t.Fatalf("still warning after its until date: %+v", d)
	}
}

func TestAnchorClockShapes(t *testing.T) {
	for _, s := range []string{"2026-09-13T04:29", "2026-09-13T04:29Z", "2026-09-13 04:29:03Z", "2026-09-13 04:29", "2026-09-13T04:29:03Z", "2026-09", "2026-09-13t04:29:03z"} {
		if _, ok := parseAnchorDate(s); !ok {
			t.Errorf("anchor %q not parsed", s)
		}
	}
}

// One shared identifier never re-confirms, even when it is the old
// record's only one.
func TestNotReconfirmedBySingleIdentRecord(t *testing.T) {
	st := tmpStore(t)
	old := recAt("20260901000000000000000000cc", "constraint", "Since 2026-09-01 api-prod is down.", "2026-09-01T00:00:00Z")
	writeRec(t, st, old)
	writeRec(t, st, recAt("20260920000000000000000000dd", "constraint", "Never restart api-prod in business hours.", "2026-09-20T00:00:00Z"))
	if id, ok := Reconfirmed(st, old, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), DefaultStaleWindow); ok {
		t.Fatalf("re-confirmed by %q on a single identifier", id)
	}
}

// An end anchor holds through the period it names.
func TestEndAnchorHoldsThroughItsPeriod(t *testing.T) {
	cases := []struct {
		text string
		at   time.Time
		want bool
	}{
		{"Freeze merges until 2026-12.", time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), false},
		{"Freeze merges until 2026-12.", time.Date(2027, 1, 1, 0, 0, 1, 0, time.UTC), true},
		{"Freeze merges until 2026-09-30.", time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC), false},
		{"Freeze merges until 2026-09-30.", time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC), true},
	}
	for _, c := range cases {
		rec := recAt("x", "constraint", c.text, "2026-09-01T00:00:00Z")
		if d := ConstraintDecay(rec, c.at, DefaultStaleWindow); d.Decayed != c.want {
			t.Errorf("%q at %s: decayed=%v want %v", c.text, c.at, d.Decayed, c.want)
		}
	}
}
