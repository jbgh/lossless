package retrieve

import (
	"math"
	"os"
	"sort"
	"strings"

	"lossless/internal/claim"
)

// The relevance ranker (v2) is the default. LOSSLESS_RANKER=v1 (or
// Engine.Ranker "v1") brings back the type-first pack for one release.
//
// v1 ranked by job: a failed-first slot, two records per type, and rescue
// passes that each fixed one incident. On blind-judged asks (60 from
// October, 40 unseen from November 2026; docs/eval.md) it scored 3.95 and
// 5.22 out of 10 per ask, below a plain "BM25 plus a shared file" baseline
// at 4.58 and 6.65. Every weight v2 first added on top of relevance (a
// boost for remembered records, recency, staleness, a shared identifier,
// a per-type penalty) measured as noise or as a loss.
//
// So the score is two things: how well the words match, and whether the
// record is about a file the ask named. It is the raw BM25 of the ask's
// own words against the record (lexV2), plus a fixed bonus for each path key the
// record shares with the ask. A file has two keys, its path as given and
// its base name (claim.PathKeys), so the same path is worth two bonuses
// and the same file name in another directory one. Raw, not scaled to the best hit: scaling made a
// shared file worth more than any wording match, and that lost to this
// form on both judged sets (5.02 and 6.56). The pack takes records in
// score order. Nothing reserves a slot, spreads the pack across types, or
// drops a decision because a later failure shares its file: those drops
// hid records the judges scored 1.7 of 2.
//
// The candidate routes, the relevance floor and the said filters are
// v1's, and so is the rule for which packed record warns. v1's caps on
// how many records of one type pack (said included) are not applied.
const (
	V2SaidLex     = 0.8  // a said hit's lexical score, against a typed hit's 1
	V2PathBonus   = 10.0 // per path key the record shares with the ask, in BM25 units
	V2Hop         = 1.0  // no match of its own, but on a file the first hits named
	V2RelativeCut = 0.2  // no shared file, and below this share of the best match: leave it out
)

// RankerV2 reports whether this engine ranks with v2. It does unless v1
// is asked for by name.
func (e Engine) RankerV2() bool {
	r := e.Ranker
	if r == "" {
		r = strings.TrimSpace(os.Getenv("LOSSLESS_RANKER"))
	}
	return !strings.EqualFold(r, "v1")
}

// v2Score is a candidate's v2 score. best is the ask's best lexical score:
// a vector hit, which has no BM25 of its own, is scaled to it so a perfect
// cosine is worth the best wording match.
func v2Score(c scored, best float64) float64 {
	rel := lexV2(c)
	rel += V2PathBonus * float64(c.npath)
	rel += c.vector * math.Max(best, 1)
	// A failure on the file the matching records are about shares no word
	// with "add rate limiting". It gets a small fixed score: enough to
	// pack when the store is small and the best match is modest, and
	// under the relative cut when real matches fill the pack (it changed
	// none of the 100 judged packs).
	if rel == 0 && c.hop > 0 {
		rel = V2Hop
	}
	return rel
}

// lexV2 is a candidate's wording match: the raw BM25 of the ask's own
// words for a typed record (pool.own, not the tape-widened query that
// only finds candidates), and of the said query for a said record.
func lexV2(c scored) float64 {
	switch {
	case c.ownFTS:
		return math.Max(0, -c.ownRaw)
	case c.saidFTS:
		return math.Max(0, -c.ftsRaw) * V2SaidLex
	}
	return 0
}

// bestLexV2 is the best wording match among the candidates.
func bestLexV2(cs []scored) float64 {
	best := 0.0
	for _, c := range cs {
		if v := lexV2(c); v > best {
			best = v
		}
	}
	return best
}

