package retrieve

import (
	"os"
	"regexp"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// Warning decay for time-anchored constraints (docs/algorithm.md §7a).
//
// An incident-shaped constraint — "Since 2026-09-13 the Tailscale key on
// api-prod is EXPIRED, do not merge until …" — keeps warning long after
// the incident closes. The live repro is record
// 2026091304482500000000000000aa in acme/ops: the incident was closed by
// the state record 2026091417444400000000000000bb on 2026-09-14, but that
// state row carries no paths, so dropOlderConflicts (same-type + shared path
// cluster) and dropInvalidatedByNewerFailed (failed newer than a
// decision/constraint) both miss it. Nothing at read time links the two, and
// the read-time gate is off limits, so the constraint is retired here.
//
// Decay is warning-only. A decayed constraint still packs, with its visible
// date, so the history stays readable. `lossless supersede <id>` is the
// explicit primitive; decay is the blast-radius guard for the ones nobody
// gets round to retiring by hand.
//
// The two anchor kinds mean different things and decay differently:
//
//   - End anchors ("until <date>", "expires <date>") name a fixed
//     termination. Once the date passes the warning has nothing left to say.
//   - Start anchors ("since <date>", "as of <date>") name when the text
//     became true. On incident vocabulary that is a snapshot of a moment, so
//     it stops warning after StaleWindow. On a standing rule ("since
//     2026-01, CI requires Go 1.23") the same words are a rule with no end
//     date, and it never decays.
//
// A text with no anchor is unaffected: it never claimed an end and never
// claimed a start.

// DefaultStaleWindow is how long a start-anchored incident constraint keeps
// warning before it decays. Fourteen days is long enough to cover an
// incident's write-up-and-handoff span and short enough that the next
// unremembered outage does not inherit a month of silence.
const DefaultStaleWindow = 14 * 24 * time.Hour

// StaleWindowEnv overrides DefaultStaleWindow. The value is a Go duration
// (time.ParseDuration), so "336h" for 14 days — "14d" is not a Go duration.
// Zero, negative, or unparseable falls back to the default.
const StaleWindowEnv = "LOSSLESS_STALE_WINDOW"

// Decay reasons, reported by ConstraintDecay and shown by
// `inspect --prune`'s stale listing.
const (
	ReasonEndAnchor      = "end-anchor"
	ReasonIncidentWindow = "incident-window"
)

// endAnchorPat matches an explicit termination date. "until" and "expires"
// are the two shapes the spec names; both need the date to parse, so a
// standing "expires" in prose does not match.
var endAnchorPat = regexp.MustCompile(`(?i)\b(?:until|expires?)\s+(\d{4}-\d{2}(?:-\d{2})?(?:[T ]\d{2}:\d{2}(?::\d{2})?Z?)?)`)

// untilAnchorPat is the constraint's own stated end ("do not merge until
// 2026-12-01"). A future one keeps an incident constraint warning past the
// start-anchor window.
var untilAnchorPat = regexp.MustCompile(`(?i)\buntil\s+(\d{4}-\d{2}(?:-\d{2})?(?:[T ]\d{2}:\d{2}(?::\d{2})?Z?)?)`)

// startAnchorPat matches the moment the text became true. "as of" and
// "since" both qualify; a bare "as of yesterday" has no date and does not
// match, so it decays never rather than wrongly.
var startAnchorPat = regexp.MustCompile(`(?i)\b(?:since|as of)\s+(\d{4}-\d{2}(?:-\d{2})?(?:[T ]\d{2}:\d{2}(?::\d{2})?Z?)?)`)

// incidentWords are the vocabulary that makes a start anchor a snapshot of a
// moment rather than a standing rule. "not deployed" is a phrase, the rest
// are single words; each is matched on word boundaries so "download" is not
// a "down" and "outages" is not an "outage" false positive on prose.
var incidentWords = []string{
	"expired", "expires", "down", "broken", "blocked", "outage", "not deployed",
}

// incidentPat is the compiled form. "not deployed" must come before "not" so
// the alternation prefers the longer phrase; Go's regexp is leftmost-first,
// and the longer literal is listed first here.
var incidentPat = func() *regexp.Regexp {
	quoted := make([]string, 0, len(incidentWords))
	for _, w := range incidentWords {
		quoted = append(quoted, regexp.QuoteMeta(w))
	}
	// Longest first so a phrase wins over a word it contains.
	for i := 0; i < len(quoted); i++ {
		for j := i + 1; j < len(quoted); j++ {
			if len(quoted[j]) > len(quoted[i]) {
				quoted[i], quoted[j] = quoted[j], quoted[i]
			}
		}
	}
	return regexp.MustCompile(`(?i)\b(?:` + strings.Join(quoted, "|") + `)\b`)
}()

// StaleWindow is the effective incident window: the Engine override if set,
// else LOSSLESS_STALE_WINDOW, else DefaultStaleWindow. A non-positive or
// unparseable value falls back rather than disabling decay — a typo in an
// env var must not silently un-retire every incident constraint.
func StaleWindow() time.Duration { return staleWindow(0) }

func staleWindow(override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	if v := strings.TrimSpace(os.Getenv(StaleWindowEnv)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return DefaultStaleWindow
}

// Decay is the verdict for one constraint: whether its warning has decayed,
// under which rule, and the anchor that decided it. Anchor is always the
// visible date, so a caller can print the evidence rather than the rule name
// alone.
type Decay struct {
	Decayed bool
	Reason  string
	Anchor  string
}

// ConstraintDecay decides whether a constraint's warning has decayed at now.
//
// The two anchor rules are independent, and the end anchor is checked first
// because it is unconditional. A text can carry both: the live repro does —
// "Since 2026-09-13T04:29:03Z api-prod's Tailscale node key is EXPIRED …
// NOT deployed … (eu expires 2026-10-06T22:28:49Z)". That trailing date is a
// subordinate clause about a different box, api-eu, so it must not shield
// the api-prod incident from the start-anchor rule. An end anchor that has
// not passed is therefore not a veto; it simply does not fire.
func ConstraintDecay(rec claim.Record, now time.Time, window time.Duration) Decay {
	if window <= 0 {
		window = DefaultStaleWindow
	}
	if end, raw, ok := lastAnchor(endAnchorPat, rec.Text); ok && now.After(end) {
		return Decay{Decayed: true, Reason: ReasonEndAnchor, Anchor: raw}
	}
	start, ok := firstAnchor(startAnchorPat, rec.Text)
	if !ok {
		return Decay{}
	}
	if until, _, ok := lastAnchor(untilAnchorPat, rec.Text); ok && !now.After(until) {
		// "blocked since 2026-09-13, do not merge until 2026-12-01" names
		// its own end: it warns until then, not for the window. An
		// "expires <date>" is not this veto: it states when some thing
		// expires ("eu expires 2026-10-06" about another box), not how
		// long the constraint holds.
		return Decay{}
	}
	if !incidentPat.MatchString(rec.Text) {
		// "since 2026-01, CI requires Go 1.23" is a standing rule.
		return Decay{}
	}
	created, err := time.Parse(time.RFC3339, rec.CreatedAt)
	if err != nil {
		created = start
	}
	base := start
	if created.After(base) {
		// A back-dated import (a constraint filed today about an incident
		// from last year) earns its own window from the day it was filed,
		// not from the day it happened.
		base = created
	}
	if now.Before(base.Add(window)) {
		return Decay{}
	}
	return Decay{Decayed: true, Reason: ReasonIncidentWindow, Anchor: start.Format(time.RFC3339)}
}

// firstAnchor returns the earliest date the pattern names. Earliest, not
// first-found: a start anchor decays from the moment the text became
// true, not from whichever date the regex happened to reach.
func firstAnchor(pat *regexp.Regexp, text string) (time.Time, bool) {
	var best time.Time
	found := false
	for _, m := range pat.FindAllStringSubmatch(text, -1) {
		d, ok := parseAnchorDate(m[1])
		if !ok {
			continue
		}
		if !found || d.Before(best) {
			best, found = d, true
		}
	}
	return best, found
}

// lastAnchor returns the latest date the pattern names. An end anchor
// retires the warning only once every termination it names has passed:
// "the staging key expires 2026-09-20, the prod key expires 2027-01-05"
// still has something to say after the first date.
//
// The time is when the anchor has passed (the end of the period it
// names); the string is the date as written, for display.
func lastAnchor(pat *regexp.Regexp, text string) (time.Time, string, bool) {
	var best time.Time
	var raw string
	found := false
	for _, m := range pat.FindAllStringSubmatch(text, -1) {
		d, ok := parseAnchorEnd(m[1])
		if !ok {
			continue
		}
		if !found || d.After(best) {
			best, raw, found = d, m[1], true
		}
	}
	return best, raw, found
}

// parseAnchorDate accepts the shapes these texts actually use: a full RFC3339
// stamp, a date with a clock, a bare date, and a year-month ("since
// 2026-01"). All are read as UTC. An unparseable value is not an anchor.
func parseAnchorDate(s string) (time.Time, bool) {
	t, _, ok := parseAnchor(s)
	return t, ok
}

// parseAnchorEnd is the moment an end anchor has passed: the end of the
// period it names. "until 2026-12" holds through December and "until
// 2026-09-30" through that day; a stamp with a clock ends at that clock.
func parseAnchorEnd(s string) (time.Time, bool) {
	t, layout, ok := parseAnchor(s)
	if !ok {
		return t, false
	}
	switch layout {
	case "2006-01":
		return t.AddDate(0, 1, 0), true
	case "2006-01-02":
		return t.AddDate(0, 0, 1), true
	}
	return t, true
}

func parseAnchor(s string) (time.Time, string, bool) {
	// Every shape the anchor patterns capture, in either case (they match
	// case-insensitively): date, year-month, and a date with a clock (T or
	// space, with or without seconds and Z).
	norm := strings.TrimSuffix(strings.ToUpper(strings.Replace(s, " ", "T", 1)), "Z")
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
		"2006-01",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, norm); err == nil {
			return t.UTC(), l, true
		}
	}
	return time.Time{}, "", false
}

