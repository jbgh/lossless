package retrieve

import (
	"strings"
	"unicode/utf8"
)

// Warnings of the same kind merge into one line that cites every id
// (docs/algorithm.md §7b). Agents opened about 1% of warnings with
// get_record, and an ask carried 3.8 of them on average (up to 6), so five
// lines that each say "see <id>" were read as noise. Merging keeps every id and
// drops no warning kind; a kind with one id keeps its original sentence.

type warnKind struct {
	one    string // prefix emit writes for one record, up to "(see "
	tail   string // the sentence after "). " in emit's line
	many   string // merged prefix, up to "(see "
	manyTl string // merged closing sentence
}

// Kinds in warnKinds, by index.
const (
	warnFailed = iota
	warnCover
	warnConstraint
)

// line is the kind's warning for one record, as emit writes it.
func (k warnKind) line(id string) string { return k.one + id + "). " + k.tail }

// warnKinds holds the only copy of these sentences: emit writes them and
// mergeWarnings reads them back, so the two cannot drift.
var warnKinds = []warnKind{
	{
		one:    "A prior attempt at this goal failed (see ",
		tail:   "Do not repeat it without new evidence.",
		many:   "Prior attempts at this goal failed (see ",
		manyTl: "Do not repeat them without new evidence.",
	},
	{
		one:    "Existing implementation may already cover part of this goal (see ",
		tail:   "get_record that id before treating it as done.",
		many:   "Existing implementation may already cover part of this goal (see ",
		manyTl: "get_record the one that matches before treating it as done.",
	},
	{
		one:    "A standing constraint applies (see ",
		tail:   "Do not violate it without an explicit override.",
		many:   "Standing constraints apply (see ",
		manyTl: "Do not violate them without an explicit override.",
	},
}

// mergeWarnings folds emit's per-record lines into one line per kind, in
// the order each kind first appeared. Lines of any other shape (recurrence)
// pass through unchanged, in place.
func mergeWarnings(warnings []string) []string {
	if len(warnings) < 2 {
		return warnings
	}
	ids := make([][]string, len(warnKinds))
	slot := make([]int, len(warnKinds)) // index in out of the kind's line
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		k, id := matchWarnKind(w)
		if k < 0 {
			out = append(out, w)
			continue
		}
		if len(ids[k]) == 0 {
			slot[k] = len(out)
			out = append(out, w)
		}
		dup := false
		for _, have := range ids[k] {
			if have == id {
				dup = true
				break
			}
		}
		if !dup {
			ids[k] = append(ids[k], id)
		}
	}
	for k, list := range ids {
		if len(list) < 2 {
			continue
		}
		wk := warnKinds[k]
		out[slot[k]] = wk.many + strings.Join(list, ", ") + "). " + wk.manyTl
	}
	return out
}

// matchWarnKind returns the kind index and the cited id of one of emit's
// lines, or -1 when the line is not one of them.
func matchWarnKind(w string) (int, string) {
	for k, wk := range warnKinds {
		if !strings.HasPrefix(w, wk.one) || !strings.HasSuffix(w, "). "+wk.tail) || len(w) < len(wk.one)+len("). "+wk.tail) {
			continue
		}
		id := w[len(wk.one) : len(w)-len("). "+wk.tail)]
		if id == "" || strings.ContainsAny(id, " ,()") {
			continue
		}
		return k, id
	}
	return -1, ""
}

// warnClaimIDs returns every record id one of the pack-side warnings cites
// in its "(see …)" group: one for an unmerged line, several for a merged
// one. Any other line (recurrence, which may quote a rule that itself says
// "(see …)") cites none here.
func warnClaimIDs(w string) []string {
	known := false
	for _, wk := range warnKinds {
		if strings.HasPrefix(w, wk.one) || strings.HasPrefix(w, wk.many) {
			known = true
			break
		}
	}
	i := strings.Index(w, "(see ")
	if !known || i < 0 {
		return nil
	}
	rest := w[i+5:]
	j := strings.IndexByte(rest, ')')
	if j < 0 {
		return nil
	}
	var out []string
	for _, id := range strings.Split(rest[:j], ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// warnRecordCount is how many records a set of warnings stands for: each
// id a line cites, and one for a line that cites none with "(see …)"
// (recurrence). It equals the line count before merging, so the ask log's
// warning count keeps its meaning across the merge.
func warnRecordCount(warnings []string) int {
	n := 0
	for _, w := range warnings {
		if ids := warnClaimIDs(w); len(ids) > 0 {
			n += len(ids)
		} else {
			n++
		}
	}
	return n
}

// warnRuleText is a record sentence as a warning quotes it: one line, no
// backticks or double quotes that would break the quote, cut on a rune
// boundary.
func warnRuleText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.NewReplacer("\"", "'", "`", "'").Replace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n == max {
			return strings.TrimRight(s[:i], " ,;:") + "…"
		}
		n++
	}
	return s
}
