package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func PiHome() string {
	if h := os.Getenv("PI_HOME"); h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".pi")
}

// PiSessionSlug is --<cwd with / replaced by ->-- as documented in
// earendil-works/pi packages/coding-agent/docs/session-format.md.
func PiSessionSlug(cwd string) string {
	p := filepath.ToSlash(filepath.Clean(cwd))
	p = strings.TrimPrefix(p, "/")
	p = strings.ReplaceAll(p, "/", "-")
	p = strings.ReplaceAll(p, ":", "-")
	return "--" + p + "--"
}

func PiSessionsRoot() string {
	return filepath.Join(PiHome(), "agent", "sessions")
}

func PiSessionIDFromPath(p string) string {
	base := strings.TrimSuffix(filepath.Base(p), ".jsonl")
	if i := strings.LastIndex(base, "_"); i >= 0 && i+1 < len(base) {
		return base[i+1:]
	}
	return base
}

func LocatePi(transcript, sessionID, cwd string) Locate {
	if transcript != "" {
		sid := sessionID
		if sid == "" {
			sid = PiSessionIDFromPath(transcript)
		}
		return Locate{JSONL: transcript, SessionID: sid, CWD: cwd}
	}
	root := PiSessionsRoot()
	if cwd != "" {
		dir := filepath.Join(root, PiSessionSlug(cwd))
		if sessionID != "" {
			if p := findPiBySession(dir, sessionID); p != "" {
				return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
			}
		}
		if p, sid := latestPi(dir); p != "" {
			if sessionID == "" {
				sessionID = sid
			}
			return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
		}
	}
	if sessionID != "" {
		if p := findPiBySession(root, sessionID); p != "" {
			if cwd == "" {
				cwd = PeekPiCWD(p)
			}
			return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
		}
	}
	return Locate{SessionID: sessionID, CWD: cwd}
}

func findPiBySession(root, sessionID string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if PiSessionIDFromPath(path) == sessionID || strings.Contains(filepath.Base(path), sessionID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func latestPi(dir string) (string, string) {
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
	return best, PiSessionIDFromPath(best)
}

func PeekPiCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() {
		return ""
	}
	var o map[string]any
	if json.Unmarshal(sc.Bytes(), &o) != nil {
		return ""
	}
	if o["type"] != "session" {
		return ""
	}
	cwd, _ := o["cwd"].(string)
	return cwd
}