// decayConstraintWarnings drops the standing-constraint warning of every
// decayed constraint in the pack, and re-enables it when a newer active
// constraint re-states the trap (Reconfirmed).
//
// The filter runs on the emitted warning list rather than inside emit, so
// pack.go's warning text stays the one place it is written and the read-time
// gate is untouched. Warnings are matched on the record id they cite, which
// is how emit() identifies them — a failed or decision warning about a
// different record is never caught by this.
func decayConstraintWarnings(packed []scored, warnings []string, hits []Hit, tokens int, memo *decayMemo) ([]string, int) {
	decayed := map[string]bool{}
	for _, c := range packed {
		if c.rec.Type != "constraint" {
			continue
		}
		if !memo.decayed(c.rec) {
			continue
		}
		decayed[c.rec.ID] = true
	}
	if len(decayed) == 0 {
		return warnings, tokens
	}
	kept := make([]string, 0, len(warnings))
	for _, w := range warnings {
		drop := false
		for id := range decayed {
			if strings.Contains(w, "(see "+id+")") {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, w)
		}
	}
	if len(kept) == len(warnings) {
		return warnings, tokens
	}
	// Recount the way emit counted: hits plus the surviving warnings.
	total := 0
	if len(hits) > 0 {
		total += estimateTokens(mustJSON(hits))
	}
	if len(kept) > 0 {
		total += estimateTokens(strings.Join(kept, "\n"))
	}
	return kept, total
}

