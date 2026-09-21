package retrieve

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// Recurrence-keyed warnings (docs/algorithm.md §7). A gate, script, or
// tool identifier that keeps showing up in a project's failure and
// constraint records is a standing trap — the audit-bundle miss: five
// prior trips of `pr-size-check`, none of them packable, because the
// records were pathless and the ask shared no content vocabulary with
// them. Frequency of a rare code-shaped identifier is the one signal
// that needs zero vocabulary agreement with the ask.

var recIdentPat = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._-]{3,}`)

// Version numbers and ticket/PR ids are not traps: v1.2.3, gh-1234.
var recVersionPat = regexp.MustCompile(`^v?\d+([._-]\d+)+$`)
var recTicketPat = regexp.MustCompile(`^[a-z]{1,5}-\d+$`)

// File suffixes make an identifier a path, not a gate: ci.yml mentions
// are config chatter, not a recurring trap.
var recIdentExt = map[string]bool{
	".yml": true, ".yaml": true, ".go": true, ".ts": true, ".tsx": true,
	".js": true, ".json": true, ".md": true, ".sh": true, ".py": true,
	".sql": true, ".swift": true, ".kt": true, ".rs": true, ".rb": true,
}

// shaped separates identifiers from prose: gates and tools are joined
// with -, _, or . (pr-size-check, workspace_root). claim.CodeShaped is
// NOT usable here — its hyphenIdent demands digits or single-char
// segments, so it rejects pr-size-check itself.
func recShaped(s string) bool {
	return strings.ContainsAny(s, "-_.")
}

// recKey normalizes separator variants so pr-size-check, pr_size_check,
// and pr.size.check count as one trap.
func recKey(s string) string {
	return strings.NewReplacer("_", "-", ".", "-").Replace(s)
}

type recurrenceCluster struct {
	ident string
	count int
	ref   store.RecurrenceRow // reference record: newest constraint (always exists)
	ids   []string            // member record ids
}

// recurrenceClusters scans the project's active failed/constraint
// records inside the recency window and groups them by identifier.
func recurrenceClusters(st *store.Store, project string, now time.Time) []recurrenceCluster {
	if st == nil {
		return nil
	}
	since := now.AddDate(0, 0, -RecurrenceWindowDays).UTC().Format(time.RFC3339)
	recs, err := st.RecordsForRecurrence(project, since)
	if err != nil || len(recs) == 0 {
		return nil
	}
	projToks := claimTokensLower(project)
	byIdent := map[string][]store.RecurrenceRow{}
	for _, rec := range recs {
		for ident := range recIdents(rec.Text, projToks) {
			byIdent[ident] = append(byIdent[ident], rec)
		}
	}
	// Rarity: a compound that shows up in most of the window's records
	// is prose ("code-review"), not a trap. The floor keeps small
	// projects and fixtures alive; the ratio scales with the store.
	rareMax := 10
	if r := len(recs) / 20; r > rareMax {
		rareMax = r
	}
	var out []recurrenceCluster
	for ident, rs := range byIdent {
		if len(rs) < RecurrenceMinRecords || len(rs) > rareMax {
			continue
		}
		// A cluster without a constraint is chatter: real standing traps
		// get written up as rules. This gate removed first-row/img-src
		// class noise from the live store while keeping audit-bundle.
		// Recurrence is repeated work across sessions or days, not one
		// session's retry burst: require members to span at least two
		// distinct days or two distinct sessions.
		hasConstraint := false
		ref := rs[0]
		newest := rs[0]
		ids := make([]string, 0, len(rs))
		days := map[string]bool{}
		sessions := map[string]bool{}
		for _, r := range rs {
			ids = append(ids, r.ID)
			if len(r.CreatedAt) >= 10 {
				days[r.CreatedAt[:10]] = true
			}
			if r.SessionID != "" {
				sessions[r.SessionID] = true
			}
			if r.CreatedAt > newest.CreatedAt {
				newest = r
			}
			if r.Type == "constraint" && (ref.Type != "constraint" || r.CreatedAt >= ref.CreatedAt) {
				ref = r
				hasConstraint = true
			}
		}
		if !hasConstraint {
			continue
		}
		if len(days) < 2 && len(sessions) < 2 {
			continue
		}
		out = append(out, recurrenceCluster{ident: ident, count: len(rs), ref: ref, ids: ids})
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Newest rule first: a freshly written standing rule should
		// surface on every ask until a newer one is written. Chronic
		// count only breaks ties among equally fresh rules.
		if out[i].ref.CreatedAt != out[j].ref.CreatedAt {
			return out[i].ref.CreatedAt > out[j].ref.CreatedAt
		}
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].ident < out[j].ident
	})
	return out
}

// recIdents extracts the code-shaped identifiers of one record text,
// keyed by recKey-normalized form.
func recIdents(text string, projToks map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, m := range recIdentPat.FindAllString(text, -1) {
		if strings.Contains(m, "://") {
			continue
		}
		s := strings.ToLower(strings.Trim(m, "._-"))
		if len(s) < 5 || !recShaped(s) || recVersionPat.MatchString(s) || recTicketPat.MatchString(s) {
			continue
		}
		extSkip := false
		for ext := range recIdentExt {
			if strings.HasSuffix(s, ext) {
				extSkip = true
				break
			}
		}
		if extSkip {
			continue
		}
		bare := strings.Trim(s, "0123456789-_.")
		if bare == "" || !strings.ContainsFunc(bare, func(r rune) bool { return r >= 'a' && r <= 'z' }) {
			continue
		}
		skip := false
		for tok := range projToks {
			if strings.Contains(s, tok) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		out[recKey(s)] = true
	}
	return out
}

func claimTokensLower(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range claim.Tokens(s) {
		if len(t) >= 4 {
			out[t] = true
		}
	}
	return out
}

// recurrenceWarnings fires on cluster existence, not on ask relevance.
// Standing traps are project-level: the audit-bundle miss happened on an
// ask that shared no real vocabulary with the trap records, and any
// ask-side relevance gate is either too porous (stopword FTS hits) or
// misses exactly the incident it exists for. The cluster gates above —
// rarity, a constraint member, day/session span, one warning per ask —
// bound the noise instead. Suppressed when any member constraint
// already delivered the standing-constraint warning from the pack.
func recurrenceWarnings(st *store.Store, q query, packed []scored, now time.Time) []string {
	clusters := recurrenceClusters(st, q.ProjectKey, now)
	if len(clusters) == 0 {
		return nil
	}
	warnedAlready := map[string]bool{}
	for _, p := range packed {
		if p.rec.Type == "constraint" && p.shippedOverlap == 1 {
			warnedAlready[p.rec.ID] = true
		}
	}
	var live []recurrenceCluster
	for i := range clusters {
		c := clusters[i]
		covered := false
		for _, id := range c.ids {
			if warnedAlready[id] {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		live = append(live, c)
	}
	// live preserves recurrenceClusters' count/newest order; the ident
	// tiebreaker keeps two fully tied clusters deterministic.
	var out []string
	for _, c := range live {
		if len(out) >= RecurrenceMaxWarn {
			break
		}
		out = append(out, fmt.Sprintf(
			"Recurring failure in this project: `%s` appears in %d failure/constraint records over the last %d days (newest %s). get_record the newest id and confirm this goal will not trip it again without an explicit override.",
			c.ident, c.count, RecurrenceWindowDays, c.ref.ID))
	}
	return out
}
