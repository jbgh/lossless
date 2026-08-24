package retrieve

import (
	"os"
	"path/filepath"

	"lossless/internal/harness"
	"lossless/internal/projectkey"
	"lossless/internal/store"
	"lossless/internal/write"
)

const (
	// Omitted-sid ask is a delta on rows the store already knows.
	// Never first-ingest a 17 MB unknown file on this path.
	catchUpStoredMaxSessions   = 8
	catchUpStoredMaxFirstBytes = 1 << 20
	catchUpStoredMaxDeltaBytes = 2 << 20
)

// maybeCatchUp copies complete session lines that hooks/watch have not
// ingested yet so ask packs this session's latest claims.
//
// Store-first: omitted session_id catch-up stored sessions for this
// owner/repo that are behind, with a budget. A set-but-unknown
// session_id is exact locate only. Never walk a harness home for
// newest mtime. CatchUp always receives the real session id.
func (e Engine) maybeCatchUp(req Request) {
	if e.Store == nil {
		return
	}
	if req.SessionID != "" {
		if sess, ok := e.Store.SessionByID(req.SessionID); ok && sess.JSONL != "" {
			e.catchUpKnown(req, sess)
			return
		}
		e.catchUpLocated(req)
		return
	}
	e.catchUpStoredWorkspace(req)
}

func (e Engine) catchUpKnown(req Request, sess store.Session) {
	fi, err := os.Stat(sess.JSONL)
	if err != nil || e.Store.Cursor(sess.JSONL) == fi.Size() {
		return
	}
	project := sess.Project
	if project == "" {
		project = req.Project
	}
	ws := sess.Workspace
	if ws == "" {
		ws = req.WorkspaceRoot
	}
	_, _ = write.CatchUp(e.Store, write.CatchUpRequest{
		JSONL: sess.JSONL, Project: project, WorkspaceRoot: ws,
		Harness: sess.Harness, SessionID: sess.SessionID, Source: "turn",
	})
}

func (e Engine) catchUpLocated(req Request) {
	if req.WorkspaceRoot == "" || req.SessionID == "" {
		return
	}
	jsonl, hid, harn := locateExact(req.WorkspaceRoot, req.SessionID)
	if jsonl == "" || hid == "" {
		return
	}
	project := req.Project
	if project == "" {
		project = projectkey.FromWorkspace(req.WorkspaceRoot)
	}
	_, _ = write.CatchUp(e.Store, write.CatchUpRequest{
		JSONL: jsonl, Project: project, WorkspaceRoot: req.WorkspaceRoot,
		Harness: harn, SessionID: hid, Source: "turn",
	})
}

func locateExact(ws, sid string) (jsonl, sessionID, harn string) {
	if p := harness.LocateGrok(ws, sid).JSONL; fileRegular(p) {
		return p, sid, "grok"
	}
	if p := harness.LocateClaude("", sid, ws).JSONL; fileRegular(p) {
		return p, sid, "claude"
	}
	// Codex/Pi must not receive cwd: with cwd they fall back to newest-mtime.
	if loc := harness.LocateCodex("", sid, ""); fileRegular(loc.JSONL) {
		hid := loc.SessionID
		if hid == "" {
			hid = sid
		}
		return loc.JSONL, hid, "codex"
	}
	if loc := harness.LocatePi("", sid, ""); fileRegular(loc.JSONL) {
		hid := loc.SessionID
		if hid == "" {
			hid = sid
		}
		return loc.JSONL, hid, "pi"
	}
	return "", "", ""
}

func fileRegular(p string) bool {
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func (e Engine) catchUpStoredWorkspace(req Request) {
	sess, err := e.Store.ListSessions()
	if err != nil {
		return
	}
	proj := projectkey.Normalize(req.Project)
	if proj == "" && req.WorkspaceRoot != "" {
		proj = projectkey.FromWorkspace(req.WorkspaceRoot)
	}
	ws := ""
	if req.WorkspaceRoot != "" {
		ws = filepath.Clean(req.WorkspaceRoot)
	}
	var same, other []store.Session
	for _, s := range sess {
		if s.JSONL == "" || s.SessionID == "" {
			continue
		}
		if proj != "" {
			if projectkey.Normalize(s.Project) != proj {
				continue
			}
		} else if ws == "" || filepath.Clean(s.Workspace) != ws {
			continue
		}
		if ws != "" && filepath.Clean(s.Workspace) == ws {
			same = append(same, s)
		} else {
			other = append(other, s)
		}
	}
	var n int
	var copied int64
	for _, s := range append(same, other...) {
		if n >= catchUpStoredMaxSessions {
			break
		}
		delta, skip := storedCatchUpDelta(e.Store, s.JSONL)
		if skip {
			continue
		}
		if copied+delta > catchUpStoredMaxDeltaBytes && n > 0 {
			continue
		}
		e.catchUpKnown(req, s)
		n++
		copied += delta
	}
}

func storedCatchUpDelta(st *store.Store, jsonl string) (delta int64, skip bool) {
	fi, err := os.Stat(jsonl)
	if err != nil || !fi.Mode().IsRegular() {
		return 0, true
	}
	size := fi.Size()
	cur := st.Cursor(jsonl)
	if cur == size {
		return 0, true
	}
	if cur > size {
		// Shrink: CatchUp resets. Do not apply the first-ingest cap.
		return size, false
	}
	if cur == 0 && size > catchUpStoredMaxFirstBytes {
		return 0, true
	}
	d := size - cur
	if d < 0 {
		d = 0
	}
	return d, false
}