// StaleCandidate is one decayed, still-active constraint: `inspect --prune`
// lists these as a prompt to run `lossless supersede <id> [reason]`. It is a
// listing only; nothing here supersedes or deletes.
type StaleCandidate struct {
	ID      string  `json:"id"`
	Project string  `json:"project"`
	When    string  `json:"when"`
	AgeDays float64 `json:"age_days"`
	Anchor  string  `json:"anchor"`
	Reason  string  `json:"reason"`
	Text    string  `json:"text"`
}

// StaleCandidates lists the project's decayed, still-active constraints,
// newest first. A superseded record is gone from the listing because
// supersede is the resolution it is prompting — the row and its text survive
// in the store either way.
func StaleCandidates(st *store.Store, project string, now time.Time, window time.Duration) ([]StaleCandidate, error) {
	if st == nil {
		return nil, nil
	}
	recs, err := st.ListActive(project)
	if err != nil {
		return nil, err
	}
	return StaleCandidatesIn(st, recs, now, window), nil
}

// StaleCandidatesIn is StaleCandidates over records the caller already
// holds, which may span projects (`inspect --prune` with no --project).
// Non-constraints and non-active records are skipped.
func StaleCandidatesIn(st *store.Store, recs []claim.Record, now time.Time, window time.Duration) []StaleCandidate {
	var out []StaleCandidate
	for _, r := range recs {
		if r.Type != "constraint" || r.Status != "active" {
			continue
		}
		// A newer active constraint that re-states the trap keeps the
		// warning earning its place, so it is not listed.
		d, ok := constraintDecayed(st, r, now, window)
		if !ok {
			continue
		}
		age := 0.0
		if created, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil {
			age = now.Sub(created).Hours() / 24
			if age < 0 {
				age = 0
			}
		}
		out = append(out, StaleCandidate{
			ID: r.ID, Project: r.ProjectKey, When: r.CreatedAt, AgeDays: age,
			Anchor: d.Anchor, Reason: d.Reason, Text: r.Text,
		})
	}
	sortStale(out)
	return out
}

func sortStale(s []StaleCandidate) {
	// Newest first, id as the tiebreaker so the listing is deterministic.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0; j-- {
			a, b := s[j-1], s[j]
			if a.When > b.When || (a.When == b.When && a.ID <= b.ID) {
				break
			}
			s[j-1], s[j] = b, a
		}
	}
}

