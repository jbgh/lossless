package retrieve

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	"lossless/internal/claim"
	"lossless/internal/gate"
	"lossless/internal/projectkey"
	"lossless/internal/redact"
	"lossless/internal/write"
)

type Request struct {
	Question      string   `json:"question,omitempty"`
	Project       string   `json:"project,omitempty"`
	WorkspaceRoot string   `json:"workspace_root,omitempty"`
	Goal          string   `json:"goal,omitempty"`
	Paths         []string `json:"paths,omitempty"`
	SessionID     string   `json:"session_id,omitempty"`
	LimitTokens   int      `json:"limit_tokens,omitempty"`
	// skipCatchUp is set by compact checkout. Catch-up already ran;
	// the live harness file may be shrinking. JSON/MCP cannot set this.
	skipCatchUp bool
}

type Hit struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Text       string   `json:"text"`
	When       string   `json:"when"`
	Paths      []string `json:"paths"`
	Harness    string   `json:"harness"`
	Status     string   `json:"status"`
	Source     string   `json:"source,omitempty"`
	HasExcerpt bool     `json:"has_excerpt"`
}

type Response struct {
	Context  []Hit    `json:"context"`
	Warnings []string `json:"warnings"`
	Tokens   int      `json:"tokens"`
	Project  string   `json:"project"`
}

type query struct {
	ProjectKey     string
	QuestionTokens []string
	GoalTokens     []string
	LookupTokens   []string
	PathKeys       []string
	Symbols        []string
	OwnSymbols     []string // this ask's own symbols; hydrate adds inherited ones to Symbols only
	ContentTokens  int
	OverlapTokens  []string // distinct content tokens of this ask, minus the project's own name
	Targeted       bool     // one content token and an interrogative: a lookup, not a task
	SessionID      string
	Served         map[string]bool
	Dwell          map[string]bool
	Continue       bool
	Cold           bool
	Head           bool
	CompactAt      time.Time // the asking session's last compact; zero if none known
	WorkspaceRoot  string
	LimitTokens    int
}

func resolveProject(req Request) (string, error) {
	if strings.TrimSpace(req.Project) != "" {
		return projectkey.Normalize(req.Project), nil
	}
	if strings.TrimSpace(req.WorkspaceRoot) != "" {
		return projectkey.FromWorkspace(req.WorkspaceRoot), nil
	}
	return "", errBadRequest
}

// CleanSessionID drops sentinels children send instead of omitting the field.
// "default" must not exact-locate or bind a fake action tape id.
func CleanSessionID(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "default", "none", "null", "undefined":
		return ""
	}
	return s
}

func normalize(req Request) (query, error) {
	project, err := resolveProject(req)
	if err != nil {
		return query{}, err
	}
	qtoks := claim.Tokens(req.Question)
	gtoks := claim.Tokens(req.Goal)
	paths := claim.PathKeys(redact.FilterPaths(req.Paths))
	var symbols []string
	seen := map[string]bool{}
	addSym := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] || identStop[s] {
			return
		}
		if !identLower(s) && !strings.ContainsAny(s, "/.") {
			return
		}
		seen[s] = true
		symbols = append(symbols, s)
		for _, extra := range claim.ExpandIdent(s) {
			e := strings.ToLower(extra)
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			symbols = append(symbols, e)
		}
	}
	for _, t := range append(append([]string{}, qtoks...), gtoks...) {
		addSym(t)
	}
	lookup := claim.Uniq(append(append([]string{}, qtoks...), gtoks...))
	limit := req.LimitTokens
	if limit <= 0 {
		limit = DefaultLimit
	}
	q := query{
		ProjectKey:     project,
		QuestionTokens: qtoks,
		GoalTokens:     gtoks,
		LookupTokens:   lookup,
		PathKeys:       paths,
		Symbols:        symbols,
		OwnSymbols:     append([]string(nil), symbols...),
		SessionID:      CleanSessionID(req.SessionID),
		Served:         map[string]bool{},
		Dwell:          map[string]bool{},
		Cold:           len(qtoks) == 0 && len(gtoks) == 0,
		WorkspaceRoot:  req.WorkspaceRoot,
		LimitTokens:    limit,
	}
	q.Head = isHead(q)
	return q, nil
}

