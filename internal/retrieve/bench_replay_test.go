package retrieve

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// Bench: replay the three real incidents from the 2026-09-21 audit-bundle
// postmortem against synthetic stores shaped like the live memora one.

func benchDate(daysAgo int) string {
	return time.Date(2026, 9, 21, 21, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAgo).Format(time.RFC3339)
}

func benchID(prefix string, i int) string {
	return fmt.Sprintf("01JBENCH%s%d", strings.ToUpper(prefix), i)
}

func writeRecProject(t *testing.T, st *store.Store, project string, rec claim.Record) {
	t.Helper()
	rec.ProjectKey = project
	writeRec(t, st, rec)
}

func benchAt(t *testing.T, st *store.Store, req Request) Response {
	t.Helper()
	e := Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 9, 21, 21, 0, 0, 0, time.UTC)
	}}
	out, err := e.Ask(req)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Case A — the audit-bundle incident. Five pathless pr-size-check failed
// notes (turn-extracted, read-time noise by the deliberate statusFailed
// gate) plus one remember-source standing constraint. The ask carries
// harness-guessed paths that match nothing. The recurrence warning must
// fire (store-level scan, zero vocabulary dependence) and the constraint
// must reach the pack via evictConstraint. The status-snapshot failed
// must stay silent.
func TestBenchAuditBundleRecurrence(t *testing.T) {
	st := tmpStore(t)
	project := "acme/forge"
	faileds := []string{
		"**pr-size-check** failed at **952 lines** — added `audit-bundle` (coherent cross-platform foundation + tests)",
		"Only failure is the size gate (518 > 500) — this is a cohesive reliability bundle under `pr-size-check`",
		"Pre-push failed only on size (738 lines); `audit-bundle` is on the PR, pr-size-check needs a restart.",
		"Pre-push failed only on size (793 lines); `audit-bundle` is on the PR, waiting on pr-size-check restart.",
		"PR has 1063 changed lines (>500) — pr-size-check error; add the `audit-bundle` label and restart.",
	}
	days := []int{35, 28, 26, 25, 0}
	for i, text := range faileds {
		writeRecProject(t, st, project, claim.Record{
			ID:        benchID("F", i),
			Type:      "failed",
			Source:    "turn",
			Text:      text,
			CreatedAt: benchDate(days[i]),
		})
	}
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("C", 1),
		Type:      "constraint",
		Source:    "remember",
		Text:      "acme/forge CI standing rule: a PR whose diff exceeds 500 changed lines (after the locale and lockfile excludes) FAILS the Woodpecker `pr-size-check` step unless the PR carries the `audit-bundle` label. Flow for agit PRs: open the PR first, then add the label via the Gitea issues add-labels API (POST, not PATCH), then restart the pipeline — the label never lands before the push.",
		CreatedAt: benchDate(0),
		Paths:     []string{".woodpecker/ci.yml", ".gitea/PULL_REQUEST_TEMPLATE.md"},
	})
	// Distractors: the records that won the pack race in the live miss.
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("D", 1),
		Type:      "decision",
		Source:    "turn",
		Text:      "Wave-12 monitoring stack: backend-metrics-alert.sh drives the systemd onfailure units; keep the threshold alerts primitive.",
		CreatedAt: benchDate(1),
		Paths:     []string{"deploy/scripts/backend-metrics-alert.sh"},
	})
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("S", 1),
		Type:      "state",
		Source:    "turn",
		Text:      "Deploy wave-12 landed on the Helsinki host; monitoring verified.",
		CreatedAt: benchDate(2),
		Paths:     []string{"deploy/scripts/lib-glitchtip.sh"},
	})
	out := benchAt(t, st, Request{
		Project:  project,
		Question: "review the open tickets and the wave status",
		Goal:     "read the brief and execute the stream inside the worktree",
		Paths:    []string{"deploy/scripts", "scripts/test"},
	})
	warned := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "pr-size-check") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("recurrence warning for pr-size-check missing: %v", out.Warnings)
	}
	if !strings.Contains(textsOf(out), "audit-bundle") {
		t.Fatalf("standing constraint not packed: %s", textsOf(out))
	}
	for _, w := range out.Warnings {
		if strings.Contains(w, "prior attempt at this goal failed") {
			t.Fatalf("status-snapshot faileds must stay silent, got: %v", out.Warnings)
		}
	}
}

