package harness

import (
	"os"
	"path/filepath"
	"strings"
)

type Locate struct {
	JSONL     string
	SessionID string
	CWD       string
	// Harness is set when a hook for one harness handed us another
	// harness's file (Grok fires Claude-scope hooks with updates.jsonl).
	Harness string
}

// GrokEventStream is Grok's updates.jsonl: a JSON-RPC event log beside the
// tape, never a session file. Grok 1.0.13 passes it as transcript_path to
// Claude-scope hooks.
func GrokEventStream(path string) bool {
	if path == "" {
		return false
	}
	n := filepath.ToSlash(path)
	return filepath.Base(n) == "updates.jsonl" || strings.Contains(n, "/.grok/sessions/")
}

// LocateGrokFromEventStream maps updates.jsonl to the sibling
// chat_history.jsonl. cwd falls back to the decoded session directory.
func LocateGrokFromEventStream(path, sessionID, cwd string) Locate {
	dir := filepath.Dir(path)
	sid := sessionID
	if sid == "" {
		sid = filepath.Base(dir)
	}
	if cwd == "" {
		cwd = DecodeGrokSessionDir(filepath.Base(filepath.Dir(dir)))
	}
	return Locate{JSONL: filepath.Join(dir, "chat_history.jsonl"), SessionID: sid, CWD: cwd, Harness: "grok"}
}

// LocateGrok finds chat_history.jsonl. Never updates.jsonl.
func LocateGrok(workspace, sessionID string) Locate {
	home := os.Getenv("GROK_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".grok")
	}
	enc := EncodeCWD(workspace)
	base := filepath.Join(home, "sessions", enc, sessionID)
	hist := filepath.Join(base, "chat_history.jsonl")
	if fileExists(hist) {
		return Locate{JSONL: hist, SessionID: sessionID, CWD: workspace}
	}
	return Locate{JSONL: hist, SessionID: sessionID, CWD: workspace}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// EncodeCWD is Grok's session-directory encoding of a workspace path.
func EncodeCWD(s string) string { return encodeURIComponent(s) }

func encodeURIComponent(s string) string {
	// Grok uses URL-encode of the cwd (slashes as %2F).
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b = append(b, c)
			continue
		}
		const hex = "0123456789ABCDEF"
		b = append(b, '%', hex[c>>4], hex[c&15])
	}
	return string(b)
}
