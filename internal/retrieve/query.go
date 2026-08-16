package retrieve

import (
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
		if s == "" || seen[s] {
			return
		}
		if !identLower(s) && !strings.ContainsAny(s, "/.") {
			return
		}
		seen[s] = true
		symbols = append(symbols, s)
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
	hay := map[string]bool{}
	for _, t := range claim.Tokens(text) {
		if len([]rune(t)) > 3 {
			hay[t] = true
		}
	}
	for _, t := range queryTokens {
		if len([]rune(t)) > 3 && hay[t] {
			return true
		}
	}
	return false
}

func estimateTokens(s string) int {
	n := (len(s) + 3) / 4
	if n < 1 {
		return 1
	}
	return n
}