// Case B — Tailscale staleness. A standing constraint closed by a newer
// state must stop warning. Expected-fail: constraint lifecycle
// invalidation is the follow-up PR, not this arc.
func TestBenchTailscaleStaleConstraintQuits(t *testing.T) {
	t.Skip("expected-fail until constraint lifecycle invalidation (follow-up PR)")
	st := tmpStore(t)
	project := "acme/forge"
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("TS", 1),
		Type:      "constraint",
		Source:    "remember",
		Text:      "Deploys are HELD until Jay re-authenticates Tailscale on the forge host.",
		CreatedAt: benchDate(8),
	})
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("TS", 2),
		Type:      "state",
		Source:    "turn",
		Text:      "Jay re-authenticated Tailscale; deploys unblocked.",
		CreatedAt: benchDate(7),
	})
	out := benchAt(t, st, Request{
		Project:  project,
		Question: "why are the deploys held",
		Goal:     "ship the deploy",
	})
	for _, w := range out.Warnings {
		if strings.Contains(w, "standing constraint applies") {
			t.Fatalf("stale constraint still warns: %v", out.Warnings)
		}
	}
}

// Case C — clone-failure cluster regression guard. A cluster of real
// grounded clone failures packs; 3 of 4 was the live behavior before
// this arc.
func TestBenchCloneFailureClusterGuards(t *testing.T) {
	st := tmpStore(t)
	project := "acme/forge"
	clones := []claim.Record{
		{ID: benchID("CL", 1), Type: "failed", Source: "turn", CreatedAt: benchDate(33),
			Text:  "worktree setup for w22-ci died: `git worktree add` failed on the lock file.",
			Paths: []string{".claude/worktrees/w22-ci"}},
		{ID: benchID("CL", 2), Type: "failed", Source: "turn", CreatedAt: benchDate(33),
			Text:  "worktree setup for w22-android failed: git worktree add reported the branch already checked out.",
			Paths: []string{".claude/worktrees/w22-android"}},
		{ID: benchID("CL", 3), Type: "failed", Source: "turn", CreatedAt: benchDate(33),
			Text:  "worktree setup for w22-ios failed: git worktree add hit a stale admin entry; prune fixed it.",
			Paths: []string{".claude/worktrees/w22-ios"}},
		{ID: benchID("CL", 4), Type: "failed", Source: "turn", CreatedAt: benchDate(33),
			Text:  "worktree setup for w22-sweeps failed on the branch-already-checked-out guard; recovered with prune.",
			Paths: []string{".claude/worktrees/w22-sweeps"}},
	}
	for _, rec := range clones {
		writeRecProject(t, st, project, rec)
	}
	out := benchAt(t, st, Request{
		Project:  project,
		Question: "set up the w22 worktrees and clone the branches",
		Goal:     "clone each wave worktree and start the streams",
	})
	got := 0
	for _, h := range out.Context {
		if strings.Contains(h.Text, "worktree setup") {
			got++
		}
	}
	if got < 3 {
		t.Fatalf("clone-failure cluster must pack >=3, got %d: %s", got, textsOf(out))
	}
}

// A multi-day cluster with no constraint member is chatter, not a trap:
// silent regardless of vocabulary or candidacy.
func TestBenchRecurrenceNeedsConstraintMember(t *testing.T) {
	st := tmpStore(t)
	project := "acme/forge"
	for i := 0; i < 3; i++ {
		writeRecProject(t, st, project, claim.Record{
			ID:        benchID("NC", i),
			Type:      "failed",
			Source:    "turn",
			SessionID: benchID("SES", i),
			Text:      "the flaky-deploy gate tripped again on the staging host.",
			CreatedAt: benchDate(i * 5),
		})
	}
	out := benchAt(t, st, Request{
		Project:  project,
		Question: "why does the flaky-deploy gate keep tripping",
		Goal:     "run the deploy",
	})
	for _, w := range out.Warnings {
		if strings.Contains(w, "Recurring failure") {
			t.Fatalf("no-constraint cluster must stay silent: %v", out.Warnings)
		}
	}
}

// A cluster inside another project never warns here.
func TestBenchRecurrenceCrossProjectSilent(t *testing.T) {
	st := tmpStore(t)
	for i := 0; i < 3; i++ {
		writeRecProject(t, st, "other/repo", claim.Record{
			ID:        benchID("XP", i),
			Type:      "failed",
			Source:    "turn",
			SessionID: benchID("SES", i),
			Text:      "pr-size-check failed again at the size gate.",
			CreatedAt: benchDate(i * 5),
		})
		writeRecProject(t, st, "other/repo", claim.Record{
			ID:        benchID("XC", i),
			Type:      "constraint",
			Source:    "remember",
			Text:      "Always add the audit-bundle label before pr-size-check runs.",
			CreatedAt: benchDate(i * 5),
		})
	}
	out := benchAt(t, st, Request{
		Project:  "acme/forge",
		Question: "what about the pr-size-check gate",
		Goal:     "push a PR through pr-size-check",
	})
	for _, w := range out.Warnings {
		if strings.Contains(w, "Recurring failure") {
			t.Fatalf("cross-project cluster must stay silent: %v", out.Warnings)
		}
	}
}

