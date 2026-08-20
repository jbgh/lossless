package write

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"lossless/internal/claim"
	"lossless/internal/gate"
	"lossless/internal/redact"
)

var pathRE = regexp.MustCompile(`(?:[\w.-]+/)+[\w.-]+\.[A-Za-z0-9]+`)

var priority = map[string]int{"failed": 5, "decision": 4, "constraint": 3, "state": 2, "thread": 1}

type ExtractOpts struct {
	ProjectKey    string
	WorkspaceRoot string
	Harness       string
	SessionID     string
	Source        string
	Trace         *ExtractTrace
}

// ExtractTrace is why extract kept or skipped sentences. Nil on the live path.
type ExtractTrace struct {
	Messages   int            `json:"messages"`
	Sentences  int            `json:"sentences"`
	Drafts     int            `json:"drafts"`
	Kept       int            `json:"kept"`
	SkipCounts map[string]int `json:"skip_counts,omitempty"`
	Samples    []SkipSample   `json:"samples,omitempty"`
}

type SkipSample struct {
	Reason string `json:"reason"`
	Text   string `json:"text"`
}

func (t *ExtractTrace) note(reason, sent string) {
	if t == nil {
		return
	}
	if t.SkipCounts == nil {
		t.SkipCounts = map[string]int{}
	}
	t.SkipCounts[reason]++
	if len(t.Samples) < 16 {
		t.Samples = append(t.Samples, SkipSample{Reason: reason, Text: clipSent(sent, 120)})
	}
}

func (t *ExtractTrace) skip(reason, sent string) {
	if t == nil {
		return
	}
	t.Sentences++
	t.note(reason, sent)
}

