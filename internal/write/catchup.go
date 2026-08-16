package write

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
	"lossless/internal/redact"
	"lossless/internal/store"
)

type CatchUpRequest struct {
	JSONL         string
	Project       string
	WorkspaceRoot string
	Harness       string
	SessionID     string
	Source        string
	Messages      []map[string]any
}

type CatchUpResult struct {
	Copied     int64    `json:"copied"`
	Extracted  int      `json:"extracted"`
	IDs        []string `json:"ids"`
	Superseded []string `json:"superseded"`
	RawPath    string   `json:"raw_path"`
	Sealed     string   `json:"sealed,omitempty"`
	Noop       bool     `json:"noop"`
}

func CatchUp(st *store.Store, req CatchUpRequest) (CatchUpResult, error) {
	var out CatchUpResult
	if req.JSONL == "" {
		if p, err := materializeSource(st, &req); err != nil {
			return out, err
		} else if p != "" {
			req.JSONL = p
		} else {
			return out, fmt.Errorf("path_to_jsonl required")
		}
	}
	project := req.Project
	if project == "" && req.WorkspaceRoot != "" {
		project = projectkey.FromWorkspace(req.WorkspaceRoot)
	}
	if project == "" {
		return out, fmt.Errorf("project or workspace_root required")
	}
	project = projectkey.Normalize(project)
	if req.Harness == "" {
		req.Harness = "other"
	}
	if req.Source == "" {
		req.Source = "compact"
	}
	session := req.SessionID
	if session == "" {
		session = strings.TrimSuffix(filepath.Base(req.JSONL), ".jsonl")
	}

	info, err := os.Stat(req.JSONL)
	if err != nil {
		return out, err
	}
	srcOff := st.Cursor(req.JSONL)
	if srcOff >= info.Size() {
		out.Noop = true
		out.RawPath = st.LiveRawPath(project, session, info.ModTime())
		_ = st.UpsertSession(store.Session{
			JSONL: req.JSONL, SessionID: session, Harness: req.Harness,
			Workspace: req.WorkspaceRoot, Project: project,
		})
		if req.Source == "session_end" {
			if z, err := SealRaw(out.RawPath); err == nil {
				out.Sealed = z
			}
		}
		return out, nil
	}

	src, err := os.Open(req.JSONL)
	if err != nil {
		return out, err
	}
	defer src.Close()
	if _, err := src.Seek(srcOff, io.SeekStart); err != nil {
		return out, err
	}
	buf, err := io.ReadAll(src)
	if err != nil {
		return out, err
	}

	now := time.Now()
	rawPath := st.LiveRawPath(project, session, now)
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o755); err != nil {
		return out, err
	}
	raw, err := os.OpenFile(rawPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return out, err
	}
	if err := syscall.Flock(int(raw.Fd()), syscall.LOCK_EX); err != nil {
		_ = raw.Close()
		return out, err
	}
	unlock := func() {
		_ = syscall.Flock(int(raw.Fd()), syscall.LOCK_UN)
		_ = raw.Close()
	}

	var clean strings.Builder
	rest := string(buf)
	// keep only complete lines in this catch-up
	if !strings.HasSuffix(rest, "\n") {
		if i := strings.LastIndex(rest, "\n"); i >= 0 {
			rest = rest[:i+1]
		} else {
			// wait for a full line
			unlock()
			return out, nil
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(rest, "\n"), "\n") {
		clean.WriteString(redact.Line(line))
	}
	written, err := raw.WriteString(clean.String())
	if err != nil {
		unlock()
		return out, err
	}
	if err := raw.Sync(); err != nil {
		unlock()
		return out, err
	}
	unlock()

	consumed := int64(len(rest))
	if err := st.SetCursor(req.JSONL, srcOff+consumed); err != nil {
		return out, err
	}
	out.Copied = int64(written)
	out.RawPath = rawPath

	msgs, _ := ParseJSONL(clean.String(), 0)
	if xs := ChunkExcerpts(session, project, msgs); len(xs) > 0 {
		if err := st.WriteExcerpts(now.UTC().Format("2006-01"), xs); err != nil {
			return out, err
		}
	}
	recs := Extract(msgs, ExtractOpts{
		ProjectKey:    project,
		WorkspaceRoot: req.WorkspaceRoot,
		Harness:       req.Harness,
		SessionID:     session,
		Source:        req.Source,
	})
	for _, rec := range recs {
		sup, err := st.WriteClaim(rec)
		if err != nil {
			return out, err
		}
		out.IDs = append(out.IDs, rec.ID)
		if sup != "" {
			out.Superseded = append(out.Superseded, sup)
		}
	}
	if err := st.UpsertSession(store.Session{
		JSONL: req.JSONL, SessionID: session, Harness: req.Harness,
		Workspace: req.WorkspaceRoot, Project: project,
	}); err != nil {
		return out, err
	}
	out.Extracted = len(out.IDs)
	if req.Source == "session_end" {
		if z, err := SealRaw(rawPath); err != nil {
			return out, err
		} else {
			out.Sealed = z
		}
	}
	enqueueHomePush(st, CatchUpRequest{
		Project: project, Harness: req.Harness, SessionID: session,
		WorkspaceRoot: req.WorkspaceRoot, Source: req.Source,
	}, clean.String())
	return out, nil
}