// packV2 scores cand with v2, in place so a trace shows the v2 scores,
// and packs in score order. net marks the recent-failed fallback, which
// v2 never offers: it is no match.
func packV2(cand []scored, limit int, net map[string]bool) []scored {
	best := bestLexV2(cand)
	cs := make([]scored, 0, len(cand))
	for i := range cand {
		cand[i].score = 0
		if cand[i].ungrounded || net[cand[i].rec.ID] {
			continue
		}
		cand[i].score = v2Score(cand[i], best)
		if cand[i].score > 0 {
			cs = append(cs, cand[i])
		}
	}
	if len(cs) == 0 {
		return nil
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].score != cs[j].score {
			return cs[i].score > cs[j].score
		}
		return cs[i].rec.CreatedAt > cs[j].rec.CreatedAt
	})
	floor := cutV2(cs)
	var out []scored
	var packedText [][]string
	tokens := 0
	for _, c := range cs {
		n := estimateTokens(mustJSON(toHit(c, false)))
		// Two records of one type that say nearly the same thing are one
		// memory restated, and one of them packs: the one an agent wrote
		// down over one picked up from a turn, else the newer, also when
		// the other scores higher because the ask names the old value.
		// This runs before the cap and the cut, which would both keep the
		// first of the two that was packed.
		if i := twinV2(c, out, packedText); i >= 0 {
			was := estimateTokens(mustJSON(toHit(out[i], false)))
			if prefersTwinV2(c, out[i]) && (n <= was || tokens-was+n <= limit) {
				tokens += n - was
				out[i] = c
				packedText[i] = claim.Tokens(c.rec.Text)
			}
			continue
		}
		if len(out) >= PackCap || belowCutV2(c, floor) {
			continue
		}
		if diverseSkip(c, out, packedText) {
			continue
		}
		if tokens+n > limit && len(out) > 0 {
			continue
		}
		out = append(out, c)
		packedText = append(packedText, claim.Tokens(c.rec.Text))
		tokens += n
	}
	return out
}

// twinV2 is the index of a packed record that c restates: same type,
// near-identical text (the bar diverseSkip uses), not the same claim. -1
// if there is none. Said records are quotes, not restatements of each
// other.
func twinV2(c scored, packed []scored, packedText [][]string) int {
	if c.rec.Type == "said" {
		return -1
	}
	var toks []string
	for i, p := range packed {
		if p.rec.Type != c.rec.Type || i >= len(packedText) {
			continue
		}
		if p.rec.ClaimHash != "" && p.rec.ClaimHash == c.rec.ClaimHash {
			continue
		}
		if toks == nil {
			toks = claim.Tokens(c.rec.Text)
		}
		if jaccard(toks, packedText[i]) >= DiversityJac {
			return i
		}
	}
	return -1
}

// prefersTwinV2 reports whether c should take its packed twin p's place.
// A record an agent wrote with remember stands over a turn's echo of it,
// whichever is newer: the echo may not warn (warnKindOf), so the swap
// would drop a warning. Between two of one kind the newer stands.
func prefersTwinV2(c, p scored) bool {
	ce, pe := claim.ExplicitMemory(c.rec.Source), claim.ExplicitMemory(p.rec.Source)
	if ce != pe {
		return ce
	}
	return c.rec.CreatedAt > p.rec.CreatedAt
}

// cutV2 is the score a record with no shared file must reach: a share of
// the best match with the file bonus taken out. The bonus is a fixed
// number of BM25 units while BM25 itself grows with the store, so in a
// small store one shared file outscores every wording match. Measured
// against the top score, the cut would then drop all of them.
func cutV2(cs []scored) float64 {
	best := 0.0
	for _, c := range cs {
		if v := c.score - V2PathBonus*float64(c.npath); v > best {
			best = v
		}
	}
	return best * V2RelativeCut
}

func belowCutV2(c scored, floor float64) bool {
	return c.npath == 0 && c.score < floor
}

// packSkipReasonV2 is why packV2 left a candidate out, for the trace.
func packSkipReasonV2(c scored, packed []scored, packedText [][]string, net map[string]bool, floor float64) string {
	switch {
	case net[c.rec.ID]:
		return "recent-failed-net"
	case c.ungrounded || c.score <= 0:
		return "off-topic"
	case twinV2(c, packed, packedText) >= 0:
		return "diversity"
	case len(packed) >= PackCap:
		return "pack-cap"
	case belowCutV2(c, floor):
		return "below-cut"
	case diverseSkip(c, packed, packedText):
		return "diversity"
	}
	return "token-limit"
}
