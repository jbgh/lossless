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
			if typ == "failed" && (statusFailed(sent) || failedAsObject(sent) || !groundedFailed(sent, paths)) {
				continue
			}
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
	if msg.Error || (failedRE.MatchString(stripTypeTalk(sentence)) && !metaFailedTalk(sentence)) {
		return "failed"
	}
	if decisionRE.MatchString(sentence) && !planningDecision(sentence) {
		return "decision"
	}
	if constraintRE.MatchString(sentence) && msg.Role == "user" && !hedgeRE.MatchString(sentence) && !sessionOpConstraint(sentence) && !agentPromptConstraint(sentence) {
		return "constraint"
	}
	if stateRE.MatchString(sentence) {
		return "state"
	}
	return ""
}

var (
	backtickSpan = regexp.MustCompile("`[^`]*`")
	typeTalkRE   = regexp.MustCompile(`(?i)\b(failed-overlap|shipped-overlap|type-cap|packtypecap|classified as|claim type|as a failed|as failed)\b`)
)

func stripTypeTalk(s string) string {
	s = backtickSpan.ReplaceAllString(s, " ")
	s = typeTalkRE.ReplaceAllString(s, " ")
	return s
}

func metaFailedTalk(s string) bool {
	low := strings.ToLower(s)
	for _, n := range []string{
		"failed-overlap", "classified as", "type-cap", "packtype",
		"extract noise", "ask pack", "in context", "blocking warning",
		"failure mode", "failed eviction",
	} {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func skipSentence(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	t = strings.TrimLeft(t, "\"“”'`")
	if t == "" {
		return true
	}
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "|") {
		return true
	}
	if strings.Contains(t, " | ") {
		return true
	}
	if strings.HasPrefix(t, "**") && strings.HasSuffix(t, "**") && !strings.Contains(t, ".") {
		return true
	}
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
		rest := strings.TrimSpace(t[2:])
		if strings.Contains(t, "**") || strings.HasPrefix(rest, "`") || strings.HasPrefix(rest, ">") || (len(t) < 80 && pathRE.FindString(t) == "") {
			return true
		}
	}
	low := strings.ToLower(t)
	if strings.Contains(low, "fixture") || strings.Contains(low, "quoted the") || strings.Contains(low, "quoting the") {
		return true
	}
	if strings.HasPrefix(low, "next i ") || strings.HasPrefix(low, "next i'") || strings.HasPrefix(low, "next i’ll") || strings.HasPrefix(low, "next i'll") {
		return true
	}
	if metaFailedTalk(t) || sessionOpConstraint(t) || agentPromptConstraint(t) || planningDecision(t) || statusFailed(t) || failedAsObject(t) {
		return true
	}
	if strings.HasSuffix(t, "(") || strings.HasSuffix(t, "`." ) || strings.HasSuffix(t, "do not") {
		return true
	}
	return false
}

func planningDecision(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	for _, p := range []string{
		"i'll check", "i’ll check", "i will check", "i'll look", "i’ll look",
		"i'll read", "i’ll read", "i'll audit", "i’ll audit",
		"i'll fix", "i’ll fix", "i'll start", "i’ll start", "i'll add", "i’ll add",
		"i'll inspect", "i’ll inspect",
	} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func sessionOpConstraint(s string) bool {
	low := strings.ToLower(s)
	for _, p := range []string{
		"don't ask", "do not ask", "don't change source", "don't delete data",
		"do not open a pr", "do not redo", "don't flag", "do not start",
	} {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

func agentPromptConstraint(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "why don't you") || strings.Contains(low, "why dont you") ||
		strings.HasPrefix(low, "can you ") || strings.HasPrefix(low, "could you ")
}

func statusFailed(s string) bool {
	low := strings.ToLower(s)
	for _, n := range []string{
		"ci unit-test", "unit-test failure", "unit test failure",
		"background notification", "checking #", "pr #", "pr-size-check",
		"which of those", "re-pushing", "exit 0",
	} {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func failedAsObject(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "failed items") || strings.Contains(low, "re-queues failed") ||
		strings.Contains(low, "pre-failed skip") || strings.Contains(low, "failure reason") ||
		strings.Contains(low, "retryable failure")
}

func groundedFailed(s string, paths []string) bool {
	if len(paths) > 0 || pathRE.FindString(s) != "" {
		return true
	}
	if statusFailed(s) || failedAsObject(s) {
		return false
	}
	if strings.Contains(s, "`") || strings.Contains(s, "**") {
		return true
	}
	fields := strings.Fields(s)
	for i, w := range fields {
		w = strings.Trim(w, ".,;:()[]\"'")
		if w == "" {
			continue
		}
		if i == 0 && sentenceStarter[w] {
			continue
		}
		if len(w) >= 3 && w[0] >= 'A' && w[0] <= 'Z' && !allUpper(w) {
			return true
		}
		if strings.Contains(w, "-") && w[0] >= 'A' && w[0] <= 'Z' {
			return true
		}
	}
	return false
}

var sentenceStarter = map[string]bool{
	"The": true, "This": true, "That": true, "These": true, "Those": true,
	"So": true, "If": true, "We": true, "On": true, "A": true, "An": true,
	"It": true, "I": true, "After": true, "Before": true, "When": true,
	"While": true, "Then": true, "Also": true, "Just": true, "Checking": true,
}

func allUpper(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

func isQuestion(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "?") {
		return true
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "why don't you") || strings.Contains(low, "why dont you") {
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
