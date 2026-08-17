package retrieve

import (
	"encoding/json"
	"sort"
	"strings"

	"lossless/internal/claim"
)

func dropInvalidatedByNewerFailed(cand []scored) []scored {
	drop := map[string]bool{}
	for _, f := range cand {
		if f.rec.Type != "failed" {
			continue
		}
		for _, d := range cand {
			if d.rec.Type != "decision" && d.rec.Type != "constraint" {
				continue
			}
			if !sharePathCluster(f, d) || f.rec.CreatedAt <= d.rec.CreatedAt {
				continue
			}
			if jaccard(topicTokens(f.rec), topicTokens(d.rec)) < InvalidateJac {
				continue
			}
			drop[d.rec.ID] = true
		}
	}
	if len(drop) == 0 {
		return cand
	}
	var out []scored
	for _, c := range cand {
		if !drop[c.rec.ID] {
			out = append(out, c)
		}
	}
	return out
}

func dropOlderConflicts(cand []scored) []scored {
	drop := map[string]bool{}
	for i := range cand {
		a := cand[i]
		if a.rec.Type != "decision" && a.rec.Type != "constraint" {
			continue
		}
		for j := i + 1; j < len(cand); j++ {
			b := cand[j]
			if a.rec.Type != b.rec.Type || !sharePathCluster(a, b) {
				continue
			}
			if jaccard(claim.Tokens(a.rec.Text), claim.Tokens(b.rec.Text)) < ConflictJac {
				continue
			}
			if a.rec.CreatedAt >= b.rec.CreatedAt {
				drop[b.rec.ID] = true
			} else {
				drop[a.rec.ID] = true
			}
		}
	}
	if len(drop) == 0 {
		return cand
	}
	var out []scored
	for _, c := range cand {
		if !drop[c.rec.ID] {
			out = append(out, c)
		}
	}
	return out
}

// topicTokens drops file paths so a newer failed on the same
// file does not look like it invalidated an unrelated decision.
func topicTokens(rec claim.Record) []string {
	text := rec.Text
	for _, p := range rec.Paths {
		n := strings.ReplaceAll(p, "\\", "/")
		text = strings.ReplaceAll(text, p, " ")
		text = strings.ReplaceAll(text, n, " ")
		if i := strings.LastIndex(n, "/"); i >= 0 {
			n = n[i+1:]
		}
		if n != "" {
			text = strings.ReplaceAll(text, n, " ")
		}
		if i := strings.LastIndex(n, "."); i > 0 {
			text = strings.ReplaceAll(text, n[:i], " ")
		}
	}
	return claim.Tokens(text)
}

func typeCount(out []scored, typ string) int {
	n := 0
	for _, p := range out {
		if p.rec.Type == typ {
			n++
		}
	}
	return n
}

func hasOtherType(remaining, packed []scored, typ string) bool {
	for _, p := range packed {
		if p.rec.Type != typ {
			return true
		}
	}
	seen := map[string]int{}
	for _, p := range packed {
		seen[p.rec.Type]++
	}
	for _, c := range remaining {
		if c.rec.Type != typ && seen[c.rec.Type] < PackTypeCap {
			return true
		}
	}
	return false
}

func pack(cs []scored, limit int, head bool) []scored {
	if len(cs) == 0 {
		return nil
	}
	remaining := append([]scored(nil), cs...)
	var out []scored
	var packedText [][]string
	tokens := 0

	fits := func(c scored) (int, bool) {
		n := estimateTokens(mustJSON(toHit(c, false)))
		if tokens+n > limit && len(out) > 0 {
			return 0, false
		}
		return n, true
	}
	takeAt := func(i int) bool {
		c := remaining[i]
		if diverseSkip(c, out, packedText) {
			remaining = append(remaining[:i], remaining[i+1:]...)
			return false
		}
		n, ok := fits(c)
		if !ok {
			remaining = append(remaining[:i], remaining[i+1:]...)
			return false
		}
		out = append(out, c)
		packedText = append(packedText, claim.Tokens(c.rec.Text))
		tokens += n
		remaining = append(remaining[:i], remaining[i+1:]...)
		return true
	}

	bestFail := -1
	for i, c := range remaining {
		if c.failedOverlap == 1 && c.rec.Type == "failed" {
			if bestFail < 0 || c.score > remaining[bestFail].score {
				bestFail = i
			}
		}
	}
	if bestFail >= 0 {
		_ = takeAt(bestFail)
	}

	for len(out) < PackCap && len(remaining) > 0 {
		best := -1
		bestVal := -1e9
		bestSim := 1.0
		for i, c := range remaining {
			if c.score <= 0 && len(out) > 0 {
				continue
			}
			if diverseSkip(c, out, packedText) {
				continue
			}
			if typeCount(out, c.rec.Type) >= PackTypeCap && hasOtherType(remaining, out, c.rec.Type) {
				continue
			}
			sim := 0.0
			for _, p := range out {
				if s := coverSim(c, p, head); s > sim {
					sim = s
				}
			}
			val := c.score - WCoverage*sim
			if best < 0 || val > bestVal || (val == bestVal && sim < bestSim) {
				best = i
				bestVal = val
				bestSim = sim
			}
		}
		if best < 0 {
			break
		}
		if remaining[best].score <= 0 && len(out) > 0 {
			break
		}
		if !takeAt(best) {
			continue
		}
	}
	return out
}