func rich(q query) bool {
	return len(q.PathKeys) > 0 || len(q.QuestionTokens)+len(q.GoalTokens) >= RichTokenMin
}

func isHead(q query) bool {
	return len(q.PathKeys) == 0 && len(q.Symbols) == 0 && len(q.LookupTokens) == 0
}

// sharedCodeIdent is the "shared identifier" strong-overlap trigger
// (docs/algorithm.md §7). Extract stores every word of a claim as a
// symbol, so a bare intersection is "any shared word" — weak. Strong
// needs the shared symbol to be code-shaped, or the ask to be a
// targeted lookup: one content token plus an interrogative ("why not
// jose"). "add tests" is a task whose verb is a stop word, not a lookup.
func sharedCodeIdent(qSym, recSyms []string, targeted bool) bool {
	if len(qSym) == 0 || len(recSyms) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, s := range qSym {
		set[strings.ToLower(s)] = true
	}
	for _, s := range recSyms {
		if !set[strings.ToLower(s)] {
			continue
		}
		if targeted || codeShaped(s) {
			return true
		}
	}
	return false
}

var interrogative = map[string]bool{
	"why": true, "what": true, "which": true, "how": true, "should": true,
	"did": true, "does": true, "is": true, "are": true, "can": true,
	"could": true, "would": true, "where": true, "when": true, "who": true,
}

func hasInterrogative(qtoks, gtoks []string) bool {
	for _, t := range append(append([]string{}, qtoks...), gtoks...) {
		if interrogative[strings.ToLower(t)] {
			return true
		}
	}
	return false
}

// sharedCount is the size of the symbol intersection.
func sharedCount(a, b []string) int {
	set := map[string]bool{}
	for _, s := range a {
		set[strings.ToLower(s)] = true
	}
	n := 0
	seen := map[string]bool{}
	for _, s := range b {
		l := strings.ToLower(s)
		if set[l] && !seen[l] {
			seen[l] = true
			n++
		}
	}
	return n
}

// codeShaped is claim.CodeShaped: the one home for "reads as code".
func codeShaped(s string) bool {
	return claim.CodeShaped(s)
}

func contentTokenCount(qtoks, gtoks []string) int {
	seen := map[string]bool{}
	for _, t := range append(append([]string{}, qtoks...), gtoks...) {
		t = strings.ToLower(strings.TrimSpace(t))
		if isContentToken(t) {
			seen[t] = true
		}
	}
	return len(seen)
}

func identLower(s string) bool {
	if len(s) < 3 {
		return false
	}
	r := []rune(s)
	if r[0] != '_' && !unicode.IsLetter(r[0]) {
		return false
	}
	for _, c := range r[1:] {
		if c != '_' && !unicode.IsLetter(c) && !unicode.IsNumber(c) {
			return false
		}
	}
	return true
}

func uniq(xs []string) []string { return claim.Uniq(xs) }

func ftsMatch(tokens []string) string {
	var parts []string
	for _, t := range tokens {
		t = strings.Map(func(r rune) rune {
			if r == '"' {
				return -1
			}
			return r
		}, t)
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		parts = append(parts, `"`+t+`"`)
	}
	return strings.Join(parts, " OR ")
}

