// Package debuglog is a local JSONL of ask/catch-up identity.
// It is not uploaded. doctor does not phone home.
package debuglog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxBytes = 8 << 20
	tailMax  = 24
)

// Event is one local debug line. No claim text.
type Event struct {
	At         string         `json:"at"`
	Kind       string         `json:"kind"`
	Project    string         `json:"project,omitempty"`
	Workspace  string         `json:"workspace,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
	SessionSet bool           `json:"session_set"`
	Identity   string         `json:"identity,omitempty"`
	Hits       int            `json:"hits"`
	Warns      int            `json:"warns"`
	Copied     int64          `json:"copied,omitempty"`
	Extracted  int            `json:"extracted,omitempty"`
	Messages   int            `json:"messages,omitempty"`
	Sentences  int            `json:"sentences,omitempty"`
	Kept       int            `json:"kept,omitempty"`
	Skip       map[string]int `json:"skip,omitempty"`
	Noop       bool           `json:"noop,omitempty"`
}

func path(home string) string {
	return filepath.Join(home, "debug", "events.jsonl")
}

// Path is the on-disk log. Empty home is ignored.
func Path(home string) string {
	if home == "" {
		return ""
	}
	return path(home)
}

// Identity is how the project key was chosen: given, origin, or path.
func Identity(givenProject, resolved string) string {
	if strings.TrimSpace(givenProject) != "" {
		return "given"
	}
	if strings.HasPrefix(resolved, "path-") {
		return "path"
	}
	if resolved == "" {
		return "empty"
	}
	return "origin"
}

// Append writes one event. Fail-open.
func Append(home string, ev Event) {
	if home == "" || ev.Kind == "" {
		return
	}
	if ev.At == "" {
		ev.At = time.Now().UTC().Format(time.RFC3339)
	}
	p := path(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	rotate(p)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		_ = f.Close()
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
}

func rotate(p string) {
	fi, err := os.Stat(p)
	if err != nil || fi.Size() < maxBytes {
		return
	}
	_ = os.Rename(p, p+".1")
}

// Tail returns the last n events, newest last. project filters if set.
func Tail(home string, n int, project string) []Event {
	if n <= 0 {
		n = tailMax
	}
	p := path(home)
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var raw []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		raw = append(raw, line)
		if len(raw) > n*8 {
			raw = raw[len(raw)-n*4:]
		}
	}
	var out []Event
	for _, line := range raw {
		var ev Event
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if project != "" && ev.Project != project && ev.Project != "" {
			continue
		}
		out = append(out, ev)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}
