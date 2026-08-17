package retrieve

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"lossless/internal/claim"
	"lossless/internal/env"
	"lossless/internal/projectkey"
	"lossless/internal/write"
)

var (
	compilePathRE   = regexp.MustCompile(`(?:[\w.-]+/)+[\w.-]+\.[A-Za-z0-9]+`)
	compileFailedRE = regexp.MustCompile(`(?i)\b(rejected|didn't work|did not work|revert|abort|failed|failure|error|didn't compile|does not work)\b`)
)

func (e Engine) maybeCompile(req Request, q query) query {
	if rich(q) {
		return q
	}
	filled := req
	path := e.locateSession(q.ProjectKey, q.WorkspaceRoot)
	tail := readTail(path)
	if filled.Question == "" {
		if u := lastUserText(tail); u != "" {
			if len(u) > CompileQuestionCap {
				u = u[:CompileQuestionCap]
			}
			filled.Question = u
		}
	}
	if len(filled.Paths) == 0 {
		filled.Paths = pathsFromTail(tail)
		if path != "" && e.Store != nil && len(filled.Paths) < CompilePathCap {
			recent, err := e.Store.RecentPaths(q.ProjectKey, RecentClaimPathLimit)
			if err == nil {
				filled.Paths = uniq(append(filled.Paths, recent...))
			}
		}
		if len(filled.Paths) > CompilePathCap {
			filled.Paths = filled.Paths[:CompilePathCap]
		}
	}
	out, err := normalize(filled)
	if err != nil {
		return q
	}
	out.LookupTokens = uniq(append(out.LookupTokens, failedTokens(tail)...))
	out.Head = isHead(out)
	return out
}

func (e Engine) locateSession(project, workspace string) string {
	if e.LocateSession != nil {
		return e.LocateSession(project, workspace)
	}
	root := e.Home
	if root == "" && e.Store != nil && e.Store.Root != "" {
		root = e.Store.Root
	}
	if root == "" {
		root = env.Home()
	}
	if p := newestJSONL(filepath.Join(root, "raw", projectkey.Encode(project))); p != "" {
		return p
	}
	return ""
}

func newestJSONL(dir string) string {
	var newest string
	var newestMod int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.zst") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mt := info.ModTime().UnixNano()
		if newest == "" || mt > newestMod {
			newest = p
			newestMod = mt
		}
		return nil
	})
	return newest
}

func readTail(path string) []write.Message {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	if len(b) > CompileTailChars*2 {
		b = b[len(b)-CompileTailChars*2:]
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	msgs, _ := write.ParseJSONL(string(b), 0)
	var usable []write.Message
	for _, m := range msgs {
		if m.Skip || strings.TrimSpace(m.Text) == "" {
			continue
		}
		usable = append(usable, m)
	}
	if len(usable) > CompileTailMsgs {
		usable = usable[len(usable)-CompileTailMsgs:]
	}
	var n int
	for i := len(usable) - 1; i >= 0; i-- {
		n += len(usable[i].Text)
		if n > CompileTailChars {
			return usable[i+1:]
		}
	}
	return usable
}

func lastUserText(msgs []write.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if strings.EqualFold(msgs[i].Role, "user") {
			return strings.TrimSpace(msgs[i].Text)
		}
	}
	return ""
}

func pathsFromTail(msgs []write.Message) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range msgs {
		for _, p := range compilePathRE.FindAllString(m.Text, -1) {
			for _, k := range claim.PathKeys([]string{p}) {
				if k == "" || seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

func failedTokens(msgs []write.Message) []string {
	var out []string
	for _, m := range msgs {
		if !m.Error && !compileFailedRE.MatchString(m.Text) {
			continue
		}
		out = append(out, claim.Tokens(m.Text)...)
	}
	return out
}