// Reconfirmed reports whether a newer active constraint re-states the trap
// rec describes, which re-enables its warning.
//
// The match is deterministic by construction: the same claim_hash, or the
// same rare code-shaped identifiers recurrence detection already clusters
// on (recIdents, internal/retrieve/recurrence.go): at least two of rec's
// identifiers. A single shared identifier is too loose:
// any later rule that names the same host ("never restart api-prod in
// business hours") would revive an unrelated incident. The newer
// constraint warns on its own merits anyway, so a strict match loses
// nothing. Anything fuzzier is
// auto-supersede-on-contradiction, which is out of scope. A record that
// mentions the same ISO date is not a re-confirmation: recIdents can pick a
// date fragment out of "2026-09-13T04:29:03Z", and two unrelated constraints
// filed the same day would match on it.
//
// A newer constraint that has itself decayed re-confirms nothing: in a
// chain of restatements of one incident, every link decays on its own
// clock, and the oldest must not be revived by a newer one that has gone
// quiet.
func Reconfirmed(st *store.Store, rec claim.Record, now time.Time, window time.Duration) (string, bool) {
	if st == nil || rec.Type != "constraint" {
		return "", false
	}
	since := rec.CreatedAt
	rows, err := st.RecordsForRecurrence(rec.ProjectKey, since)
	if err != nil {
		return "", false
	}
	projToks := claimTokensLower(rec.ProjectKey)
	want := decRareIdents(rec.Text, projToks)
	for _, row := range rows {
		if row.ID == rec.ID || row.Type != "constraint" {
			continue
		}
		// RecordsForRecurrence filters created_at >= since; a re-confirmation
		// must be strictly newer than the record it re-enables.
		if row.CreatedAt <= rec.CreatedAt {
			continue
		}
		if !claimHashMatch(rec, row) && !decSharesIdent(want, row.Text, projToks) {
			continue
		}
		if ConstraintDecay(claim.Record{ID: row.ID, Type: row.Type, Text: row.Text, CreatedAt: row.CreatedAt}, now, window).Decayed {
			continue
		}
		return row.ID, true
	}
	return "", false
}

func claimHashMatch(rec claim.Record, row store.RecurrenceRow) bool {
	return claim.Hash(rec.ProjectKey, row.Type, row.Text) == rec.ClaimHash
}

// decRareIdentPat matches the ISO date fragments recIdents can lift out of a
// timestamp: "2026-09-13t04" and "2026-10-06t22" are hyphenated and survive
// every filter, so two unrelated constraints filed the same day would share
// one and look like a re-confirmation. A date is not an identifier, and this
// is the one place that distinction matters.
var decRareIdentPat = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}t?\d*$|^t?\d{2}$`)

// decRareIdents is RareIdents with date fragments removed.
func decRareIdents(text string, projToks map[string]bool) map[string]bool {
	out := map[string]bool{}
	for id := range RareIdents(text, "") {
		if decRareIdentPat.MatchString(id) {
			continue
		}
		out[id] = true
	}
	if len(projToks) == 0 {
		return out
	}
	// The exported helper filters the project name via its own key argument;
	// re-apply the caller's token set so a caller holding project tokens from
	// a different source gets the same filter.
	for id := range out {
		for tok := range projToks {
			if strings.Contains(id, tok) {
				delete(out, id)
				break
			}
		}
	}
	return out
}

// decSharesIdent reports whether text carries at least two of the rare
// identifiers want names. A record with fewer than two re-confirms only by
// claim_hash: one identifier is a host or a job name, not a trap.
func decSharesIdent(want map[string]bool, text string, projToks map[string]bool) bool {
	const need = 2
	if len(want) < need {
		return false
	}
	shared := 0
	for id := range decRareIdents(text, projToks) {
		if want[id] {
			shared++
			if shared >= need {
				return true
			}
		}
	}
	return false
}

// decayMemo is one ask's decay verdicts: one clock reading and one
// Reconfirmed query per constraint, shared by the pack's warnings and the
// recurrence route, so the two cannot disagree about a constraint that
// crosses its window mid-ask.
type decayMemo struct {
	st     *store.Store
	now    time.Time
	window time.Duration
	m      map[string]bool
}

func newDecayMemo(st *store.Store, now time.Time, window time.Duration) *decayMemo {
	return &decayMemo{st: st, now: now, window: window, m: map[string]bool{}}
}

func (d *decayMemo) decayed(rec claim.Record) bool {
	if rec.Type != "constraint" {
		return false
	}
	if v, ok := d.m[rec.ID]; ok {
		return v
	}
	_, v := constraintDecayed(d.st, rec, d.now, d.window)
	d.m[rec.ID] = v
	return v
}

// constraintDecayed reports whether a constraint's warning has decayed at
// now and no newer constraint re-confirms it. It is the one verdict the
// pack's warnings, the recurrence warnings, and the stale listing share.
func constraintDecayed(st *store.Store, rec claim.Record, now time.Time, window time.Duration) (Decay, bool) {
	d := ConstraintDecay(rec, now, window)
	if !d.Decayed {
		return d, false
	}
	if _, live := Reconfirmed(st, rec, now, window); live {
		return d, false
	}
	return d, true
}