// When a member constraint already packs with shippedOverlap, the
// standing-constraint warning covers the trap — no double warning.
func TestBenchRecurrenceNoDoubleWarning(t *testing.T) {
	st := tmpStore(t)
	project := "acme/forge"
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("DW", 1),
		Type:      "constraint",
		Source:    "remember",
		Text:      "A PR over 500 lines needs the audit-bundle label for pr-size-check.",
		CreatedAt: benchDate(4),
		Paths:     []string{".woodpecker/ci.yml"},
	})
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("DW", 2),
		Type:      "constraint",
		Source:    "remember",
		Text:      "pr-size-check still blocks: audit-bundle label then restart the pipeline.",
		CreatedAt: benchDate(0),
	})
	for i := 3; i < 5; i++ {
		writeRecProject(t, st, project, claim.Record{
			ID:        benchID("DW", i),
			Type:      "failed",
			Source:    "turn",
			SessionID: benchID("SES", i),
			Text:      "pr-size-check failed at 700 lines; audit-bundle needed.",
			CreatedAt: benchDate(i * 3),
		})
	}
	out := benchAt(t, st, Request{
		Project:  project,
		Question: "how do I push a large PR here",
		Goal:     "open the PR and pass pr-size-check with the audit-bundle label",
		Paths:    []string{".woodpecker/ci.yml"},
	})
	standing, recurring := false, false
	for _, w := range out.Warnings {
		if strings.Contains(w, "standing constraint applies") {
			standing = true
		}
		if strings.Contains(w, "Recurring failure") {
			recurring = true
		}
	}
	if recurring {
		t.Fatalf("covered cluster must not double-warn: %v", out.Warnings)
	}
	if !standing {
		t.Fatalf("standing-constraint warning expected: %v", out.Warnings)
	}
}

// A compound present in most of the window's records is prose, not a
// rare trap: it must not steal the warning slot.
func TestBenchRecurrenceRarityCap(t *testing.T) {
	st := tmpStore(t)
	project := "acme/forge"
	// code-review appears in 12 of 16 records (75% — far over the cap);
	// the real trap audit-gate appears in 4.
	for i := 0; i < 12; i++ {
		writeRecProject(t, st, project, claim.Record{
			ID:        benchID("CR", i),
			Type:      "failed",
			Source:    "turn",
			SessionID: benchID("SES", i),
			Text:      "code-review feedback applied to the handler.",
			CreatedAt: benchDate(i),
		})
	}
	writeRecProject(t, st, project, claim.Record{
		ID:        benchID("RG", 1),
		Type:      "constraint",
		Source:    "remember",
		Text:      "The audit-gate requires a signed manifest before any deploy.",
		CreatedAt: benchDate(2),
	})
	for i := 1; i < 4; i++ {
		writeRecProject(t, st, project, claim.Record{
			ID:        benchID("RG", i+1),
			Type:      "failed",
			Source:    "turn",
			SessionID: benchID("SES", i+20),
			Text:      "audit-gate rejected the deploy: manifest unsigned.",
			CreatedAt: benchDate(i * 4),
		})
	}
	out := benchAt(t, st, Request{
		Project:  project,
		Question: "why did the deploy fail",
		Goal:     "pass the audit-gate and deploy",
	})
	for _, w := range out.Warnings {
		if strings.Contains(w, "code-review") {
			t.Fatalf("non-rare compound must not warn: %v", out.Warnings)
		}
	}
	// The rare trap must be delivered — by the standing-constraint
	// warning if the constraint packs on overlap (the no-double rule
	// then correctly suppresses the recurrence warning), else by the
	// recurrence warning itself.
	delivered := strings.Contains(strings.Join(out.Warnings, " "), "standing constraint applies")
	if !delivered {
		for _, w := range out.Warnings {
			if strings.Contains(w, "audit-gate") {
				delivered = true
			}
		}
	}
	if !delivered {
		for _, h := range out.Context {
			if h.ID == benchID("RG", 1) {
				delivered = true
			}
		}
	}
	if !delivered {
		t.Fatalf("rare trap should win delivery, got warnings=%v context=%s", out.Warnings, textsOf(out))
	}
}
