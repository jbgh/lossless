package harness

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func CodexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	return filepath.Join(os.Getenv("HOME"), ".codex")
}

// CodexSessionIDFromPath pulls the UUID suffix from rollout-<ts>-<uuid>.jsonl.
func CodexSessionIDFromPath(p string) string {
	base := strings.TrimSuffix(filepath.Base(p), ".jsonl")
	base = strings.TrimPrefix(base, "rollout-")
	const uuidLen = 36
	if len(base) >= uuidLen {
		cand := base[len(base)-uuidLen:]
		if strings.Count(cand, "-") == 4 {
			return cand
		}
	}
	if i := strings.LastIndex(base, "-"); i >= 0 && i+1 < len(base) {
		return base[i+1:]
	}
	return base
}

func LocateCodex(transcript, sessionID, cwd string) Locate {
	if transcript != "" {
		sid := sessionID
		if sid == "" {
			sid = CodexSessionIDFromPath(transcript)
		}
		return Locate{JSONL: transcript, SessionID: sid, CWD: cwd}
	}
	if sessionID != "" {
		if p := findCodexInState(CodexHome(), sessionID); p != "" {
			return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
		}
	}
	root := filepath.Join(CodexHome(), "sessions")
	if sessionID != "" {
		if p := findCodexBySession(root, sessionID); p != "" {
			return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
		}
	}
	if cwd != "" {
		if p, sid := findCodexByCWD(root, cwd); p != "" {
			if sessionID == "" {
				sessionID = sid
			}
			return Locate{JSONL: p, SessionID: sessionID, CWD: cwd}
		}
	}
	return Locate{SessionID: sessionID, CWD: cwd}
}

func findCodexBySession(root, sessionID string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !isCodexRollout(path) {
			return nil
		}
		if CodexSessionIDFromPath(path) == sessionID || strings.Contains(filepath.Base(path), sessionID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func findCodexByCWD(root, cwd string) (string, string) {
	cwd = filepath.Clean(cwd)
	var best string
	var bestMod time.Time
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !isCodexRollout(path) {
			return nil
		}
		_, metaCWD := PeekCodexMeta(path)
		if filepath.Clean(metaCWD) != cwd {
			return nil
		}
		st, err := os.Stat(path)
		if err != nil {
			return nil
		}
		if best == "" || st.ModTime().After(bestMod) {
			best = path
			bestMod = st.ModTime()
		}
		return nil
	})
	if best == "" {
		return "", ""
	}
	return best, CodexSessionIDFromPath(best)
}

func isCodexRollout(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, "rollout-") && strings.HasSuffix(base, ".jsonl")
}

// PeekCodexMeta reads session_meta (or legacy SessionMeta) from the head of a rollout file.
func PeekCodexMeta(path string) (id, cwd string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 0; i < 20 && sc.Scan(); i++ {
		var o map[string]any
		if json.Unmarshal(sc.Bytes(), &o) != nil {
			continue
		}
		if item, ok := o["item"].(map[string]any); ok && o["type"] == nil {
			o = item
		}
		typ, _ := o["type"].(string)
		payload, _ := o["payload"].(map[string]any)
		if payload == nil {
			payload = o
		}
		if typ == "session_meta" || typ == "SessionMeta" {
			id, _ = asString(payload["id"])
			if id == "" {
				id, _ = asString(o["id"])
			}
			cwd, _ = asString(payload["cwd"])
			if cwd == "" {
				cwd, _ = asString(o["cwd"])
			}
			return id, cwd
		}
	}
	return "", ""
}

func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func findCodexInState(home, sessionID string) string {
	for _, name := range []string{"state_5.sqlite", "state_4.sqlite", "state.sqlite"} {
		p := filepath.Join(home, name)
		if !fileExists(p) {
			continue
		}
		if path := queryCodexRollout(p, sessionID); path != "" {
			return path
		}
	}
	return ""
}

func queryCodexRollout(dbPath, sessionID string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()
	var path string
	err = db.QueryRow(`SELECT rollout_path FROM threads WHERE id = ? AND rollout_path IS NOT NULL AND rollout_path != ''`, sessionID).Scan(&path)
	if err != nil || path == "" {
		return ""
	}
	if fileExists(path) {
		return path
	}
	return ""
}

// CodexStateRollouts lists rollout_path values from the desktop/CLI state DB.
func CodexStateRollouts(home string) []string {
	var out []string
	for _, name := range []string{"state_5.sqlite", "state_4.sqlite", "state.sqlite"} {
		p := filepath.Join(home, name)
		if !fileExists(p) {
			continue
		}
		db, err := sql.Open("sqlite", "file:"+p+"?mode=ro")
		if err != nil {
			continue
		}
		rows, err := db.Query(`SELECT rollout_path FROM threads WHERE rollout_path IS NOT NULL AND rollout_path != ''`)
		if err != nil {
			_ = db.Close()
			continue
		}
		for rows.Next() {
			var path string
			if rows.Scan(&path) == nil && fileExists(path) {
				out = append(out, path)
			}
		}
		_ = rows.Close()
		_ = db.Close()
	}
	return out
}
