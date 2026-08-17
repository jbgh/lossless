package retrieve

import (
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func (e Engine) hydrateActions(req Request, q query) query {
	if e.Store == nil {
		return q
	}
	q.SessionID = e.resolveSession(req, q)
	if q.Served == nil {
		q.Served = map[string]bool{}
	}
	if q.Dwell == nil {
		q.Dwell = map[string]bool{}
	}
	if q.Warned == nil {
		q.Warned = map[string]bool{}
	}
	acts, err := e.Store.RecentActions(q.ProjectKey, q.SessionID, ActionTapeCap)
	if err != nil || len(acts) == 0 {
		return q
	}

	fillPaths := len(q.PathKeys) == 0
	var extraToks []string
	var extraPaths []string
	var lastAskAt string
	var lastAskToks []string
	var lastAskPaths []string
	for _, a := range acts {
		switch a.Kind {
		case store.ActionAsk:
			if lastAskAt == "" {
				lastAskAt = a.At
				lastAskToks = a.Tokens
			}
			if a.At == lastAskAt {
				if a.ClaimID != "" {
					q.Served[a.ClaimID] = true
				}
				lastAskPaths = append(lastAskPaths, a.Paths...)
			}
		case store.ActionGet, store.ActionRemember:
			if a.ClaimID != "" {
				q.Dwell[a.ClaimID] = true
			}
		case store.ActionWarn:
			if a.ClaimID != "" {
				q.Warned[a.ClaimID] = true
			}
		}
	}

	// Thin continue / same question: mix last-ask tokens and dwell text.
	// A rich ask on a new file must not inherit Redis symbols from a GET.
	useTapeRecall := continueTape(q, lastAskToks, lastAskPaths)
	if useTapeRecall {
		for _, a := range acts {
			switch a.Kind {
			case store.ActionAsk:
				extraToks = append(extraToks, a.Tokens...)
				if fillPaths {
					extraPaths = append(extraPaths, a.Paths...)
				}
			case store.ActionGet, store.ActionRemember:
				if fillPaths {
					extraPaths = append(extraPaths, a.Paths...)
				}
			}
		}
	}

	if fillPaths {
		for _, p := range extraPaths {
			q.PathKeys = append(q.PathKeys, claim.PathKeys([]string{p})...)
		}
		q.PathKeys = uniq(q.PathKeys)
		if len(q.PathKeys) > CompilePathCap {
			q.PathKeys = q.PathKeys[:CompilePathCap]
		}
	}

	if useTapeRecall {
		if ids := dwellOrWarned(q); len(ids) > 0 {
			if recs, err := e.Store.GetMany(ids); err == nil {
				for _, rec := range recs {
					extraToks = append(extraToks, claim.Tokens(rec.Text)...)
				}
			}
		}
	}

	for _, t := range extraToks {
		if isContentToken(t) {
			q.LookupTokens = append(q.LookupTokens, t)
		}
	}
	q.LookupTokens = uniq(q.LookupTokens)
	q.Symbols = addSymbols(q.Symbols, extraToks)
	q.Continue = useTapeRecall
	cur := uniq(append(append([]string{}, q.QuestionTokens...), q.GoalTokens...))
	if sameTokenSet(cur, lastAskToks) {
		q.Served = map[string]bool{}
	}
	q.Head = isHead(q)
	q.Cold = len(q.QuestionTokens) == 0 && len(q.GoalTokens) == 0
	return q
}

// continueTape is true when this ask is still the same work: same
// question, or a thin/anaphoric follow-up, or the same files.
func continueTape(q query, lastToks, lastPaths []string) bool {
	cur := uniq(append(append([]string{}, q.QuestionTokens...), q.GoalTokens...))
	if sameTokenSet(cur, lastToks) {
		return true
	}
	if len(q.PathKeys) == 0 && !hasStrongIdent(q) {
		return true
	}
	if len(q.PathKeys) > 0 && len(lastPaths) > 0 {
		return jaccard(q.PathKeys, claim.PathKeys(lastPaths)) > 0
	}
	return false
}

func sameTokenSet(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	as := map[string]bool{}
	for _, x := range a {
		as[x] = true
	}
	bs := map[string]bool{}
	for _, x := range b {
		bs[x] = true
	}
	if len(as) != len(bs) {
		return false
	}
	for x := range as {
		if !bs[x] {
			return false
		}
	}
	return true
}

func dwellOrWarned(q query) []string {
	var ids []string
	for id := range q.Dwell {
		ids = append(ids, id)
	}
	return ids
}

func addSymbols(dst, toks []string) []string {
	seen := map[string]bool{}
	for _, s := range dst {
		seen[s] = true
	}
	for _, raw := range toks {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" || seen[s] || identStop[s] {
			continue
		}
		if !identLower(s) && !strings.ContainsAny(s, "/.") {
			continue
		}
		seen[s] = true
		dst = append(dst, s)
		for _, extra := range claim.ExpandIdent(s) {
			e := strings.ToLower(extra)
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			dst = append(dst, e)
		}
	}
	return dst
}

func (e Engine) resolveSession(req Request, q query) string {
	if s := strings.TrimSpace(req.SessionID); s != "" {
		return s
	}
	if q.SessionID != "" {
		return q.SessionID
	}
	// Never borrow another harness's tape. Claims are project-shared;
	// served/dwell stay on this session or the project default.
	return "default"
}

func (e Engine) recordAsk(req Request, q query, seedPaths []string, out Response) {
	if e.Store == nil || q.ProjectKey == "" {
		return
	}
	sess := q.SessionID
	if sess == "" {
		sess = e.resolveSession(req, q)
	}
	at := e.now().UTC().Format(time.RFC3339)
	toks := uniq(append(append([]string{}, q.QuestionTokens...), q.GoalTokens...))
	var acts []store.Action
	if len(out.Context) == 0 {
		acts = append(acts, store.Action{
			ProjectKey: q.ProjectKey, SessionID: sess, Kind: store.ActionAsk,
			Paths: seedPaths, Tokens: toks, At: at,
		})
	}
	for _, h := range out.Context {
		acts = append(acts, store.Action{
			ProjectKey: q.ProjectKey, SessionID: sess, Kind: store.ActionAsk,
			ClaimID: h.ID, Paths: seedPaths, Tokens: toks, At: at,
		})
	}
	for _, w := range out.Warnings {
		if id := warnClaimID(w); id != "" {
			acts = append(acts, store.Action{
				ProjectKey: q.ProjectKey, SessionID: sess, Kind: store.ActionWarn,
				ClaimID: id, At: at,
			})
		}
	}
	_ = e.Store.AppendActions(acts)
}

func warnClaimID(w string) string {
	i := strings.Index(w, "(see ")
	if i < 0 {
		return ""
	}
	rest := w[i+5:]
	j := strings.IndexByte(rest, ')')
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}
