package write

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/redact"
)

var (
	pathRE       = regexp.MustCompile(`(?:[\w.-]+/)+[\w.-]+\.[A-Za-z0-9]+`)
	failedRE     = regexp.MustCompile(`(?i)\b(we rejected|was rejected|didn't work|did not work|revert|abort|failed|failure|didn't compile|does not work|threw|exception)\b`)
	constraintRE = regexp.MustCompile(`(?i)\b(always|never|don't|do not|must|we use|we don't)\b`)
	hedgeRE      = regexp.MustCompile(`(?i)\b(i don't think|i do not think|not sure|maybe|probably|might|should we|could we|can we|do we)\b`)
	questionRE   = regexp.MustCompile(`(?i)^\s*(should|could|can|may|do|did|is|are|will)\b`)
	stateRE      = regexp.MustCompile(`(?i)\b(working on|current plan|next step|now implementing)\b`)
	decisionRE   = regexp.MustCompile(`(?i)\b(decided|going with|we'll use|we will use|picked \w+ over|chose|instead of)\b`)
)

var priority = map[string]int{"failed": 5, "decision": 4, "constraint": 3, "state": 2, "thread": 1}

type ExtractOpts struct {
	ProjectKey    string
	WorkspaceRoot string
	Harness       string
	SessionID     string
	Source        string
}

func Extract(msgs []Message, opts ExtractOpts) []claim.Record {
	var usable []Message
	for _, m := range msgs {
		if !m.Skip {
			usable = append(usable, m)
		}
	}
	recent := tail(usable, 40, 32000)
	inTail := map[int64]bool{}
	for _, m := range recent {
		inTail[m.Offset] = true
	}
	drafts := []claim.Record{}
	for _, msg := range usable {
		if msg.Role == "tool" {
			continue
		}
		paths := redact.FilterPaths(uniq(append(pathRE.FindAllString(msg.Text, -1), nearby(msg, usable)...)))
		for _, sent := range splitSentences(msg.Text) {
			if skipSentence(sent) {
				continue
			}
			typ := classify(sent, msg)
			if typ == "" {
				continue
			}
			if (typ == "state" || typ == "thread") && !inTail[msg.Offset] {
				continue
			}
			text := strings.TrimSpace(sent)
			if len(text) < 12 || len(text) > 600 {
				continue
			}
			if redact.ShouldDropClaim(text, paths) {
				continue
			}
			if p := paths; len(p) > 8 {
				p = p[:8]
			}
			drafts = append(drafts, makeRec(typ, text, paths, msg.Offset, opts))
		}
	}
	dedup := map[string]claim.Record{}
	for _, r := range drafts {
		prev, ok := dedup[r.ClaimHash]
		if !ok || priority[r.Type] > priority[prev.Type] {
			dedup[r.ClaimHash] = r
		}
	}
	out := make([]claim.Record, 0, len(dedup))
	for _, r := range dedup {
		out = append(out, r)
	}
	if len(out) > 12 {
		// keep highest priority
		sortByPri(out)
		out = out[:12]
	}
	return out
}

func classify(sentence string, msg Message) string {
	if isQuestion(sentence) {
		return ""
	}
	if msg.Error || failedRE.MatchString(sentence) {
		return "failed"
	}
	if decisionRE.MatchString(sentence) {
		return "decision"
	}
	if constraintRE.MatchString(sentence) && msg.Role == "user" && !hedgeRE.MatchString(sentence) {
		return "constraint"
	}
	if stateRE.MatchString(sentence) {
		return "state"
	}
	return ""
}

func skipSentence(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") {
		return true
	}
	if strings.HasPrefix(t, "**") && strings.HasSuffix(t, "**") && !strings.Contains(t, ".") {
		return true
	}
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
		if strings.Contains(t, "**") || strings.HasPrefix(strings.TrimSpace(t[2:]), "`") || strings.HasPrefix(strings.TrimSpace(t[2:]), ">") {
			return true
		}
	}
	low := strings.ToLower(t)
	if strings.Contains(low, "fixture") || strings.Contains(low, "quoted the") {
		return true
	}
	return false
}

func isQuestion(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "?") {
		return true
	}
	return questionRE.MatchString(s)
}

func makeRec(typ, text string, paths []string, offset int64, opts ExtractOpts) claim.Record {
	if len(paths) > 8 {
		paths = paths[:8]
	}
	mt := map[string]int64{}
	if opts.WorkspaceRoot != "" {
		for _, p := range paths {
			if fi, err := os.Stat(filepath.Join(opts.WorkspaceRoot, p)); err == nil {
				mt[p] = fi.ModTime().UnixMilli()
			}
		}
	}
	return claim.Record{
		ID:            claim.NewID(),
		Type:          typ,
		ProjectKey:    opts.ProjectKey,
		WorkspaceRoot: opts.WorkspaceRoot,
		Harness:       opts.Harness,
		SessionID:     opts.SessionID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Text:          text,
		Paths:         paths,
		Symbols:       claim.ExtractSymbols(text, paths),
		PathMtime:     mt,
		Status:        "active",
		Source:        opts.Source,
		ClaimHash:     claim.Hash(opts.ProjectKey, typ, text),
		TranscriptRef: &claim.TranscriptRef{
			SessionID:   opts.SessionID,
			StartOffset: offset,
			EndOffset:   offset + int64(len(text)),
		},
	}
}

func tail(msgs []Message, maxN, maxChars int) []Message {
	var out []Message
	chars := 0
	for i := len(msgs) - 1; i >= 0 && len(out) < maxN; i-- {
		chars += len(msgs[i].Text)
		if chars > maxChars && len(out) > 0 {
			break
		}
		out = append(out, msgs[i])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func collectPaths(msgs []Message) []string {
	var found []string
	for _, m := range msgs {
		found = append(found, pathRE.FindAllString(m.Text, -1)...)
	}
	return uniq(found)
}

func nearby(msg Message, all []Message) []string {
	idx := -1
	for i, m := range all {
		if m.Offset == msg.Offset {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	lo, hi := idx-2, idx+3
	if lo < 0 {
		lo = 0
	}
	if hi > len(all) {
		hi = len(all)
	}
	return collectPaths(all[lo:hi])
}

func splitSentences(text string) []string {
	var out []string
	var cur strings.Builder
	rs := []rune(text)
	for i, r := range rs {
		cur.WriteRune(r)
		if r == '\n' || r == '!' || r == '?' || (r == '.' && !fileExtDot(rs, i)) {
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func fileExtDot(rs []rune, i int) bool {
	if i == 0 || !alnum(rs[i-1]) {
		return false
	}
	n := 0
	j := i + 1
	for j < len(rs) && alnum(rs[j]) && n < 8 {
		j++
		n++
	}
	if n == 0 {
		return false
	}
	if j == len(rs) {
		return true
	}
	switch rs[j] {
	case ' ', '\t', ',', ';', ':', ')', ']', '\'', '"', '.', '!', '?':
		return true
	}
	return false
}

func alnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func sortByPri(rs []claim.Record) {
	for i := 0; i < len(rs); i++ {
		for j := i + 1; j < len(rs); j++ {
			if priority[rs[j].Type] > priority[rs[i].Type] {
				rs[i], rs[j] = rs[j], rs[i]
			}
		}
	}
}