func enqueueHomePush(st *store.Store, req CatchUpRequest, body string) {
	if !HomeIsRemote() || body == "" || req.SessionID == "" {
		return
	}
	key := "home-push:" + req.SessionID
	prev := st.Cursor(key)
	MaybeEnqueuePush(st.Root, req, body, prev)
	_ = st.SetCursor(key, prev+int64(len(body)))
}

func materializeSource(st *store.Store, req *CatchUpRequest) (string, error) {
	if len(req.Messages) > 0 {
		return writeVirtualJSONL(st, req.SessionID, req.Messages)
	}
	if req.Harness == "opencode" && req.SessionID != "" {
		return dumpOpenCode(st, req)
	}
	return "", nil
}

type virtualLine struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content any    `json:"content,omitempty"`
}

type virtualPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func stabilizeContent(v any) any {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var out []virtualPart
		for _, p := range t {
			m, ok := p.(map[string]any)
			if !ok {
				if s, ok := p.(string); ok {
					out = append(out, virtualPart{Type: "text", Text: s})
				}
				continue
			}
			out = append(out, virtualPart{
				Type: stringField(m, "type", "text"),
				Text: stringField(m, "text", ""),
			})
		}
		return out
	default:
		return v
	}
}

func writeVirtualJSONL(st *store.Store, session string, msgs []map[string]any) (string, error) {
	if session == "" {
		session = "messages"
	}
	dir := filepath.Join(st.Root, "spool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "virtual-"+sanitizeName(session)+".jsonl")
	var b strings.Builder
	for _, m := range msgs {
		line, err := json.Marshal(virtualLine{
			Type:    stringField(m, "type", "message"),
			Role:    stringField(m, "role", "other"),
			Content: stabilizeContent(m["content"]),
		})
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(dest, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func stringField(m map[string]any, key, fallback string) string {
	if s, ok := m[key].(string); ok && s != "" {
		return s
	}
	return fallback
}

func dumpOpenCode(st *store.Store, req *CatchUpRequest) (string, error) {
	db := os.Getenv("OPENCODE_DB")
	if db == "" {
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(os.Getenv("HOME"), ".local", "share")
		}
		db = filepath.Join(data, "opencode", "opencode.db")
	}
	cwd, lines, err := ReadOpenCodeSession(db, req.SessionID)
	if err != nil {
		return "", err
	}
	if req.WorkspaceRoot == "" {
		req.WorkspaceRoot = cwd
	}
	return writeVirtualJSONL(st, req.SessionID, lines)
}

func Remember(st *store.Store, rec claim.Record) (CatchUpResult, error) {
	var out CatchUpResult
	if rec.Text == "" || rec.Type == "" {
		return out, fmt.Errorf("type and text required")
	}
	if rec.ProjectKey == "" && rec.WorkspaceRoot != "" {
		rec.ProjectKey = projectkey.FromWorkspace(rec.WorkspaceRoot)
	}
	if rec.ProjectKey == "" {
		return out, fmt.Errorf("project or workspace_root required")
	}
	rec.ProjectKey = projectkey.Normalize(rec.ProjectKey)
	if rec.Harness == "" {
		rec.Harness = "other"
	}
	if rec.Source == "" {
		rec.Source = "remember"
	}
	if rec.SessionID == "" {
		rec.SessionID = "manual"
	}
	if rec.ClaimHash == "" {
		rec.ClaimHash = claim.Hash(rec.ProjectKey, rec.Type, rec.Text)
	}
	if rec.ID == "" {
		rec.ID = claim.NewID()
	}

	rawPath := st.ManualRawPath(time.Now())
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o755); err != nil {
		return out, err
	}
	line, _ := json.Marshal(map[string]any{
		"type": "remember", "role": "user", "text": rec.Text, "claim_type": rec.Type,
	})
	f, err := os.OpenFile(rawPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return out, err
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
	out.RawPath = rawPath

	sup, err := st.WriteClaim(rec)
	if err != nil {
		return out, err
	}
	out.IDs = []string{rec.ID}
	out.Extracted = 1
	if sup != "" {
		out.Superseded = []string{sup}
	}
	_ = st.AppendActions([]store.Action{{
		ProjectKey: rec.ProjectKey,
		SessionID:  rec.SessionID,
		Kind:       store.ActionRemember,
		ClaimID:    rec.ID,
		Paths:      rec.Paths,
		Tokens:     claim.Tokens(rec.Text),
		At:         time.Now().UTC().Format(time.RFC3339),
	}})
	return out, nil
}