// ftsStemMatch is ftsMatch as prefix queries on stems, so the lexical
// route finds "languages" for "language". A stem under four letters is
// too short to be a prefix ("set*" is half the dictionary), so that word
// is asked for as itself, as its stem, as the stem's plural, and by its
// own first four letters: the forms stemOverlap accepts (sameWord). The
// floor judges the match.
func ftsStemMatch(tokens []string) string {
	var parts []string
	seen := map[string]bool{}
	add := func(q string) {
		if !seen[q] {
			seen[q] = true
			parts = append(parts, q)
		}
	}
	for _, t := range tokens {
		t = strings.ToLower(strings.Map(func(r rune) rune {
			if r == '"' {
				return -1
			}
			return r
		}, strings.TrimSpace(t)))
		if t == "" {
			continue
		}
		st := stem(t)
		if len(st) >= 4 {
			add(`"` + st + `"*`)
			continue
		}
		add(`"` + t + `"`)
		if st != "" && st != t {
			add(`"` + st + `"`)
			add(`"` + st + `s"`)
		}
		if len(t) > 4 {
			add(`"` + t[:4] + `"*`)
		}
	}
	return strings.Join(parts, " OR ")
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	as := map[string]bool{}
	for _, x := range a {
		if x != "" {
			as[x] = true
		}
	}
	bs := map[string]bool{}
	for _, x := range b {
		if x != "" {
			bs[x] = true
		}
	}
	if len(as) == 0 || len(bs) == 0 {
		return 0
	}
	inter := 0
	for x := range as {
		if bs[x] {
			inter++
		}
	}
	union := len(as) + len(bs) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenOverlap(queryTokens []string, text string) bool {
	return contentOverlap(queryTokens, text) > 0
}

// contentOverlap counts distinct query content tokens that hit the claim
// text (or an ExpandIdent alias). Function words do not count.
func contentOverlap(queryTokens []string, text string) int {
	hay := map[string]bool{}
	addHay := func(t string) {
		t = strings.ToLower(strings.TrimSpace(t))
		if !isContentToken(t) {
			return
		}
		hay[t] = true
	}
	for _, t := range claim.Tokens(text) {
		addHay(t)
		for _, e := range claim.ExpandIdent(t) {
			addHay(e)
		}
	}
	seen := map[string]bool{}
	n := 0
	for _, t := range queryTokens {
		t = strings.ToLower(strings.TrimSpace(t))
		if !isContentToken(t) || seen[t] {
			continue
		}
		hit := hay[t]
		if !hit {
			for _, e := range claim.ExpandIdent(t) {
				if hay[strings.ToLower(e)] {
					hit = true
					break
				}
			}
		}
		if hit {
			seen[t] = true
			n++
		}
	}
	return n
}

// stem folds the endings that separate a goal's word from the same word
// in a sentence someone typed: plurals, -ing, -ed, a final e. It is not
// linguistics ("speed" gives "spe"). What matters is that every form of a
// word lands on one stem: "embeddings", "embedding", "embedded" and
// "embed" all give "emb".
func stem(t string) string {
	t = strings.ToLower(t)
	switch {
	case len(t) > 4 && strings.HasSuffix(t, "ies"):
		t = t[:len(t)-3] + "y"
	case len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss"):
		t = t[:len(t)-1]
	}
	for i := 0; i < 3; i++ {
		cut := 0
		switch {
		case len(t) > 5 && strings.HasSuffix(t, "ing"):
			cut = 3
		case len(t) > 4 && strings.HasSuffix(t, "ed"):
			cut = 2
		}
		if cut == 0 {
			break
		}
		t = t[:len(t)-cut]
		// "embedd" from embedded, "runn" from running
		if n := len(t); n > 3 && t[n-1] == t[n-2] && !strings.ContainsRune("aeiouls", rune(t[n-1])) {
			t = t[:n-1]
		}
	}
	if len(t) > 3 && strings.HasSuffix(t, "e") {
		t = t[:len(t)-1]
	}
	return t
}

// stemOverlap is contentOverlap on stems, with adjacent words of the text
// also read joined ("back ups" meets "backups"). Two words that share a
// stem must also share their first four letters, or all of the shorter
// one: "cors" and "core" both stem to "cor" and are not the same word.
func stemOverlap(queryTokens []string, text string) int {
	words := map[string][]string{} // stem -> the words that gave it
	add := func(w string) {
		st := stem(w)
		words[st] = append(words[st], w)
	}
	prev := ""
	for _, t := range claim.Tokens(topicText(text)) {
		t = strings.ToLower(t)
		if isContentToken(t) {
			add(t)
		}
		if prev != "" {
			add(prev + t)
		}
		prev = t
	}
	seen := map[string]bool{}
	n := 0
	for _, t := range queryTokens {
		t = strings.ToLower(strings.TrimSpace(t))
		if !isContentToken(t) {
			continue
		}
		st := stem(t)
		if seen[st] {
			continue
		}
		for _, w := range words[st] {
			if sameWord(t, w) {
				seen[st] = true
				n++
				break
			}
		}
	}
	return n
}

func sameWord(a, b string) bool {
	need := 4
	if len(a) < need {
		need = len(a)
	}
	if len(b) < need {
		need = len(b)
	}
	return a[:need] == b[:need]
}

// jobOverlapText strips claim-type jargon so "classified as failed"
// does not count as a job-1 overlap with an ask about retrieve.
func jobOverlapText(s string) string {
	var b strings.Builder
	inTick := false
	for _, r := range s {
		if r == '`' {
			inTick = !inTick
			b.WriteByte(' ')
			continue
		}
		if !inTick {
			b.WriteRune(r)
		}
	}
	return topicText(b.String())
}

// topicText drops the claim-type jargon and keeps code spans: a shared
// `identifier` is the most specific word a record has.
func topicText(s string) string {
	out := strings.ToLower(s)
	for _, w := range jobOverlapStop {
		out = strings.ReplaceAll(out, w, " ")
	}
	return out
}

var jobOverlapStop = []string{
	"failed-overlap", "shipped-overlap", "type-cap", "packtypecap",
	"classified as", "extract noise", "ask pack",
	"failed", "failure",
}

func extractNoise(rec claim.Record) bool {
	t := strings.TrimSpace(rec.Text)
	if t == "" {
		return true
	}
	// remember is deliberate memory: no shape gate applies, whatever the
	// sentence looks like. (Whether it is about this ask is the relevance
	// floor's question, belowFloor, and that one is asked of every source.) import and empty-source rows (bench seeds,
	// fixtures, legacy) keep the shape gates and only skip the grounding
	// gates below (claim.ExplicitMemory).
	if rec.Source == "remember" {
		return false
	}
	// A said record is a user turn kept whole: the sentence shapes below
	// judge extracted claims, not this.
	if rec.Type == "said" {
		// The writer's own rule, asked again: a row an older version
		// stored with markup in it is not known to be the user's words.
		return gate.MachineTurn(t) || write.SaidMarkedUp(t)
	}
	if gate.SkipProse(t) || gate.ListChrome(t, true) {
		return true
	}
	// A stored line of source or of a diff (rows from before extract
	// skipped them) is nobody's rule, decision, or failure.
	// A workflow finding stores as a failure whatever its shape, and one
	// can quote a statement or read like a call: for failures only the
	// start-of-line shapes (a diff marker, a comment, a JSON member) count.
	if rec.Type == "failed" {
		if gate.CodeLead(t) {
			return true
		}
	} else if gate.CodeLine(t) || gate.CodeStatement(t) {
		return true
	}
	// Constraints are user-typed by construction; an arrow there is
	// memory, not a diagram.
	if rec.Type != "constraint" && !claim.ExplicitMemory(rec.Source) && gate.ArrowChrome(t) && len(rec.Paths) == 0 &&
		!strings.Contains(t, "`") && !strings.Contains(t, "**") {
		return true
	}
	switch rec.Type {
	case "state":
		if gate.NextI(t) || gate.ProcessState(t) {
			return true
		}
	case "decision":
		if gate.Planning(t) || gate.QuotedAttribution(t) || gate.NarrativeDecision(t) || write.HypotheticalDecision(t) {
			return true
		}
		// Legacy turn-extracted decisions with no referent. Explicit
		// memory (remember, import, unknown provenance) stays whatever
		// its shape.
		// Attached paths are trusted: the extractor only stores a
		// decision it grounded (own anchors or a neighbor sentence's
		// path). Legacy rows that inherited a nearby user path are the
		// residual; the extractor cannot create new ones.
		if !claim.ExplicitMemory(rec.Source) && !write.GroundedDecision(t, rec.Paths) {
			return true
		}
	case "constraint":
		if gate.SessionOp(t) || gate.AgentPrompt(t) || gate.ConstraintFragment(t) {
			return true
		}
	case "failed":
		if gate.SuccessReport(t) {
			return true
		}
		if gate.StatusFailed(t) || gate.FailedAsObject(t) || (len(rec.Paths) == 0 && failedOnlyInTicks(t)) || write.NotAFailure(t) {
			return true
		}
		if !write.GroundedFailed(t, rec.Paths) {
			return true
		}
	}
	return false
}

var failedWord = regexp.MustCompile(`(?i)\bfailed\b`)

func failedOnlyInTicks(s string) bool {
	if !failedWord.MatchString(s) {
		return false
	}
	var b strings.Builder
	inTick := false
	for _, r := range s {
		if r == '`' {
			inTick = !inTick
			continue
		}
		if !inTick {
			b.WriteRune(r)
		}
	}
	return !failedWord.MatchString(b.String())
}

func isContentToken(t string) bool {
	if t == "" || overlapStop[t] {
		return false
	}
	if len([]rune(t)) > 3 {
		return true
	}
	// jwt, jose — short but they are the identifiers.
	return identLower(t)
}

// overlapNeed is how many shared content words make the word route to a
// strong overlap. Two is a topic match on a short goal; a thirty-word goal
// shares two ordinary words with almost any record, so the bar rises by
// one for every eight content tokens past sixteen.
func overlapNeed(contentTokens int) int {
	need := (contentTokens + 7) / 8
	if need < OverlapStrongMin {
		need = OverlapStrongMin
	}
	return need
}

// topicNeed is how many of the ask's content words a record must share to
// pack on words alone: one for a goal of up to sixteen, one more for every
// sixteen after that (half the strong bar, overlapNeed). A thirty-word goal
// shares one ordinary word with almost any record in the project.
func topicNeed(contentTokens int) int {
	need := (contentTokens + 15) / 16
	if need < 1 {
		need = 1
	}
	return need
}

// belowFloor is the pack's relevance floor: a record that shares no file,
// no identifier, and too few of the goal's words with this ask is filler,
// whatever its type and age score. Five slots are a cap, not a quota.
// A record the agent opened stays while the ask is the same work. An ask
// with no content words of its own (a bare continue, paths only) has no
// vocabulary to judge by, so the floor does not apply. The path hop is
// judged afterwards, across the pool (groundHops).
func belowFloor(q query, ov []string, strong bool, rec claim.Record) bool {
	if len(ov) == 0 || strong {
		return false
	}
	if q.Dwell[rec.ID] && q.Continue {
		return false
	}
	// Stems, so "rate limits" is on topic for a goal about rate limiting.
	// And the names of the record's files: "change pack ordering" is about
	// a decision filed on pack.go whatever words the decision uses.
	text := topicText(rec.Text) + " " + fileNames(rec.Paths)
	return contentOverlap(ov, text) < topicNeed(len(ov)) && stemOverlap(ov, text) < topicNeed(len(ov))
}

// fileNames is the base name of each path without its extension.
func fileNames(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		p = strings.ReplaceAll(p, "\\", "/")
		if i := strings.LastIndex(p, "/"); i >= 0 {
			p = p[i+1:]
		}
		if i := strings.Index(p, "."); i > 0 {
			p = p[:i]
		}
		b.WriteString(p)
		b.WriteByte(' ')
	}
	return b.String()
}

