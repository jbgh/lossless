package write

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"lossless/internal/projectkey"
	"lossless/internal/redact"
	"lossless/internal/store"
)

type AppendRequest struct {
	Project   string
	Harness   string
	SessionID string
	Client    string
	Workspace string
	Source    string
	PrevOff   int64
	Body      []byte
}

type AppendResult struct {
	AcceptedThrough int64 `json:"accepted_through"`
	Extracted       int   `json:"extracted"`
	Conflict        bool  `json:"conflict,omitempty"`
	Noop            bool  `json:"noop,omitempty"`
}

func appendCursorKey(client, session string) string {
	if client == "" {
		client = "default"
	}
	return "append:" + client + ":" + session
}

// Append is the home ingest path. Sidecars POST incremental redacted JSONL.
// Idempotent: same X-Prev-Offset after a successful ack is a 409 with the
// current accepted_through so the client retries from there.
func Append(st *store.Store, req AppendRequest) (AppendResult, error) {
	var out AppendResult
	if req.SessionID == "" {
		return out, fmt.Errorf("session required")
	}
	if req.Project == "" {
		return out, fmt.Errorf("project required")
	}
	req.Project = projectkey.Normalize(req.Project)
	if req.Harness == "" {
		req.Harness = "other"
	}
	if req.Source == "" {
		req.Source = "append"
	}
	// Serialize check-and-set with other appends and catch-up: the
	// conflict check reads the cursor that only advances after the write.
	unlock, err := lockSession(st, req.Project, req.SessionID)
	if err != nil {
		return out, err
	}
	defer unlock()
	key := appendCursorKey(req.Client, req.SessionID)
	body := req.Body
	if !strings.HasSuffix(string(body), "\n") {
		if i := strings.LastIndex(string(body), "\n"); i >= 0 {
			body = body[:i+1]
		} else {
			out.Noop = true
			return out, nil
		}
	}
	if len(body) == 0 {
		out.Noop = true
		return out, nil
	}

	now := time.Now()
	rawPath := st.LiveRawPath(req.Project, req.SessionID, now)
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o700); err != nil {
		return out, err
	}
	raw, err := os.OpenFile(rawPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return out, err
	}
	if err := syscall.Flock(int(raw.Fd()), syscall.LOCK_EX); err != nil {
		_ = raw.Close()
		return out, err
	}
	current := st.Cursor(key)
	out.AcceptedThrough = current
	if req.PrevOff != current {
		_ = syscall.Flock(int(raw.Fd()), syscall.LOCK_UN)
		_ = raw.Close()
		out.Conflict = true
		return out, nil
	}
	var clean strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		clean.WriteString(redact.Line(line))
	}
	rawBase := fileByteSize(raw)
	if _, err := raw.WriteString(clean.String()); err != nil {
		_ = syscall.Flock(int(raw.Fd()), syscall.LOCK_UN)
		_ = raw.Close()
		return out, err
	}
	if err := raw.Sync(); err != nil {
		_ = syscall.Flock(int(raw.Fd()), syscall.LOCK_UN)
		_ = raw.Close()
		return out, err
	}
	_ = syscall.Flock(int(raw.Fd()), syscall.LOCK_UN)
	_ = raw.Close()

	next := current + int64(len(body))
	if err := st.SetCursor(key, next); err != nil {
		return out, err
	}
	out.AcceptedThrough = next

	msgs, _ := ParseJSONL(clean.String(), rawBase)
	if xs := ChunkExcerpts(req.SessionID, req.Project, msgs); len(xs) > 0 {
		if err := st.WriteExcerpts(now.UTC().Format("2006-01"), xs); err != nil {
			return out, err
		}
	}
	recs := Extract(msgs, ExtractOpts{
		ProjectKey:    req.Project,
		WorkspaceRoot: req.Workspace,
		Harness:       req.Harness,
		SessionID:     req.SessionID,
		Source:        req.Source,
	})
	for _, rec := range recs {
		if _, err := st.WriteClaim(rec); err != nil {
			return out, err
		}
		out.Extracted++
	}
	_ = st.UpsertSession(store.Session{
		JSONL:     key,
		SessionID: req.SessionID,
		Harness:   req.Harness,
		Workspace: req.Workspace,
		Project:   req.Project,
	})
	return out, nil
}
