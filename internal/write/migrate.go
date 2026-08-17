package write

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lossless/internal/env"
	"lossless/internal/projectkey"
	"lossless/internal/store"
)

const migrateChunk = 1 << 20

type Tape struct {
	Project   string
	SessionID string
	Harness   string
	Workspace string
	Paths     []string
}

type MigrateOpts struct {
	DataHome string
	URL      string
	Token    string
	Push     bool
}

type MigrateResult struct {
	URL     string
	Tapes   int
	Pushed  int
	Bytes   int64
	Skipped int
	Auth    string
	Errors  []string
}

func (r MigrateResult) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "home    %s\n", r.URL)
	if r.Auth != "" {
		fmt.Fprintf(&b, "auth    %s\n", r.Auth)
	}
	fmt.Fprintf(&b, "tapes   %d\n", r.Tapes)
	fmt.Fprintf(&b, "pushed  %d sessions, %d bytes\n", r.Pushed, r.Bytes)
	if r.Skipped > 0 {
		fmt.Fprintf(&b, "skipped %d already on home\n", r.Skipped)
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "error   %s\n", e)
	}
	return b.String()
}

func ProbeHome(base, token string) error {
	base = env.CanonicalURL(base)
	if base == "" {
		return fmt.Errorf("no home URL")
	}
	if err := CheckRemoteURL(base); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"project": "lossless/migrate", "question": "ping"})
	req, err := http.NewRequest(http.MethodPost, base+"/v1/ask", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := outboundClient(5 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized — check LOSSLESS_TOKEN")
	}
	if res.StatusCode >= 500 {
		return fmt.Errorf("home status %d", res.StatusCode)
	}
	return nil
}

func Migrate(opts MigrateOpts) (MigrateResult, error) {
	var out MigrateResult
	base := env.CanonicalURL(opts.URL)
	if base == "" {
		return out, fmt.Errorf("url required")
	}
	if err := CheckRemoteURL(base); err != nil {
		return out, err
	}
	if parsed, err := url.Parse(base); err == nil && remoteHost(parsed.Hostname()) && opts.Token == "" {
		return out, fmt.Errorf("token required for remote home")
	}
	out.URL = base
	if err := ProbeHome(base, opts.Token); err != nil {
		return out, err
	}
	out.Auth = "ok"

	tapes, err := ListRawTapes(opts.DataHome)
	if err != nil {
		return out, err
	}
	if st, e := store.Open(opts.DataHome); e == nil {
		hydrateTapes(st, tapes)
		_ = st.Close()
	}
	out.Tapes = len(tapes)
	if !opts.Push {
		return out, nil
	}
	client := ClientID(opts.DataHome)
	for _, tape := range tapes {
		n, skipped, err := pushTape(base, opts.Token, client, tape)
		out.Bytes += n
		if skipped {
			out.Skipped++
		} else if n > 0 {
			out.Pushed++
		}
		if err != nil {
			out.Errors = append(out.Errors, tape.SessionID+": "+err.Error())
		}
	}
	if len(out.Errors) > 0 {
		return out, fmt.Errorf("pushed with %d errors", len(out.Errors))
	}
	return out, nil
}

func ListRawTapes(root string) ([]Tape, error) {
	rawRoot := filepath.Join(root, "raw")
	st, err := os.Lstat(rawRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("raw/ must not be a symlink")
	}
	grouped := map[string]*Tape{}
	err = filepath.WalkDir(rawRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".jsonl.zst") {
			return nil
		}
		rel, err := filepath.Rel(rawRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return nil
		}
		project, session, ok := tapeFromRel(rel)
		if !ok {
			return nil
		}
		key := project + "\x00" + session
		t := grouped[key]
		if t == nil {
			t = &Tape{Project: project, SessionID: session, Harness: "other"}
			grouped[key] = t
		}
		t.Paths = append(t.Paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []Tape
	for _, t := range grouped {
		sort.Strings(t.Paths)
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out, nil
}

func tapeFromRel(rel string) (project, session string, ok bool) {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return "", "", false
	}
	project = projectkey.Decode(parts[0])
	if project == "" || project == "unknown" {
		return "", "", false
	}
	base := parts[len(parts)-1]
	base = strings.TrimSuffix(base, ".zst")
	base = strings.TrimSuffix(base, ".jsonl")
	if i := strings.LastIndex(base, ".part"); i > 0 {
		base = base[:i]
	}
	if base == "" {
		return "", "", false
	}
	return project, base, true
}

func hydrateTapes(st *store.Store, tapes []Tape) {
	sess, err := st.ListSessions()
	if err != nil {
		return
	}
	byID := map[string]store.Session{}
	for _, s := range sess {
		if s.SessionID != "" {
			byID[s.SessionID] = s
		}
	}
	for i := range tapes {
		if s, ok := byID[tapes[i].SessionID]; ok {
			if s.Harness != "" {
				tapes[i].Harness = s.Harness
			}
			tapes[i].Workspace = s.Workspace
			if s.Project != "" {
				tapes[i].Project = s.Project
			}
		}
	}
}

func pushTape(base, token, client string, tape Tape) (int64, bool, error) {
	var buf []byte
	for _, p := range tape.Paths {
		b, err := ReadRaw(p)
		if err != nil {
			return 0, false, err
		}
		buf = append(buf, b...)
	}
	if len(buf) == 0 {
		return 0, false, nil
	}
	if !bytes.HasSuffix(buf, []byte("\n")) {
		buf = append(buf, '\n')
	}
	prev := int64(0)
	var sent int64
	skipped := false
	for prev < int64(len(buf)) {
		chunk := nextChunk(buf[prev:], migrateChunk)
		if len(chunk) == 0 {
			break
		}
		res, err := PostAppend(base, token, PushJob{
			Project:   tape.Project,
			Harness:   tape.Harness,
			SessionID: tape.SessionID,
			Client:    client,
			Workspace: tape.Workspace,
			Source:    "migrate",
			PrevOff:   prev,
			Body:      string(chunk),
		})
		if err != nil {
			return sent, skipped, err
		}
		if res.Conflict && res.AcceptedThrough > prev {
			skipped = true
			prev = res.AcceptedThrough
			continue
		}
		if res.AcceptedThrough > prev {
			sent += res.AcceptedThrough - prev
			prev = res.AcceptedThrough
			continue
		}
		prev += int64(len(chunk))
		sent += int64(len(chunk))
	}
	return sent, skipped, nil
}

func nextChunk(b []byte, max int) []byte {
	if len(b) == 0 {
		return nil
	}
	if len(b) <= max {
		if bytes.HasSuffix(b, []byte("\n")) {
			return b
		}
		return append(append([]byte(nil), b...), '\n')
	}
	cut := bytes.LastIndexByte(b[:max], '\n')
	if cut < 0 {
		nl := bytes.IndexByte(b, '\n')
		if nl < 0 {
			return append(append([]byte(nil), b...), '\n')
		}
		cut = nl
	}
	return b[:cut+1]
}
