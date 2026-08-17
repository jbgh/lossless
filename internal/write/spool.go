package write

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lossless/internal/store"
)

// SpoolJob is a durable catch-up request written when the sidecar is down.
type SpoolJob struct {
	JSONL         string `json:"jsonl"`
	Project       string `json:"project"`
	WorkspaceRoot string `json:"workspace_root"`
	Harness       string `json:"harness"`
	SessionID     string `json:"session_id"`
	Source        string `json:"source"`
	CreatedAt     string `json:"created_at"`
}

type EnsureResult struct {
	Replayed int      `json:"replayed"`
	Failed   int      `json:"failed"`
	Skipped  int      `json:"skipped"`
	Pushed   int      `json:"pushed"`
	Errors   []string `json:"errors,omitempty"`
}

func SpoolDir(home string) string {
	return filepath.Join(home, "spool")
}

func WriteSpool(home string, job SpoolJob) (string, error) {
	if job.CreatedAt == "" {
		job.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := os.MkdirAll(SpoolDir(home), 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), sanitizeName(job.SessionID))
	dest := filepath.Join(SpoolDir(home), name)
	b, err := json.Marshal(job)
	if err != nil {
		return "", err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func ListSpool(home string) ([]string, error) {
	ents, err := os.ReadDir(SpoolDir(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if strings.HasPrefix(e.Name(), "push-") {
			continue
		}
		out = append(out, filepath.Join(SpoolDir(home), e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// Ensure replays spool files into the local store, then flushes any home-push queue.
func Ensure(st *store.Store, home string) (EnsureResult, error) {
	var out EnsureResult
	files, err := ListSpool(home)
	if err != nil {
		return out, err
	}
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			out.Failed++
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		var job SpoolJob
		if err := json.Unmarshal(b, &job); err != nil {
			out.Failed++
			out.Errors = append(out.Errors, p+": "+err.Error())
			continue
		}
		if job.JSONL == "" && job.SessionID == "" {
			out.Skipped++
			_ = os.Remove(p)
			continue
		}
		_, err = CatchUp(st, CatchUpRequest{
			JSONL: job.JSONL, Project: job.Project, WorkspaceRoot: job.WorkspaceRoot,
			Harness: job.Harness, SessionID: job.SessionID, Source: job.Source,
		})
		if err != nil {
			out.Failed++
			out.Errors = append(out.Errors, p+": "+err.Error())
			continue
		}
		if err := os.Remove(p); err != nil {
			out.Failed++
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		out.Replayed++
	}
	pushed, perr := FlushPush(home)
	out.Pushed = pushed
	if perr != nil {
		out.Errors = append(out.Errors, perr.Error())
	}
	return out, nil
}

func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '-'
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "..", "_")
	if s == "" {
		return "job"
	}
	if len(s) > 40 {
		return s[:40]
	}
	return s
}
