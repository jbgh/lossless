package harness

import (
	"os"
	"path/filepath"
)

type Locate struct {
	JSONL     string
	SessionID string
	CWD       string
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
