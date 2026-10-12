package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// omp (oh-my-pi) keeps a Pi-style JSONL tape, so this adapter is the Pi one
// with omp's paths and one extra wrinkle: session files open with a
// fixed-width 256-byte `type:"title"` slot before the `type:"session"`
// header, so a cwd peek cannot read line one.

// OmpConfigRoot is where omp keeps its user config. OMP_HOME is ours;
// PI_CONFIG_DIR is omp's own relocation of the same root.
func OmpConfigRoot(home string) string {
	if h := os.Getenv("OMP_HOME"); h != "" {
		return h
	}
	if d := os.Getenv("PI_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home, ".omp")
}

// OmpAgentDir is omp's active agent directory (its docs: default
// ~/.omp/agent, profiles use ~/.omp/profiles/<name>/agent). PI_CODING_AGENT_DIR
// overrides it.
func OmpAgentDir(home string) string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	return filepath.Join(OmpConfigRoot(home), "agent")
}

func OmpSessionsRoot() string {
	return filepath.Join(OmpAgentDir(os.Getenv("HOME")), "sessions")
}

// OmpSessionSlug is omp's session-directory encoding (omp docs, session.md
// "On-Disk Layout"): the canonicalized cwd gives `-<relative>` under home,
// `-tmp-<relative>` under the temp root, and `--<encoded-absolute>--`
// anywhere else, with separators replaced by `-`. The canonical path is what
// buckets by real location, so /tmp and /private/tmp share `--private-tmp--`.
func OmpSessionSlug(cwd string) string {
	p := canonicalPath(cwd)
	if rel, ok := underPath(p, canonicalPath(os.Getenv("HOME"))); ok {
		return "-" + ompEncode(rel)
	}
	if rel, ok := underPath(p, canonicalPath(os.TempDir())); ok {
		return "-tmp-" + ompEncode(rel)
	}
	return "--" + ompEncode(strings.TrimPrefix(filepath.ToSlash(p), "/")) + "--"
}

// canonicalPath resolves symlinks when the path exists, so /tmp and
// /private/tmp land in one bucket. A path that does not exist yet still
// resolves through its longest existing prefix, so a vanished or not-yet
// created cwd buckets by its real parent.
func canonicalPath(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	dir, rest := p, ""
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
	}
}

// underPath reports the relative path when p sits under root.
func underPath(p, root string) (string, bool) {
	if p == "" || root == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func ompEncode(p string) string {
	r := strings.NewReplacer("/", "-", ":", "-", "\\", "-")
	return r.Replace(filepath.ToSlash(p))
}

func OmpSessionIDFromPath(p string) string {
	return PiSessionIDFromPath(p)
}

// ompStamp is the leading part of an omp session filename:
// 2026-10-12T16-19-23-616Z_<uuid>.jsonl (colons written as dashes).
var ompStamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T[\d-]+Z$`)

// OmpSessionFile reports an omp session tape, not one of the subagent
// transcripts and bash logs that share a session's artifact directory
// (the directory named after the session, without .jsonl).
func OmpSessionFile(path string) bool {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".jsonl") || strings.HasPrefix(base, ".") {
		return false
	}
	stem := strings.TrimSuffix(base, ".jsonl")
	i := strings.LastIndex(stem, "_")
	if i <= 0 || i+1 >= len(stem) {
		return false
	}
	return ompStamp.MatchString(stem[:i])
}

// LocateOmp finds the session tape: the hook's transcript_path when omp
// gives us one, else the cwd bucket, else any file naming that session id.
func LocateOmp(transcript, sessionID, cwd string) Locate {
	if transcript != "" {
		sid := sessionID
		if sid == "" {
			sid = OmpSessionIDFromPath(transcript)
		}
		return Locate{JSONL: transcript, SessionID: sid, CWD: cwd}
	}
	root := OmpSessionsRoot()
	if cwd != "" {
		dir := filepath.Join(root, OmpSessionSlug(cwd))
		if sessionID != "" {
			if p := findOmpBySession(dir, sessionID); p != "" {
				return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
			}
		}
		if p, sid := latestOmp(dir); p != "" {
			if sessionID == "" {
				sessionID = sid
			}
			return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
		}
	}
	if sessionID != "" {
		// The bucket name is not reversible, so a session we were handed by
		// id alone is searched for under every bucket.
		if p := findOmpBySession(root, sessionID); p != "" {
			if cwd == "" {
				cwd = PeekOmpCWD(p)
			}
			return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
		}
	}
	return Locate{SessionID: sessionID, CWD: cwd}
}

func findOmpBySession(root, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if !OmpSessionFile(path) {
			return nil
		}
		if OmpSessionIDFromPath(path) == sessionID || strings.Contains(filepath.Base(path), sessionID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func latestOmp(dir string) (string, string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	var best string
	var bestMod time.Time
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if best == "" || st.ModTime().After(bestMod) {
			best = p
			bestMod = st.ModTime()
		}
	}
	if best == "" {
		return "", ""
	}
	return best, OmpSessionIDFromPath(best)
}

// PeekOmpCWD reads the cwd out of the session header, skipping the
// fixed-width title slot a written session opens with. Legacy files that
// start at the header still work.
func PeekOmpCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 0; i < 4 && sc.Scan(); i++ {
		var o map[string]any
		if json.Unmarshal(sc.Bytes(), &o) != nil {
			continue
		}
		if o["type"] != "session" {
			continue
		}
		cwd, _ := o["cwd"].(string)
		return cwd
	}
	return ""
}