// groundHops lets a record in on the path hop when the record it hopped
// from is itself on topic: "add rate limiting" finds the limiter decision,
// and the Redis failure on that same file shares no word with the ask. A
// file named only by filler is no tie.
func groundHops(cand []scored) {
	seeds := map[string]bool{}
	for _, c := range cand {
		if c.ungrounded {
			continue
		}
		for _, p := range c.rec.Paths {
			if p != "" {
				seeds[p] = true
			}
		}
	}
	if len(seeds) == 0 {
		return
	}
	for i := range cand {
		if !cand[i].ungrounded || cand[i].hop == 0 {
			continue
		}
		// Path keys, as the hop feature compares them.
		for _, p := range claim.PathKeys(cand[i].rec.Paths) {
			if seeds[p] {
				cand[i].ungrounded = false
				break
			}
		}
	}
}

// dropSaidTwins takes a said record out of the pack when a typed record
// packed beside it is a sentence of that same turn. Same session and
// offset is not enough: a later part of a sealed tape counts from zero
// again, so the sentence must also be in the turn's text.
func dropSaidTwins(packed []scored) (kept, dropped []scored) {
	fold := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	for _, c := range packed {
		twin := false
		if c.rec.Type == "said" && c.rec.TranscriptRef != nil {
			turn := fold(c.rec.Text)
			for _, p := range packed {
				r := p.rec.TranscriptRef
				if p.rec.Type == "said" || r == nil {
					continue
				}
				if r.SessionID == c.rec.TranscriptRef.SessionID && r.StartOffset == c.rec.TranscriptRef.StartOffset &&
					strings.Contains(turn, fold(p.rec.Text)) {
					twin = true
					break
				}
			}
		}
		// A one-sentence turn and the record typed from it say the same
		// thing in the same words, whichever turn it was.
		if !twin && c.rec.Type == "said" {
			toks := claim.Tokens(c.rec.Text)
			for _, p := range packed {
				if p.rec.Type != "said" && jaccard(toks, claim.Tokens(p.rec.Text)) >= DiversityJac {
					twin = true
					break
				}
			}
		}
		if twin {
			dropped = append(dropped, c)
		} else {
			kept = append(kept, c)
		}
	}
	return kept, dropped
}

