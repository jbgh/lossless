package write

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"lossless/internal/env"
	"lossless/internal/store"
)

func SidecarURL() string { return env.Sidecar() }

func MemoryHome() string { return env.Home() }

// Compact must finish the raw copy before the harness shrinks the window.
// Turn hooks stay short so they do not stall the UI.
func catchUpWait(source string) time.Duration {
	s := strings.ToLower(strings.TrimSpace(source))
	if strings.Contains(s, "compact") {
		return 5 * time.Second
	}
	if s == "session_end" || s == "sessionend" || s == "session_shutdown" {
		return 3 * time.Second
	}
	return 400 * time.Millisecond
}

// SubmitCatchUp is the hook path: sidecar HTTP, then local store, then spool.
// If the sidecar timed out it may already be writing — spool and let --ensure
// replay (a no-op if the cursor moved). Never open the store in that case.
func SubmitCatchUp(req CatchUpRequest) {
	var err error
	if side := SidecarURL(); side != "" {
		err = postCatchUp(side, req, catchUpWait(req.Source))
		if err == nil {
			return
		}
	}
	home := MemoryHome()
	if uncertainSidecar(err) {
		_, _ = WriteSpool(home, spoolFrom(req))
		return
	}
	st, err := store.Open(home)
	if err != nil {
		_, _ = WriteSpool(home, spoolFrom(req))
		return
	}
	store.AttachEmbedder(st, home)
	defer st.Close()
	if _, err := CatchUp(st, req); err != nil {
		_, _ = WriteSpool(home, spoolFrom(req))
	}
}

func uncertainSidecar(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "status 5") || strings.Contains(s, "status 429")
}

func postCatchUp(base string, req CatchUpRequest, timeout time.Duration) error {
	body, err := json.Marshal(map[string]any{
		"jsonl":          req.JSONL,
		"path_to_jsonl":  req.JSONL,
		"project":        req.Project,
		"workspace_root": req.WorkspaceRoot,
		"harness":        req.Harness,
		"session_id":     req.SessionID,
		"source":         req.Source,
		"messages":       req.Messages,
	})
	if err != nil {
		return err
	}
	if err := CheckRemoteURL(base); err != nil {
		return err
	}
	httpReq, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/v1/catch-up", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := outboundClient(timeout)
	res, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("catch-up status %d", res.StatusCode)
	}
	return nil
}

func spoolFrom(req CatchUpRequest) SpoolJob {
	return SpoolJob{
		JSONL:         req.JSONL,
		Project:       req.Project,
		WorkspaceRoot: req.WorkspaceRoot,
		Harness:       req.Harness,
		SessionID:     req.SessionID,
		Source:        req.Source,
	}
}
