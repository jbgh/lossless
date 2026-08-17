package write

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lossless/internal/claim"
	"lossless/internal/projectkey"
	"lossless/internal/redact"
	"lossless/internal/store"
)

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
	rec.Paths = redact.FilterPaths(rec.Paths)
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
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o700); err != nil {
		return out, err
	}
	line, _ := json.Marshal(map[string]any{
		"type": "remember", "role": "user", "text": rec.Text, "claim_type": rec.Type,
	})
	f, err := os.OpenFile(rawPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
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