func diverseSkip(c scored, packed []scored, packedText [][]string) bool {
	for _, p := range packed {
		if p.rec.ClaimHash != "" && p.rec.ClaimHash == c.rec.ClaimHash {
			return true
		}
	}
	toks := claim.Tokens(c.rec.Text)
	for _, prev := range packedText {
		if jaccard(toks, prev) >= DiversityJac {
			return true
		}
	}
	return false
}

func evictFailed(packed, all []scored, limit int) []scored {
	in := map[string]bool{}
	for _, p := range packed {
		in[p.rec.ID] = true
	}
	var missing []scored
	for _, c := range all {
		if c.failedOverlap != 1 || in[c.rec.ID] {
			continue
		}
		// Job 1 is "don't repeat this file/symbol", not "every claim
		// that mentioned failed". Lexical-only overlaps stay in rank.
		if c.path == 0 && c.symbol == 0 {
			continue
		}
		missing = append(missing, c)
	}
	if len(missing) == 0 {
		return packed
	}
	sort.SliceStable(missing, func(i, j int) bool {
		return missing[i].score > missing[j].score
	})
	for _, m := range missing {
		if typeCount(packed, "failed") >= PackTypeCap {
			break
		}
		if len(packed) < PackCap {
			packed = append(packed, m)
			in[m.rec.ID] = true
			continue
		}
		idx := -1
		for i, p := range packed {
			if p.rec.Type == "failed" && p.failedOverlap == 1 {
				continue
			}
			if idx < 0 || p.score < packed[idx].score {
				idx = i
			}
		}
		if idx < 0 {
			break
		}
		delete(in, packed[idx].rec.ID)
		packed[idx] = m
		in[m.rec.ID] = true
	}
	sortScored(packed)
	if len(packed) > PackCap {
		packed = packed[:PackCap]
	}
	_ = limit
	return packed
}

func emit(packed []scored) ([]Hit, []string, int) {
	hits := make([]Hit, 0, len(packed))
	var warnings []string
	seenWarn := map[string]bool{}
	for _, c := range packed {
		hits = append(hits, toHit(c, c.stale == 1))
		if c.failedOverlap == 1 && c.rec.Type == "failed" {
			w := "A prior attempt at this goal failed (see " + c.rec.ID + "). Do not repeat it without new evidence."
			if !seenWarn[w] {
				warnings = append(warnings, w)
				seenWarn[w] = true
			}
		}
		if c.shippedOverlap == 1 && c.rec.Type == "decision" {
			w := "Existing implementation may already cover part of this goal (see " + c.rec.ID + ")."
			if !seenWarn[w] {
				warnings = append(warnings, w)
				seenWarn[w] = true
			}
		}
		if c.shippedOverlap == 1 && c.rec.Type == "constraint" {
			w := "A standing constraint applies (see " + c.rec.ID + "). Do not violate it without an explicit override."
			if !seenWarn[w] {
				warnings = append(warnings, w)
				seenWarn[w] = true
			}
		}
	}
	tokens := 0
	if len(hits) > 0 {
		tokens += estimateTokens(mustJSON(hits))
	}
	if len(warnings) > 0 {
		tokens += estimateTokens(strings.Join(warnings, "\n"))
	}
	return hits, warnings, tokens
}

func toHit(c scored, verify bool) Hit {
	text := c.rec.Text
	if verify {
		text = "[verify] " + text
	}
	paths := c.rec.Paths
	if paths == nil {
		paths = []string{}
	}
	return Hit{
		ID:      c.rec.ID,
		Type:    c.rec.Type,
		Text:    text,
		When:    c.rec.CreatedAt,
		Paths:   paths,
		Harness: c.rec.Harness,
		Status:  c.rec.Status,
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