// overlapTokens are the ask's distinct content tokens without the
// project's own name: "lossless" is in every goal and half the records of
// jbgh/lossless, so sharing it says nothing about the topic.
func overlapTokens(q query) []string {
	own := map[string]bool{}
	if !strings.HasPrefix(q.ProjectKey, "path-") {
		for _, t := range claim.Tokens(q.ProjectKey) {
			own[strings.ToLower(t)] = true
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, t := range append(append([]string{}, q.QuestionTokens...), q.GoalTokens...) {
		t = strings.ToLower(strings.TrimSpace(t))
		if !isContentToken(t) || seen[t] || own[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// Function words. Topic nouns (rate, jwt, redis) stay content.
var overlapStop = map[string]bool{
	"after": true, "before": true, "only": true, "every": true, "never": true, "always": true,
	"ever": true, "when": true, "where": true, "while": true, "until": true, "since": true,
	"there": true, "here": true, "their": true, "them": true, "they": true, "these": true,
	"those": true, "some": true, "more": true, "most": true, "each": true, "other": true,
	"such": true, "both": true, "very": true, "same": true, "still": true, "gets": true,
	"look": true, "looks": true, "last": true, "sure": true, "lets": true, "onto": true,
	"again": true, "once": true, "much": true, "many": true, "were": true, "being": true,
	"because": true, "between": true, "without": true, "within": true, "during": true,
	// three-letter function words pass the identifier-length exception below
	"are": true, "was": true, "has": true, "had": true, "can": true, "but": true, "its": true,
	"our": true, "you": true, "all": true, "any": true, "one": true, "two": true, "out": true,
	"now": true, "new": true, "own": true, "off": true, "too": true, "via": true, "per": true,
	"yet": true, "nor": true, "may": true, "did": true, "get": true, "got": true, "let": true,
	"see": true, "say": true, "way": true, "who": true, "inside": true, "outside": true,
	"the": true, "and": true, "for": true, "not": true, "why": true, "how": true,
	"what": true, "which": true, "this": true, "that": true, "with": true, "from": true,
	"into": true, "then": true, "than": true, "use": true, "using": true, "add": true,
	"pick": true, "know": true, "already": true, "about": true, "should": true,
	"would": true, "could": true, "thing": true, "idea": true, "work": true,
	"working": true, "want": true, "need": true, "make": true, "keep": true,
	"over": true, "also": true, "just": true, "like": true, "have": true, "been": true,
	"will": true, "does": true, "doing": true,
	"library": true, "choice": true, "status": true, "please": true,
}

func estimateTokens(s string) int {
	n := (len(s) + 3) / 4
	if n < 1 {
		return 1
	}
	return n
}
