package retrieve

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/embed"
	"lossless/internal/store"
)

var errBadRequest = errors.New("project or workspace_root required")

// ErrBadRequest is returned when the ask is missing project identity.
var ErrBadRequest = errBadRequest

type Engine struct {
	Store         *store.Store
	Embedder      embed.Embedder
	Now           func() time.Time
	Home          string
	LocateSession func(project, workspace string) string
}

func (e Engine) embedder() embed.Embedder {
	if e.Embedder != nil {
		return e.Embedder
	}
	if e.Store != nil {
		return e.Store.Embedder
	}
	return nil
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func Ask(st *store.Store, req Request) (Response, error) {
	var emb embed.Embedder
	if st != nil {
		emb = st.Embedder
	}
	return (Engine{Store: st, Embedder: emb}).Ask(req)
}

func (e Engine) Ask(req Request) (Response, error) {
	q, err := normalize(req)
	if err != nil {
		return Response{}, err
	}
	q = e.maybeCompile(req, q)
	seedPaths := append([]string{}, q.PathKeys...)
	q = e.hydrateActions(req, q)
	prof := selectProfile(q)
	empty := Response{Context: []Hit{}, Warnings: []string{}, Project: q.ProjectKey}
	ids, ftsBM25, knn, err := e.candidates(q)
	if err != nil {
		return Response{}, err
	}
	if len(ids) == 0 {
		e.recordAsk(req, q, seedPaths, empty)
		return empty, nil
	}
	recs, err := e.Store.GetMany(ids)
	if err != nil {
		return Response{}, err
	}
	var cand []scored
	seenHash := map[string]int{} // claim_hash -> index of newest
	dropFix := e.Store != nil && e.Store.ProjectHasLiveWork(q.ProjectKey)
	for _, id := range ids {
		rec, ok := recs[id]
		if !ok || rec.Status != "active" {
			continue
		}
		if dropFix && claim.FixtureSession(rec.SessionID) {
			continue
		}
		if extractNoise(rec) {
			continue
		}
		if prev, ok := seenHash[rec.ClaimHash]; ok {
			if rec.CreatedAt >= cand[prev].rec.CreatedAt {
				cand[prev] = e.features(rec, q, ftsBM25, knn)
			}
			continue
		}
		seenHash[rec.ClaimHash] = len(cand)
		cand = append(cand, e.features(rec, q, ftsBM25, knn))
	}
	cand = dropOlderConflicts(cand)
	cand = dropInvalidatedByNewerFailed(cand)
	normBM25(cand)
	for i := range cand {
		cand[i].score = cand[i].preStale(prof)
	}
	sortScored(cand)
	e.markStale(cand, q.WorkspaceRoot)
	for i := range cand {
		cand[i].score = cand[i].preStale(prof) - WStale*cand[i].stale
	}
	sortScored(cand)
	packed := pack(cand, q.LimitTokens, q.Head)
	packed = evictFailed(packed, cand, q.LimitTokens)
	hits, warnings, tokens := emit(packed)
	out := Response{Context: hits, Warnings: warnings, Tokens: tokens, Project: q.ProjectKey}
	e.recordAsk(req, q, seedPaths, out)
	return out, nil
}

type scored struct {
	rec            claim.Record
	typeRank       float64
	recency        float64
	path           float64
	symbol         float64
	bm25           float64
	vector         float64
	agree          float64
	failedOverlap  float64
	shippedOverlap float64
	failedWeak     float64
	shippedWeak    float64
	served         float64
	dwell          float64
	oon            float64
	stale          float64
	score          float64
	ftsRaw         float64
	isFTS          bool
}

func (s scored) pFail() float64 {
	return s.failedOverlap + PFailWeak*s.failedWeak
}

func (s scored) pRegress() float64 {
	return s.shippedOverlap + PRegressWeak*s.shippedWeak
}

func (s scored) pAnswer(p Profile) float64 {
	w := mixFor(p)
	ans := w.typeW*(s.typeRank/5) +
		w.path*s.path +
		w.symbol*s.symbol +
		w.bm25*s.bm25 +
		w.vector*s.vector +
		w.agree*s.agree +
		w.recency*s.recency
	if s.oon == 1 {
		ans *= 1 - WOon
	}
	return ans
}

func (s scored) preStale(p Profile) float64 {
	if p == ProfileHead {
		w := mixFor(p)
		return w.typeW*(s.typeRank/5) + w.path*s.path + w.recency*s.recency
	}
	return WFailedOverlap*s.pFail() +
		WShippedOverlap*s.pRegress() +
		s.pAnswer(p) +
		WDwell*s.dwell -
		WServed*s.served
}

func (e Engine) candidates(q query) ([]string, map[string]float64, map[string]float64, error) {
	fts := map[string]float64{}
	knn := map[string]float64{}
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
	if q.Head {
		pri, err := e.Store.HeadPriorityIDs(q.ProjectKey, HeadFailedCap, HeadDecisionCap, HeadConstraintCap)
		if err != nil {
			return nil, nil, nil, err
		}
		add(pri)
		if len(q.PathKeys) > 0 {
			p, err := e.Store.IDsByPath(q.ProjectKey, q.PathKeys, PathPerCap, ColdPathCap)
			if err != nil {
				return nil, nil, nil, err
			}
			add(p)
		} else if len(ids) < ColdPriorityCap {
			st, err := e.Store.IDsByType(q.ProjectKey, "state", "", ColdStateCap)
			if err != nil {
				return nil, nil, nil, err
			}
			add(st)
		}
		return ids, fts, knn, nil
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
			return nil, nil, nil, err
		}
		add(p)
	}
	if len(q.Symbols) > 0 {
		sy, err := e.Store.IDsBySymbol(q.ProjectKey, q.Symbols, SymbolPerCap, SymbolTotalCap)
		if err != nil {
			return nil, nil, nil, err
		}
		add(sy)
	}
	failedOv, err := e.Store.TypeIDsOverlapping(q.ProjectKey, "failed", q.PathKeys, q.Symbols, FailedCap)
	if err != nil {
		return nil, nil, nil, err
	}
	add(failedOv)
	dec, err := e.Store.DecisionIDsOverlapping(q.ProjectKey, q.PathKeys, q.Symbols, DecisionCap)
	if err != nil {
		return nil, nil, nil, err
	}
	add(dec)
	con, err := e.Store.ConstraintIDsOverlapping(q.ProjectKey, q.PathKeys, q.Symbols, ConstraintCap)
	if err != nil {
		return nil, nil, nil, err
	}
	add(con)
	// Vector kNN: paraphrase that shares no token/path/symbol.
	// Fail-open. Missing embedder or a failed encode is degraded mode.
	if vecIDs := e.vectorHits(q, knn); len(vecIDs) > 0 {
		add(vecIDs)
	}
	// Pathless ask: hop through files the first pass already found.
	// "add rate limiting" hits the limiter decision, then pulls the Redis
	// failed on that same file — without dumping last week's warehouse timeout.
	if len(q.PathKeys) == 0 && len(ids) > 0 {
		inferred, err := e.inferredPaths(ids)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(inferred) > 0 {
			for _, typ := range []string{"failed", "decision", "constraint"} {
				extra, err := e.Store.TypeIDsOverlapping(q.ProjectKey, typ, inferred, nil, InferTypeCap)
				if err != nil {
					return nil, nil, nil, err
				}
				add(extra)
			}
		}
	}
	// Recent faileds only when structure found nothing. Safety net, not default.
	if len(ids) == 0 {
		failedRecent, err := e.Store.IDsByType(q.ProjectKey, "failed", "", FailedRecentCap)
		if err != nil {
			return nil, nil, nil, err
		}
		add(failedRecent)
	}
	return ids, fts, knn, nil
}

