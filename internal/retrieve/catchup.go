package retrieve

import (
	"os"

	"lossless/internal/write"
)

// maybeCatchUp copies any complete session lines that hooks/watch have
// not ingested yet so ask packs this session's latest claims.
func (e Engine) maybeCatchUp(req Request) {
	if e.Store == nil || req.SessionID == "" {
		return
	}
	sess, ok := e.Store.SessionByID(req.SessionID)
	if !ok || sess.JSONL == "" {
		return
	}
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
