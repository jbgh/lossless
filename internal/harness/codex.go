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

type CodexThread struct {
	ID        string
	CWD       string
	Rollout   string
	FirstUser string
	Updated   int64
}

// CodexStateThreads lists desktop/CLI threads, including rows whose
// rollout_path is empty or the file is gone. sessions/ may not exist.
func CodexStateThreads(home string) []CodexThread {
	rows, _ := CodexStateRows(home)
	return CodexLiveRollouts(rows)
}

// CodexStateDBs are the Codex state databases CodexStateRows reads, newest
// schema first, whether or not they exist.
func CodexStateDBs(home string) []string {
	names := []string{"state_5.sqlite", "state_4.sqlite", "state.sqlite"}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(home, n)
	}
	return out
}

// CodexStateRows reads every thread row as stored, rollout_path included
// even when its file is gone. The rows change only when a state database
// does, so the watcher caches them; CodexLiveRollouts applies the file
// check, which can change any time. complete is false when a database
// that exists could not be read in full (Codex holding a lock, a schema
// mid-migration): the rows are what could be read, and a caller must not
// keep them as the database's contents.
func CodexStateRows(home string) (out []CodexThread, complete bool) {
	complete = true
	seen := map[string]bool{}
	for _, p := range CodexStateDBs(home) {
		if !fileExists(p) {
			continue
		}
		db, err := sql.Open("sqlite", "file:"+p+"?mode=ro")
		if err != nil {
			complete = false
			continue
		}
		rows, err := db.Query(`SELECT id, COALESCE(cwd, ''), COALESCE(rollout_path, ''), COALESCE(first_user_message, ''), COALESCE(updated_at, 0) FROM threads`)
		if err != nil {
			complete = false
			_ = db.Close()
			continue
		}
		for rows.Next() {
			var t CodexThread
			if rows.Scan(&t.ID, &t.CWD, &t.Rollout, &t.FirstUser, &t.Updated) != nil {
				complete = false
				continue
			}
			if t.ID == "" || seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			out = append(out, t)
		}
		if rows.Err() != nil {
			complete = false
		}
		_ = rows.Close()
		_ = db.Close()
	}
	return out, complete
}

// CodexLiveRollouts returns a copy of rows with Rollout cleared where the
// file does not exist.
func CodexLiveRollouts(rows []CodexThread) []CodexThread {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CodexThread, len(rows))
	copy(out, rows)
	for i := range out {
		if out[i].Rollout != "" && !fileExists(out[i].Rollout) {
			out[i].Rollout = ""
		}
	}
	return out
}

// CodexStateRollouts lists rollout files that still exist.
func CodexStateRollouts(home string) []string {
	var out []string
	for _, t := range CodexStateThreads(home) {
		if t.Rollout != "" {
			out = append(out, t.Rollout)
		}
	}
	return out
}
