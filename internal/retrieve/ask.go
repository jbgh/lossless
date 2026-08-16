package retrieve

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/store"
)

var errBadRequest = errors.New("project or workspace_root required")

// ErrBadRequest is returned when the ask is missing project identity.
var ErrBadRequest = errBadRequest

type Engine struct {
	Store *store.Store
	Now   func() time.Time
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func Ask(st *store.Store, req Request) (Response, error) {
	return (Engine{Store: st}).Ask(req)
}

func (e Engine) Ask(req Request) (Response, error) {
	q, err := normalize(req)
	if err != nil {
		return Response{}, err
	}
	empty := Response{Context: []Hit{}, Warnings: []string{}, Project: q.ProjectKey}
	ids, ftsBM25, err := e.candidates(q)
	if err != nil {
		return Response{}, err
	}
	if len(ids) == 0 {
		return empty, nil
	}
	recs, err := e.Store.GetMany(ids)
	if err != nil {
		return Response{}, err
	}
	var cand []scored
	seenHash := map[string]int{} // claim_hash -> index of newest
	for _, id := range ids {
		rec, ok := recs[id]
		if !ok || rec.Status != "active" {
			continue
		}
		if prev, ok := seenHash[rec.ClaimHash]; ok {
			if rec.CreatedAt >= cand[prev].rec.CreatedAt {
				cand[prev] = e.features(rec, q, ftsBM25)
			}
			continue
		}
		seenHash[rec.ClaimHash] = len(cand)
		cand = append(cand, e.features(rec, q, ftsBM25))
	}
	normBM25(cand)
	for i := range cand {
		cand[i].score = cand[i].preStale(q.Cold)
	}
	sortScored(cand)
	e.markStale(cand, q.WorkspaceRoot)
	for i := range cand {
		cand[i].score = cand[i].preStale(q.Cold) - WStale*cand[i].stale
	}
	sortScored(cand)
	packed := pack(cand, q.LimitTokens)
	packed = evictFailed(packed, cand, q.LimitTokens)
	hits, warnings, tokens := emit(packed)
	return Response{Context: hits, Warnings: warnings, Tokens: tokens, Project: q.ProjectKey}, nil
}

type scored struct {
	rec            claim.Record
	typeRank       float64
	recency        float64
	path           float64
	symbol         float64
	bm25           float64
	vector         float64
	failedOverlap  float64
	shippedOverlap float64
	stale          float64
	score          float64
	ftsRaw         float64
	isFTS          bool
}

func (s scored) preStale(cold bool) float64 {
	if cold {
		return WColdType*(s.typeRank/5) + WColdPath*s.path + WColdRecency*s.recency
	}
	return WFailedOverlap*s.failedOverlap +
		WShippedOverlap*s.shippedOverlap +
		WHotType*(s.typeRank/5) +
		WHotPath*s.path +
		WHotSymbol*s.symbol +
		WHotBM25*s.bm25 +
		WHotVector*s.vector +
		WHotRecency*s.recency
}

func (e Engine) candidates(q query) ([]string, map[string]float64, error) {
	fts := map[string]float64{}
	seen := map[string]bool{}
	var ids []string
	add := func(list []string) {
		for _, id := range list {
			if id == "" || seen[id] || len(ids) >= CandidateCap {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if q.Cold {
		pri, err := e.Store.ColdPriorityIDs(q.ProjectKey, ColdPriorityCap)
		if err != nil {
			return nil, nil, err
		}
		add(pri)
		if len(q.PathKeys) > 0 {
			p, err := e.Store.IDsByPath(q.ProjectKey, q.PathKeys, PathPerCap, ColdPathCap)
			if err != nil {
				return nil, nil, err
			}
			add(p)
		} else {
			st, err := e.Store.IDsByType(q.ProjectKey, "state", "", ColdStateCap)
			if err != nil {
				return nil, nil, err
			}
			add(st)
		}
		return ids, fts, nil
	}

	if match := ftsMatch(q.LookupTokens); match != "" {
		hits, err := e.Store.SearchFTS(q.ProjectKey, match, FTSCap)
		if err != nil {
			// malformed MATCH is degraded mode, not a hard failure
			hits = nil
		}
		var ftsIDs []string
		for _, h := range hits {
			fts[h.ID] = h.BM25
			ftsIDs = append(ftsIDs, h.ID)
		}
		add(ftsIDs)
	}
	if len(q.PathKeys) > 0 {
		p, err := e.Store.IDsByPath(q.ProjectKey, q.PathKeys, PathPerCap, PathTotalCap)
		if err != nil {
			return nil, nil, err
		}
		add(p)
	}
	if len(q.Symbols) > 0 {
		sy, err := e.Store.IDsBySymbol(q.ProjectKey, q.Symbols, SymbolPerCap, SymbolTotalCap)
		if err != nil {
			return nil, nil, err
		}
		add(sy)
	}
	since := e.now().UTC().AddDate(0, 0, -FailedLookback).Format(time.RFC3339)
	failed, err := e.Store.IDsByType(q.ProjectKey, "failed", since, FailedCap)
	if err != nil {
		return nil, nil, err
	}
	add(failed)
	dec, err := e.Store.DecisionIDsOverlapping(q.ProjectKey, q.PathKeys, q.Symbols, DecisionCap)
	if err != nil {
		return nil, nil, err
	}
	add(dec)
	return ids, fts, nil
}

func (e Engine) features(rec claim.Record, q query, fts map[string]float64) scored {
	s := scored{rec: rec}
	s.typeRank = float64(typeRank[rec.Type])
	created, err := time.Parse(time.RFC3339, rec.CreatedAt)
	if err != nil {
		created = e.now()
	}
	ageDays := e.now().Sub(created).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	s.recency = math.Pow(0.5, ageDays/HalfLifeDays)
	s.path = jaccard(q.PathKeys, claim.PathKeys(rec.Paths))
	qSym := q.Symbols
	rSym := make([]string, 0, len(rec.Symbols))
	for _, sy := range rec.Symbols {
		rSym = append(rSym, strings.ToLower(sy))
	}
	s.symbol = jaccard(qSym, rSym)
	if v, ok := fts[rec.ID]; ok {
		s.isFTS = true
		s.ftsRaw = v
	}
	overlapTokens := append(append([]string{}, q.QuestionTokens...), q.GoalTokens...)
	overlap := s.path > 0 || tokenOverlap(overlapTokens, rec.Text)
	if rec.Type == "failed" && (overlap || s.vector >= VectorGate) {
		s.failedOverlap = 1
	}
	if (rec.Type == "decision" || rec.Type == "state") && (s.path > 0 || s.symbol > 0 || tokenOverlap(overlapTokens, rec.Text)) {
		s.shippedOverlap = 1
	}
	return s
}

func normBM25(cs []scored) {
	var vals []float64
	for _, c := range cs {
		if c.isFTS {
			vals = append(vals, -c.ftsRaw)
		}
	}
	if len(vals) == 0 {
		return
	}
	min, max := vals[0], vals[0]
	for _, v := range vals[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	for i := range cs {
		if !cs[i].isFTS {
			cs[i].bm25 = 0
			continue
		}
		if max == min {
			cs[i].bm25 = 1
			continue
		}
		cs[i].bm25 = ((-cs[i].ftsRaw) - min) / (max - min)
	}
}

func (e Engine) markStale(cs []scored, workspace string) {
	if workspace == "" {
		return
	}
	n := StaleStatCap
	if n > len(cs) {
		n = len(cs)
	}
	for i := 0; i < n; i++ {
		if isStale(cs[i].rec, workspace) {
			cs[i].stale = 1
		}
	}
}

func isStale(rec claim.Record, workspace string) bool {
	if workspace == "" || len(rec.PathMtime) == 0 {
		return false
	}
	for _, p := range rec.Paths {
		stored, ok := rec.PathMtime[p]
		if !ok {
			continue
		}
		fi, err := os.Stat(filepath.Join(workspace, p))
		if err != nil {
			continue
		}
		if fi.ModTime().UnixMilli() > stored {
			return true
		}
	}
	return false
}

func sortScored(cs []scored) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].score != cs[j].score {
			return cs[i].score > cs[j].score
		}
		if cs[i].rec.CreatedAt != cs[j].rec.CreatedAt {
			return cs[i].rec.CreatedAt > cs[j].rec.CreatedAt
		}
		return cs[i].rec.ID > cs[j].rec.ID
	})
}

func pack(cs []scored, limit int) []scored {
	var out []scored
	var packedText [][]string
	tokens := 0
	for _, c := range cs {
		if c.score <= 0 && len(out) > 0 {
			break
		}
		if diverseSkip(c, out, packedText) {
			continue
		}
		hit := toHit(c, false)
		n := estimateTokens(mustJSON(hit))
		if tokens+n > limit && len(out) > 0 {
			break
		}
		out = append(out, c)
		packedText = append(packedText, claim.Tokens(c.rec.Text))
		tokens += n
		if len(out) >= PackCap {
			break
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
		if c.failedOverlap == 1 && !in[c.rec.ID] {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		return packed
	}
	for _, m := range missing {
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
	// drop extras if eviction appended past cap
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
