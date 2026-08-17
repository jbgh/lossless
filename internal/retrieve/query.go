package retrieve

import (
	"regexp"
	"strings"
	"unicode"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
)

type Request struct {
	Question      string   `json:"question,omitempty"`
	Project       string   `json:"project,omitempty"`
	WorkspaceRoot string   `json:"workspace_root,omitempty"`
	Goal          string   `json:"goal,omitempty"`
	Paths         []string `json:"paths,omitempty"`
	SessionID     string   `json:"session_id,omitempty"`
	LimitTokens   int      `json:"limit_tokens,omitempty"`
}

type Hit struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	When    string   `json:"when"`
	Paths   []string `json:"paths"`
	Harness string   `json:"harness"`
	Status  string   `json:"status"`
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
	SessionID      string
	Served         map[string]bool
	Dwell          map[string]bool
	Warned         map[string]bool
	Continue       bool
	Cold           bool
	Head           bool
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

func normalize(req Request) (query, error) {
	project, err := resolveProject(req)
	if err != nil {
		return query{}, err
	}
	qtoks := claim.Tokens(req.Question)
	gtoks := claim.Tokens(req.Goal)
	paths := claim.PathKeys(req.Paths)
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
	lookup := uniq(append(append([]string{}, qtoks...), gtoks...))
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
		SessionID:      strings.TrimSpace(req.SessionID),
		Served:         map[string]bool{},
		Dwell:          map[string]bool{},
		Warned:         map[string]bool{},
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

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	return out
}

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
	out := b.String()
	for _, w := range jobOverlapStop {
		out = strings.ReplaceAll(strings.ToLower(out), w, " ")
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
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "|") || strings.Contains(t, " | ") {
		return true
	}
	if strings.HasPrefix(t, "**") && !strings.Contains(t, ".") {
		return true
	}
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || numberedItem.MatchString(t) {
		rest := t
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			rest = strings.TrimSpace(t[2:])
		} else {
			rest = strings.TrimSpace(numberedItem.ReplaceAllString(t, ""))
		}
		if strings.Contains(t, "**") || strings.HasPrefix(rest, "`") || len(t) < 80 {
			return true
		}
	}
	low := strings.ToLower(t)
	if strings.Contains(low, "failed-overlap") || strings.Contains(low, "classified as") || strings.Contains(low, "failure mode") {
		return true
	}
	if rec.Type == "state" && (strings.HasPrefix(low, "next i ") || strings.HasPrefix(low, "next i'") || strings.HasPrefix(low, "next i’ll") || strings.HasPrefix(low, "next i'll")) {
		return true
	}
	if rec.Type == "decision" && (planningDecision(low) || quotedAttribution(rec.Text) || narrativeDecision(low)) {
		return true
	}
	if strings.HasPrefix(low, "remembered:") || strings.HasPrefix(low, "remembered ") {
		return true
	}
	if rec.Type == "constraint" && (sessionOpConstraint(low) || agentPromptConstraint(low)) {
		return true
	}
	if rec.Type == "failed" && (statusFailed(low) || failedAsObject(low) || (len(rec.Paths) == 0 && failedOnlyInTicks(t))) {
		return true
	}
	if rec.Type == "state" && processState(low) {
		return true
	}
	if truncatedClaim(t) {
		return true
	}
	low2 := strings.ToLower(strings.TrimSpace(t))
	low2 = strings.TrimPrefix(low2, "- ")
	for _, p := range []string{"text: ", "text = ", "text=", "type: failed", "type: decision", "warnings:", "context:"} {
		if strings.HasPrefix(low2, p) {
			return true
		}
	}
	return false
}

func statusFailed(low string) bool {
	for _, n := range []string{
		"ci unit-test", "unit-test failure", "unit test failure",
		"background notification", "checking #", "pr #", "pr-size-check",
		"which of those", "re-pushing", "exit 0",
		"github actions", "actions workflow", "actions job",
	} {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func failedAsObject(low string) bool {
	return strings.Contains(low, "failed items") || strings.Contains(low, "re-queues failed") ||
		strings.Contains(low, "pre-failed skip") || strings.Contains(low, "failure reason") ||
		strings.Contains(low, "retryable failure")
}

func processState(low string) bool {
	return strings.Contains(low, "in this session") || strings.Contains(low, "the next stop") ||
		strings.Contains(low, "next test that matters") || strings.Contains(low, "not another fixture") ||
		strings.Contains(low, "that row is always there") || strings.Contains(low, "i'll inspect") ||
		strings.Contains(low, "i’ll inspect")
}

func truncatedClaim(t string) bool {
	s := strings.TrimSpace(t)
	if strings.HasSuffix(s, "(") || strings.HasSuffix(s, "`." ) || strings.HasSuffix(s, "do not") {
		return true
	}
	if strings.HasSuffix(s, "path (`." ) || strings.Contains(s, "path (`." ) {
		return true
	}
	return false
}

func planningDecision(low string) bool {
	for _, p := range []string{
		"i'll check", "i’ll check", "i will check", "i'll look", "i’ll look",
		"i'll read", "i’ll read", "i'll audit", "i’ll audit",
		"i'll fix", "i’ll fix", "i'll start", "i’ll start", "i'll add", "i’ll add",
		"i'll inspect", "i’ll inspect", "i'll pull", "i’ll pull",
		"i'll go with", "i’ll go with", "i will go with",
		"let's go with", "lets go with", "i'll switch", "i’ll switch",
		"i'll try", "i’ll try",
		"i'll implement", "i’ll implement", "i will implement",
		"i'll replace", "i’ll replace", "i'll swap", "i’ll swap",
		"i'll rewrite", "i’ll rewrite", "i will rewrite",
		"i'll migrate", "i’ll migrate", "i'll refactor", "i’ll refactor",
		"the next hour", "we will use the next", "we'll use the next",
	} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func sessionOpConstraint(low string) bool {
	for _, p := range []string{
		"don't ask", "do not ask", "don't change source", "don't delete data",
		"do not open a pr", "do not redo", "don't flag", "do not start",
		"never mind", "don't push yet", "do not push yet",
		"don't merge yet", "do not merge yet",
		"don't commit yet", "do not commit yet",
		"don't wait", "do not wait",
		"don't have time", "do not have time", "we don't have time", "we do not have time",
		"must be a bug", "must be a typo",
	} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func narrativeDecision(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "chose the wrong") || strings.Contains(low, "picked the wrong") ||
		strings.Contains(low, "wrong approach") || strings.Contains(low, "chose poorly") ||
		strings.Contains(low, "picked poorly") || strings.Contains(low, "almost picked") ||
		strings.Contains(low, "almost chose") || strings.Contains(low, "almost going")
}

func quotedAttribution(s string) bool {
	low := strings.ToLower(s)
	if !strings.Contains(low, "said") {
		return false
	}
	return strings.Contains(low, "said:") || strings.Contains(s, `"`) || strings.Contains(s, "“") || strings.Contains(s, "”")
}

func agentPromptConstraint(low string) bool {
	return strings.Contains(low, "why don't you") || strings.Contains(low, "why do not you") ||
		strings.Contains(low, "why dont you") || strings.HasPrefix(low, "can you ") ||
		strings.HasPrefix(low, "could you ")
}

var (
	failedWord   = regexp.MustCompile(`(?i)\bfailed\b`)
	numberedItem = regexp.MustCompile(`^\d+[.)]\s+`)
)

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

// Function words. Topic nouns (rate, jwt, redis) stay content.
var overlapStop = map[string]bool{
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
