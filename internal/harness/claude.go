package harness

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeProjectSlug is how Claude names a project dir: /Users/a/b → -Users-a-b.
func ClaudeProjectSlug(cwd string) string {
	p := filepath.ToSlash(filepath.Clean(cwd))
	return strings.ReplaceAll(p, "/", "-")
}

// LocateClaude prefers hook transcript_path. Never invent a path when that field is set.
func LocateClaude(transcript, sessionID, cwd string) Locate {
	if transcript != "" {
		sid := sessionID
		if sid == "" {
			sid = strings.TrimSuffix(filepath.Base(transcript), ".jsonl")
		}
		return Locate{JSONL: transcript, SessionID: sid, CWD: cwd}
	}
	home := os.Getenv("CLAUDE_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".claude")
	}
	if sessionID == "" || cwd == "" {
		return Locate{SessionID: sessionID, CWD: cwd}
	}
	p := filepath.Join(home, "projects", ClaudeProjectSlug(cwd), sessionID+".jsonl")
	return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
}

func DecodeGrokSessionDir(name string) string {
	// Grok stores URL-encoded cwd (%2FUsers%2F...).
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		if name[i] == '%' && i+2 < len(name) {
			hi := unhex(name[i+1])
			lo := unhex(name[i+2])
			if hi >= 0 && lo >= 0 {
				b.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		b.WriteByte(name[i])
	}
	return b.String()
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c - 'A' + 10)
	case c >= 'a' && c <= 'f':
		return int(c - 'a' + 10)
	default:
		return -1
	}
}
