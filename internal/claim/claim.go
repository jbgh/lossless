package claim

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type Record struct {
	ID            string           `json:"id"`
	Type          string           `json:"type"`
	ProjectKey    string           `json:"project_key"`
	WorkspaceRoot string           `json:"workspace_root,omitempty"`
	Harness       string           `json:"harness"`
	SessionID     string           `json:"session_id"`
	CreatedAt     string           `json:"created_at"`
	Text          string           `json:"text"`
	Why           string           `json:"why,omitempty"`
	Paths         []string         `json:"paths"`
	Symbols       []string         `json:"symbols"`
	PathMtime     map[string]int64 `json:"path_mtime"`
	Status        string           `json:"status"`
	Supersedes    string           `json:"supersedes,omitempty"`
	Source        string           `json:"source"`
	ClaimHash     string           `json:"claim_hash"`
	TranscriptRef *TranscriptRef   `json:"transcript_ref,omitempty"`
}

type TranscriptRef struct {
	SessionID   string `json:"session_id"`
	StartOffset int64  `json:"start_offset"`
	EndOffset   int64  `json:"end_offset"`
}

func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return strings.ToUpper(time.Now().UTC().Format("20060102150405")) + hex.EncodeToString(b[:])
}

func Hash(projectKey, typ, text string) string {
	norm := normalize(text)
	sum := sha256.Sum256([]byte(projectKey + "\x00" + typ + "\x00" + norm))
	return hex.EncodeToString(sum[:])
}

// FixtureSession is a bench/eval tape imported into a live project.
// Those records stay in raw; they must not win ask.
func FixtureSession(id string) bool {
	switch strings.TrimSpace(id) {
	case "grok-auth", "claude-jwt", "grok-billing", "grok-css-noise",
		"grok-error-handling", "grok-hedge", "grok-long-tail",
		"grok-postgres", "grok-redis-retry", "grok-secret",
		"grok-two-fails", "grok-webshop", "claude-pyapi",
		"sess1", "csess":
		return true
	default:
		return false
	}
}

// Tokens returns unicode word tokens, lowercased, length > 1.
func Tokens(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		t := strings.ToLower(b.String())
		b.Reset()
		if len([]rune(t)) > 1 {
			out = append(out, t)
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			flush()
		}
	}
	if b.Len() > 0 {
		flush()
	}
	return out
}

// Uniq keeps first-seen non-empty strings.
func Uniq(xs []string) []string {
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

// PathKeys is each path as given plus its basename.
func PathKeys(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, p := range paths {
		add(p)
		add(baseName(p))
	}
	return out
}

func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Stem is the basename without a file extension.
func Stem(p string) string {
	b := baseName(p)
	if i := strings.LastIndex(b, "."); i > 0 {
		return b[:i]
	}
	return b
}

// IsIdentifier reports the retrieval identifier heuristic.
func IsIdentifier(tok string) bool {
	if identRE.MatchString(tok) {
		return true
	}
	if (strings.Contains(tok, "/") || strings.Contains(tok, ".")) && extRE.MatchString(tok) {
		return true
	}
	return false
}

var (
	identRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{2,}$`)
	identFind = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}`)
	extRE     = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]*$`)
)

// FoldIdent drops _ and - and lowercases so tokenBucket and token_bucket match.
func FoldIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// CodeShaped reports a token that reads as code, not prose: an internal
// capital (camelCase), a digit, a . / _ separator, a hyphen with a
// leading capital, or a known package alias. Sentence case ("Redis") and
// hyphenated English ("re-proposing") are prose; bare acronyms are the
// caller's call.
func CodeShaped(s string) bool {
	if HasAlias(s) {
		return true
	}
	hasInnerUpper, hasLower := false, false
	for i, r := range s {
		switch {
		case r == '_' || r == '.' || r == '/':
			return true
		case r == '-':
			if hyphenIdent(s) {
				return true
			}
		case unicode.IsDigit(r):
			return true
		case unicode.IsUpper(r):
			if i > 0 {
				hasInnerUpper = true
			}
		case unicode.IsLower(r):
			hasLower = true
		}
	}
	return hasInnerUpper && hasLower
}

// hyphenIdent: Re-Check (leading capital) or kebab identifiers like
// react-query and x-api-key. Hyphenated English ("re-proposing",
// "so-called") starts with a standard prefix or particle — a closed
// grammatical set, so it can stay a list without growing per repo.
func hyphenIdent(s string) bool {
	if s[0] >= 'A' && s[0] <= 'Z' {
		return true
	}
	head, _, ok := strings.Cut(s, "-")
	if !ok {
		return false
	}
	switch strings.ToLower(head) {
	case "re", "un", "non", "pre", "co", "de", "anti", "self", "well",
		"semi", "multi", "over", "under", "cross", "half", "so", "mid",
		"out", "off", "all", "ever", "long", "short", "one", "two":
		return false
	}
	return true
}

// ExplicitMemory reports a claim source that was deliberate — remember,
// a manual import, or unknown provenance. Read-time noise gates apply
// only to automatic transcript extraction, never to these.
func ExplicitMemory(source string) bool {
	switch source {
	case "remember", "import", "":
		return true
	}
	return false
}

// HasAlias reports whether ExpandIdent knows a package alias for s.
// Those tokens are code identifiers even when they read as plain words.
func HasAlias(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "jwt", "jsonwebtoken":
		return true
	}
	return false
}

// ExpandIdent returns the token plus coding-identifier aliases.
// jsonwebtoken ↔ jwt is the same package, not an English synonym list.
func ExpandIdent(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := []string{s}
	if f := FoldIdent(s); f != "" && !strings.EqualFold(f, s) {
		out = append(out, f)
	}
	switch strings.ToLower(s) {
	case "jsonwebtoken":
		out = append(out, "jwt")
	case "jwt":
		out = append(out, "jsonwebtoken")
	}
	return out
}

// ExtractSymbols pulls identifiers from claim text and path stems.
func ExtractSymbols(text string, paths []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, m := range identFind.FindAllString(text, -1) {
		for _, a := range ExpandIdent(m) {
			add(a)
		}
	}
	for _, p := range paths {
		add(baseName(p))
		if st := Stem(p); st != "" {
			add(st)
		}
	}
	return out
}

func normalize(text string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}