func (e Engine) vectorHits(q query, knn map[string]float64) []string {
	emb := e.embedder()
	if emb == nil || e.Store == nil {
		return nil
	}
	qtext := embed.Query(append(append([]string{}, q.QuestionTokens...), q.GoalTokens...))
	if qtext == "" {
		return nil
	}
	vecs, err := emb.Embed([]string{qtext})
	if err != nil || len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil
	}
	hits, err := e.Store.SearchKNN(q.ProjectKey, emb.Name(), vecs[0], VectorCap)
	if err != nil {
		return nil
	}
	var ids []string
	for _, h := range hits {
		knn[h.ID] = float64(h.Cosine)
		ids = append(ids, h.ID)
	}
	return ids
}

func (e Engine) inferredPaths(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	n := len(ids)
	if n > 40 {
		n = 40
	}
	recs, err := e.Store.GetMany(ids[:n])
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, id := range ids[:n] {
		rec, ok := recs[id]
		if !ok {
			continue
		}
		for _, k := range claim.PathKeys(rec.Paths) {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
			if len(out) >= InferPathCap {
				return out, nil
			}
		}
	}
	return out, nil
}

func (e Engine) features(rec claim.Record, q query, fts, knn map[string]float64) scored {
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
	s.recency = typeRecency(rec.Type, ageDays)
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
	if v, ok := knn[rec.ID]; ok && v > 0 {
		if v > 1 {
			v = 1
		}
		s.vector = v
	}
	nAgree := 0
	if s.isFTS {
		nAgree++
	}
	if s.path > 0 {
		nAgree++
	}
	if s.symbol > 0 {
		nAgree++
	}
	s.agree = float64(nAgree) / 3
	overlapTokens := append(append([]string{}, q.QuestionTokens...), q.GoalTokens...)
	hits := contentOverlap(overlapTokens, jobOverlapText(rec.Text))
	strong := s.path > 0 || s.symbol > 0 || hits >= OverlapStrongMin || s.vector >= VectorGate
	weak := !strong && hits >= 1
	switch rec.Type {
	case "failed":
		if strong {
			s.failedOverlap = 1
		} else if weak {
			s.failedWeak = 1
		}
	case "decision", "state", "constraint":
		if strong {
			s.shippedOverlap = 1
		} else if weak {
			s.shippedWeak = 1
		}
	}
	callerToks := append(append([]string{}, q.QuestionTokens...), q.GoalTokens...)
	onTopic := s.path > 0 || s.symbol > 0 || contentOverlap(callerToks, rec.Text) > 0
	if q.Dwell[rec.ID] && (q.Continue || onTopic) {
		s.dwell = 1
	}
	if q.Served[rec.ID] && s.dwell == 0 && s.failedOverlap == 0 && s.shippedOverlap == 0 {
		s.served = 1
	}
	if len(q.PathKeys) > 0 && s.path == 0 {
		s.oon = 1
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
	root := filepath.Clean(workspace)
	for _, p := range rec.Paths {
		stored, ok := rec.PathMtime[p]
		if !ok {
			continue
		}
		rel := filepath.Clean(p)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		full := filepath.Join(root, rel)
		if !strings.HasPrefix(full, root+string(filepath.Separator)) && full != root {
			continue
		}
		fi, err := os.Stat(full)
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

func typeRecency(typ string, ageDays float64) float64 {
	var hl float64
	switch typ {
	case "constraint":
		return 1
	case "decision":
		hl = DecisionHalfLifeDays
	case "state", "thread":
		hl = StateHalfLifeDays
	default:
		hl = FailedHalfLifeDays
	}
	if hl <= 0 {
		return 1
	}
	return math.Pow(0.5, ageDays/hl)
}

func pathCluster(paths []string) []string {
	var out []string
	for _, p := range claim.PathKeys(paths) {
		base := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			base = p[i+1:]
		}
		if base != "" {
			out = append(out, strings.ToLower(base))
		}
	}
	return out
}

func sharePathCluster(a, b scored) bool {
	as := map[string]bool{}
	for _, p := range pathCluster(a.rec.Paths) {
		as[p] = true
	}
	if len(as) == 0 {
		return false
	}
	for _, p := range pathCluster(b.rec.Paths) {
		if as[p] {
			return true
		}
	}
	return false
}

func coverSim(a, b scored, head bool) float64 {
	sameType := a.rec.Type == b.rec.Type
	if sameType && (head || sharePathCluster(a, b)) {
		return 1
	}
	return jaccard(claim.Tokens(a.rec.Text), claim.Tokens(b.rec.Text))
}
