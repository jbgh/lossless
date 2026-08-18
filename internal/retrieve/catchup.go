package retrieve

import (
	"os"
	"path/filepath"

	"lossless/internal/harness"
	"lossless/internal/projectkey"
	"lossless/internal/store"
	"lossless/internal/write"
)

// maybeCatchUp copies complete session lines that hooks/watch have not
// ingested yet so ask packs this session's latest claims.
//
// Store-first: omitted session_id catch-up stored sessions for this
// workspace that are behind. A set-but-unknown session_id is exact
// locate only. Never walk a harness home for newest mtime.
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
	ws := filepath.Clean(req.WorkspaceRoot)
	proj := projectkey.Normalize(req.Project)
	if proj == "" && req.WorkspaceRoot != "" {
		proj = projectkey.FromWorkspace(req.WorkspaceRoot)
	}
	for _, s := range sess {
		if s.JSONL == "" || s.SessionID == "" {
			continue
		}
		if req.WorkspaceRoot != "" {
			if filepath.Clean(s.Workspace) != ws {
				continue
			}
		} else if proj == "" || projectkey.Normalize(s.Project) != proj {
			continue
		}
		e.catchUpKnown(req, s)
	}
}
