package write

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"lossless/internal/env"
)

type PushJob struct {
	Project   string `json:"project"`
	Harness   string `json:"harness"`
	SessionID string `json:"session_id"`
	Client    string `json:"client"`
	Workspace string `json:"workspace"`
	Source    string `json:"source"`
	PrevOff   int64  `json:"prev_offset"`
	Body      string `json:"body"`
}

func HomeURL() string {
	return env.BaseURL()
}

func HomeIsRemote() bool {
	u := HomeURL()
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return remoteHost(parsed.Hostname())
}

func remoteHost(host string) bool {
	if host == "" || host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}
	return true
}

// CheckHomeURL rejects a non-loopback LOSSLESS_URL that is not https.
func CheckHomeURL() error {
	return CheckRemoteURL(HomeURL())
}

// CheckRemoteURL allows empty and loopback http. Anything else must be https.
func CheckRemoteURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("URL must be http or https")
	}
	if !remoteHost(parsed.Hostname()) {
		return nil
	}
	if scheme != "https" {
		return fmt.Errorf("remote URL must be https")
	}
	return nil
}

func errNoRedirect(*http.Request, []*http.Request) error {
	return fmt.Errorf("redirects disabled")
}

func outboundClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: errNoRedirect}
}

func ClientID(home string) string {
	if id := env.Client(); id != "" {
		return id
	}
	p := filepath.Join(home, "client_id")
	if b, err := os.ReadFile(p); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	id := fmt.Sprintf("c-%d", time.Now().UnixNano())
	_ = os.WriteFile(p, []byte(id+"\n"), 0o600)
	return id
}

func WritePush(home string, job PushJob) (string, error) {
	if err := os.MkdirAll(SpoolDir(home), 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("push-%d-%s.json", time.Now().UnixNano(), sanitizeName(job.SessionID))
	dest := filepath.Join(SpoolDir(home), name)
	b, err := json.Marshal(job)
	if err != nil {
		return "", err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	return dest, os.Rename(tmp, dest)
}

func ListPush(home string) ([]string, error) {
	ents, err := os.ReadDir(SpoolDir(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "push-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		out = append(out, filepath.Join(SpoolDir(home), e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func FlushPush(home string) (int, error) {
	if HomeURL() == "" {
		return 0, nil
	}
	if err := CheckHomeURL(); err != nil {
		return 0, err
	}
	files, err := ListPush(home)
	if err != nil {
		return 0, err
	}
	n := 0
	var first error
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		var job PushJob
		if err := json.Unmarshal(b, &job); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := postAppend(job); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := os.Remove(p); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		n++
	}
	return n, first
}

func MaybeEnqueuePush(home string, req CatchUpRequest, body string, prev int64) {
	if !HomeIsRemote() || strings.TrimSpace(body) == "" {
		return
	}
	if err := CheckHomeURL(); err != nil {
		return
	}
	_, _ = WritePush(home, PushJob{
		Project:   req.Project,
		Harness:   req.Harness,
		SessionID: req.SessionID,
		Client:    ClientID(home),
		Workspace: req.WorkspaceRoot,
		Source:    req.Source,
		PrevOff:   prev,
		Body:      body,
	})
}

func postAppend(job PushJob) error {
	_, err := PostAppend(HomeURL(), env.Token(), job)
	return err
}

func PostAppend(base, token string, job PushJob) (AppendResult, error) {
	var out AppendResult
	base = env.CanonicalURL(base)
	if base == "" {
		return out, fmt.Errorf("no home URL")
	}
	if err := CheckRemoteURL(base); err != nil {
		return out, err
	}
	u := base + "/v1/append"
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader([]byte(job.Body)))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("X-Project", job.Project)
	req.Header.Set("X-Harness", job.Harness)
	req.Header.Set("X-Session", job.SessionID)
	req.Header.Set("X-Client", job.Client)
	req.Header.Set("X-Prev-Offset", strconv.FormatInt(job.PrevOff, 10))
	if job.Workspace != "" {
		req.Header.Set("X-Workspace", job.Workspace)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := outboundClient(10 * time.Second)
	res, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	_ = json.Unmarshal(b, &out)
	if res.StatusCode == http.StatusConflict {
		// Home already has this prefix — skip ahead. If we are ahead of
		// home, keep the job so a later flush can retry after earlier chunks.
		if job.PrevOff < out.AcceptedThrough {
			return out, nil
		}
		return out, fmt.Errorf("append conflict: home at %d, job at %d", out.AcceptedThrough, job.PrevOff)
	}
	if res.StatusCode != http.StatusOK {
		return out, fmt.Errorf("append %d: %s", res.StatusCode, b)
	}
	return out, nil
}