func Extract(msgs []Message, opts ExtractOpts) []claim.Record {
	var usable []Message
	for _, m := range msgs {
		if !m.Skip {
			usable = append(usable, m)
		}
	}
	if opts.Trace != nil {
		opts.Trace.Messages = len(usable)
	}
	recent := tail(usable, 40, 32000)
	inTail := map[int64]bool{}
	for _, m := range recent {
		inTail[m.Offset] = true
	}
	tr := opts.Trace
	drafts := []claim.Record{}
	for _, msg := range usable {
		if msg.Role == "tool" {
			continue
		}
		near := nearby(msg, usable)
		for _, sent := range splitSentences(msg.Text) {
			if skipSentence(sent) {
				reason := "skip-prose"
				if !gate.SkipProse(sent) {
					reason = "list-chrome"
				}
				tr.skip(reason, sent)
				continue
			}
			paths := redact.FilterPaths(claim.Uniq(append(findPaths(sent), near...)))
			typ := classify(sent, msg)
			if typ == "state" && gate.ProcessState(sent) {
				tr.skip("process-state", sent)
				continue
			}
			if typ == "failed" && (gate.StatusFailed(sent) || gate.FailedAsObject(sent) || !groundedFailed(sent, paths)) {
				reason := "ungrounded-failed"
				if gate.StatusFailed(sent) {
					reason = "status-failed"
				} else if gate.FailedAsObject(sent) {
					reason = "failed-as-object"
				}
				tr.skip(reason, sent)
				continue
			}
			if typ == "" {
				tr.skip("untyped", sent)
				continue
			}
			if (typ == "state" || typ == "thread") && !inTail[msg.Offset] {
				tr.skip("state-not-in-tail", sent)
				continue
			}
			text := strings.TrimSpace(sent)
			if len(text) < 12 || len(text) > 600 {
				tr.skip("length", sent)
				continue
			}
			if redact.ShouldDropClaim(text, paths) {
				tr.skip("redact", sent)
				continue
			}
			if tr != nil {
				tr.Sentences++
			}
			drafts = append(drafts, makeRec(typ, text, paths, msg, opts))
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
	if tr != nil {
		tr.Drafts = len(out)
	}
	if len(out) > 12 {
		kept := capExtract(out, 12, 5)
		if tr != nil {
			stay := map[string]bool{}
			for _, r := range kept {
				stay[r.ClaimHash] = true
			}
			for _, r := range out {
				if !stay[r.ClaimHash] {
					tr.note("cap-extract", r.Text)
				}
			}
		}
		out = kept
	}
	if tr != nil {
		tr.Kept = len(out)
	}
	return out
}

func clipSent(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func capExtract(in []claim.Record, total, perType int) []claim.Record {
	sortByPri(in)
	var pathful, pathless []claim.Record
	for _, r := range in {
		if len(r.Paths) > 0 {
			pathful = append(pathful, r)
		} else {
			pathless = append(pathless, r)
		}
	}
	counts := map[string]int{}
	dirN := map[string]int{}
	var kept []claim.Record
	take := func(r claim.Record) bool {
		if counts[r.Type] >= perType {
			return false
		}
		d := primaryDir(r.Paths)
		dk := r.Type + "|" + d
		dirCap := 2
		if r.Type == "decision" || r.Type == "constraint" {
			dirCap = 5
		}
		if d != "" && dirN[dk] >= dirCap {
			return false
		}
		kept = append(kept, r)
		counts[r.Type]++
		if d != "" {
			dirN[dk]++
		}
		return len(kept) >= total
	}
	for _, r := range pathful {
		if take(r) {
			return kept
		}
	}
	for _, r := range pathless {
		if take(r) {
			return kept
		}
	}
	return kept
}

func primaryDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	p := paths[0]
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return p
}

func makeRec(typ, text string, paths []string, msg Message, opts ExtractOpts) claim.Record {
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
	ref := messageSpan(msg)
	ref.SessionID = opts.SessionID
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
		TranscriptRef: &ref,
	}
}

func messageSpan(msg Message) claim.TranscriptRef {
	end := msg.Offset + int64(len(msg.Text))
	if end <= msg.Offset {
		end = msg.Offset + 1
	}
	return claim.TranscriptRef{StartOffset: msg.Offset, EndOffset: end}
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

func slashNorm(s string) string {
	return strings.ReplaceAll(s, "\\", "/")
}

func findPaths(s string) []string {
	return pathRE.FindAllString(slashNorm(s), -1)
}

func stripPaths(s string) string {
	return pathRE.ReplaceAllString(slashNorm(s), " ")
}

func collectPaths(msgs []Message) []string {
	var found []string
	for _, m := range msgs {
		found = append(found, findPaths(m.Text)...)
	}
	return claim.Uniq(found)
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
	if msg.Role == "user" {
		return nil
	}
	// Preceding user turns only (not other assistant lines). Look
	// back a few users so "Picked jose..." still inherits auth.ts
	// from "Add rate limiting to src/middleware/auth.ts" when the
	// last user was "bigger pool?" with no path.
	var paths []string
	users := 0
	for i := idx - 1; i >= 0 && users < 3; i-- {
		if all[i].Role != "user" {
			continue
		}
		users++
		paths = append(paths, collectPaths(all[i:i+1])...)
	}
	return paths
}

func splitSentences(text string) []string {
	var out []string
	var cur strings.Builder
	rs := []rune(text)
	for i, r := range rs {
		cur.WriteRune(r)
		if r == '\n' || r == '!' || r == '?' || (r == '.' && !fileExtDot(rs, i) && !listMarkerDot(rs, i)) {
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

func listMarkerDot(rs []rune, i int) bool {
	if i == 0 || rs[i-1] < '0' || rs[i-1] > '9' {
		return false
	}
	j := i - 1
	for j > 0 && rs[j-1] >= '0' && rs[j-1] <= '9' {
		j--
	}
	if j > 0 && rs[j-1] != '\n' {
		return false
	}
	if i+1 < len(rs) && (rs[i+1] == ' ' || rs[i+1] == '\t') {
		return true
	}
	return false
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
	case ' ', '\t', ',', ';', ':', ')', ']', '\'', '"', '.', '!', '?', '-':
		return true
	}
	return false
}

func alnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func uniq(xs []string) []string { return claim.Uniq(xs) }

func sortByPri(rs []claim.Record) {
	sort.SliceStable(rs, func(i, j int) bool {
		return priority[rs[i].Type] > priority[rs[j].Type]
	})
}
