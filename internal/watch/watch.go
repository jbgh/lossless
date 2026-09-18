package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lossless/internal/harness"
	"lossless/internal/projectkey"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/version"
	"lossless/internal/write"
)

type Options struct {
	GrokRoot   string
	ClaudeRoot string
	CodexRoot  string
	PiRoot     string
	OpenCodeDB string
	Interval   time.Duration
	IdleSeal   time.Duration // 0 = 24h
}

type Result struct {
	Seen     int `json:"seen"`
	CatchUps int `json:"catch_ups"`
	Sealed   int `json:"sealed"`
}

type Target struct {
	JSONL     string
	Harness   string
	SessionID string
	Workspace string
	Project   string
	UpdatedAt int64
	Messages  []map[string]any
}

const sqliteTickCap = 16

func Defaults() Options {
	home := os.Getenv("HOME")
	grok := os.Getenv("GROK_HOME")
	if grok == "" {
		grok = filepath.Join(home, ".grok")
	}
	claude := os.Getenv("CLAUDE_HOME")
	if claude == "" {
		claude = filepath.Join(home, ".claude")
	}
	codex := os.Getenv("CODEX_HOME")
	if codex == "" {
		codex = filepath.Join(home, ".codex")
	}
	pi := os.Getenv("PI_HOME")
	if pi == "" {
		pi = filepath.Join(home, ".pi")
	}
	return Options{
		GrokRoot:   filepath.Join(grok, "sessions"),
		ClaudeRoot: filepath.Join(claude, "projects"),
		CodexRoot:  filepath.Join(codex, "sessions"),
		PiRoot:     filepath.Join(pi, "agent", "sessions"),
		OpenCodeDB: harness.OpenCodeDB(),
		Interval:   time.Second,
	}
}

func Discover(opts Options, known []store.Session) []Target {
	seen := map[string]bool{}
	var out []Target
	add := func(t Target) {
		key := t.JSONL
		if key == "" {
			if t.SessionID == "" || t.Harness == "" {
				return
			}
			key = t.Harness + ":" + t.SessionID
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, t)
	}
	for _, s := range known {
		if s.Harness == "claude" && claudeSubagentDump(s.JSONL) {
			continue
		}
		add(Target{JSONL: s.JSONL, Harness: s.Harness, SessionID: s.SessionID, Workspace: s.Workspace, Project: s.Project})
	}
	if opts.GrokRoot != "" {
		_ = filepath.WalkDir(opts.GrokRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if filepath.Base(path) != "chat_history.jsonl" {
				return nil
			}
			rel, _ := filepath.Rel(opts.GrokRoot, path)
			parts := strings.Split(filepath.ToSlash(rel), "/")
			ws, sid := "", ""
			if len(parts) >= 2 {
				ws = harness.DecodeGrokSessionDir(parts[0])
				sid = parts[1]
			}
			add(Target{JSONL: path, Harness: "grok", SessionID: sid, Workspace: ws})
			return nil
		})
	}
	if opts.ClaudeRoot != "" {
		_ = filepath.WalkDir(opts.ClaudeRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			// Task/workflow dumps are instruction chrome, not product tape.
			// Nested JSONL with cwd that is not under subagents/ still catch-up.
			if claudeSubagentDump(path) {
				return nil
			}
			cwd := harness.PeekClaudeCWD(path)
			if cwd == "" {
				return nil
			}
			sid := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			add(Target{JSONL: path, Harness: "claude", SessionID: sid, Workspace: cwd})
			return nil
		})
	}
	if opts.CodexRoot != "" {
		_ = filepath.WalkDir(opts.CodexRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			if !strings.HasPrefix(base, "rollout-") || !strings.HasSuffix(base, ".jsonl") {
				return nil
			}
			sid := harness.CodexSessionIDFromPath(path)
			_, cwd := harness.PeekCodexMeta(path)
			add(Target{JSONL: path, Harness: "codex", SessionID: sid, Workspace: cwd})
			return nil
		})
		for _, th := range harness.CodexStateThreads(filepath.Dir(opts.CodexRoot)) {
			if th.Rollout != "" {
				sid := th.ID
				if sid == "" {
					sid = harness.CodexSessionIDFromPath(th.Rollout)
				}
				cwd := th.CWD
				if cwd == "" {
					_, cwd = harness.PeekCodexMeta(th.Rollout)
				}
				add(Target{JSONL: th.Rollout, Harness: "codex", SessionID: sid, Workspace: cwd, UpdatedAt: th.Updated})
				continue
			}
			if th.ID == "" || th.CWD == "" || th.FirstUser == "" {
				continue
			}
			add(Target{
				Harness: "codex", SessionID: th.ID, Workspace: th.CWD, UpdatedAt: th.Updated,
				Messages: []map[string]any{{"type": "message", "role": "user", "content": th.FirstUser}},
			})
		}
	}
	if opts.PiRoot != "" {
		_ = filepath.WalkDir(opts.PiRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			sid := harness.PiSessionIDFromPath(path)
			cwd := harness.PeekPiCWD(path)
			add(Target{JSONL: path, Harness: "pi", SessionID: sid, Workspace: cwd})
			return nil
		})
	}
	if opts.OpenCodeDB != "" {
		for _, s := range write.ListOpenCodeSessions(opts.OpenCodeDB) {
			if s.Directory == "" {
				continue
			}
			add(Target{Harness: "opencode", SessionID: s.ID, Workspace: s.Directory, UpdatedAt: s.Updated})
		}
	}
	return out
}

// resolveProject turns a workspace into a project key by running git.
// Tests replace it to count calls.
var resolveProject = projectkey.FromWorkspace

// projectTTL is how long a workspace's resolved project is reused. An
// origin changes about never; the catch-up path resolves for itself.
const projectTTL = 10 * time.Minute

var projectCache = struct {
	sync.Mutex
	m map[string]projectEntry
}{m: map[string]projectEntry{}}

type projectEntry struct {
	key string
	at  time.Time
}

func resetProjectCache() {
	projectCache.Lock()
	projectCache.m = map[string]projectEntry{}
	projectCache.Unlock()
}

func cachedProject(workspace string) string {
	projectCache.Lock()
	defer projectCache.Unlock()
	if e, ok := projectCache.m[workspace]; ok && time.Since(e.at) < projectTTL {
		return e.key
	}
	key := resolveProject(workspace)
	projectCache.m[workspace] = projectEntry{key: key, at: time.Now()}
	return key
}

// projectsBySession indexes the stored project of every known session by
// harness and session id. Discover lists database-tracked sessions
// (OpenCode, Codex desktop) with a workspace and no project.
func projectsBySession(known []store.Session) map[string]string {
	out := make(map[string]string, len(known))
	for _, s := range known {
		if s.Project != "" && s.SessionID != "" {
			out[s.Harness+":"+s.SessionID] = s.Project
		}
	}
	return out
}

// sealProject is the project whose raw/ holds this target's live part:
// the target's own, else the one stored when the session was first
// ingested, else the workspace resolved through the TTL cache. idleSeal
// runs for every idle target on every tick; resolving each through git
// was a thousand processes a tick on a store with 1,087 OpenCode sessions.
func sealProject(t Target, known map[string]string) string {
	if t.Project != "" {
		return t.Project
	}
	if p := known[t.Harness+":"+t.SessionID]; p != "" {
		return p
	}
	if t.Workspace != "" {
		return cachedProject(t.Workspace)
	}
	return ""
}

func idleSeal(st *store.Store, t Target, opts Options, known map[string]string) bool {
	idle := opts.IdleSeal
	if idle <= 0 {
		idle = 24 * time.Hour
	}
	project := sealProject(t, known)
	if project == "" || t.SessionID == "" {
		return false
	}
	return idleSealPath(st.LiveRawPath(project, t.SessionID, time.Now()), idle)
}

func idleSealPath(raw string, idle time.Duration) bool {
	if raw == "" {
		return false
	}
	fi, err := os.Stat(raw)
	if err != nil {
		return false
	}
	if time.Since(fi.ModTime()) < idle {
		return false
	}
	_, err = write.SealRaw(raw)
	return err == nil
}

func needsCatchUp(st *store.Store, jsonl string) bool {
	if jsonl == "" {
		return false
	}
	fi, err := os.Stat(jsonl)
	if err != nil {
		return false
	}
	return st.Cursor(jsonl) != fi.Size()
}

func sqliteCursorKey(harness, sessionID string) string {
	return "sqlite:" + harness + ":" + sessionID
}

func needsSQLiteCatchUp(st *store.Store, key string, updated int64) bool {
	if key == "" {
		return false
	}
	cur := st.Cursor(key)
	if cur == 0 {
		return true
	}
	return cur < updated
}

func markSQLiteCaught(st *store.Store, key string, updated int64) {
	if updated < 1 {
		updated = 1
	}
	_ = st.SetCursor(key, updated)
}

// logf stamps a watcher line the way serve stamps its own: UTC time and
// the running version, so serve.log reads as one timeline.
func logf(format string, args ...any) {
	stamp := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(os.Stderr, stamp+" lossless watch ("+version.Version+"): "+format+"\n", args...)
}

// lastReplayLine keeps a job that fails every tick from logging every tick.
var lastReplayLine string

// replayLogLine is empty when a replay changed nothing. A turn hook gives
// the daemon 400ms and spools past that; the daemon has usually finished
// the catch-up by the time the tick replays the job, so most replays find
// the cursor already at the end of the file.
func replayLogLine(res write.EnsureResult) string {
	if res.Moved == 0 && res.Failed == 0 {
		return ""
	}
	s := fmt.Sprintf("replayed %d spooled catch-ups (%d moved tape, %d failed)", res.Replayed, res.Moved, res.Failed)
	if res.Failed > 0 && len(res.Errors) > 0 {
		s += ": " + res.Errors[0]
	}
	return s
}

func Tick(st *store.Store, opts Options) (Result, error) {
	// Hooks spool a catch-up when the daemon is down or slow. Only the
	// `ensure` CLI replayed the spool before; the tick does it now.
	if files, _ := write.ListSpool(st.Root); len(files) > 0 {
		if res, err := write.Ensure(st, st.Root); err == nil {
			line := replayLogLine(res)
			// A job that fails every tick logs once, not once a second.
			if line != "" && (res.Failed == 0 || line != lastReplayLine) {
				logf("%s", line)
			}
			lastReplayLine = line
		}
	}
	known, err := st.ListSessions()
	if err != nil {
		return Result{}, err
	}
	targets := Discover(opts, known)
	projects := projectsBySession(known)
	var res Result
	res.Seen = len(targets)
	sqliteCatchUps := 0
	for _, t := range targets {
		if t.Workspace == "" && t.Project == "" && t.Harness == "codex" && t.JSONL != "" {
			_, t.Workspace = harness.PeekCodexMeta(t.JSONL)
		}
		if t.Workspace == "" && t.Project == "" && t.Harness == "claude" && t.JSONL != "" {
			t.Workspace = harness.PeekClaudeCWD(t.JSONL)
		}
		if t.Workspace == "" && t.Project == "" {
			// Do not guess a project. Claude/OpenCode/empty-Codex stay
			// skipped until cwd is known. Do not rewrite cleanupPeriodDays.
			if t.Harness == "claude" || t.Harness == "opencode" || (t.Harness == "codex" && t.JSONL == "") {
				continue
			}
		}
		sqliteKey := ""
		if (t.Harness == "opencode" || (t.Harness == "codex" && t.JSONL == "")) && t.SessionID != "" {
			sqliteKey = sqliteCursorKey(t.Harness, t.SessionID)
		}
		if sqliteKey != "" {
			if !needsSQLiteCatchUp(st, sqliteKey, t.UpdatedAt) {
				if sealed := idleSeal(st, t, opts, projects); sealed {
					res.Sealed++
				}
				continue
			}
			if sqliteCatchUps >= sqliteTickCap {
				continue
			}
		} else if !needsCatchUp(st, t.JSONL) {
			if sealed := idleSeal(st, t, opts, projects); sealed {
				res.Sealed++
			}
			continue
		}
		req := write.CatchUpRequest{
			JSONL: t.JSONL, Project: t.Project, WorkspaceRoot: t.Workspace,
			Harness: t.Harness, SessionID: t.SessionID, Source: "turn",
			Messages: t.Messages,
		}
		out, err := write.CatchUp(st, req)
		if err != nil {
			continue
		}
		if sqliteKey != "" {
			markSQLiteCaught(st, sqliteKey, t.UpdatedAt)
			sqliteCatchUps++
		}
		if !out.Noop && out.Copied > 0 {
			res.CatchUps++
			_, _ = write.FlushPush(st.Root)
		}
		if out.SawCompact {
			req.Source = "compact"
			retrieve.RefreshActive(st, st.Root, req, out.RawPath)
		}
		idle := opts.IdleSeal
		if idle <= 0 {
			idle = 24 * time.Hour
		}
		if sealed := idleSealPath(out.RawPath, idle); sealed {
			res.Sealed++
		}
	}
	return res, nil
}

// claudeSubagentDump is Claude Code Task/workflow JSONL, not a session tape.
func claudeSubagentDump(path string) bool {
	n := filepath.ToSlash(path)
	if strings.Contains(n, "/subagents/") {
		return true
	}
	base := strings.ToLower(filepath.Base(n))
	return strings.HasPrefix(base, "agent-") && strings.HasSuffix(base, ".jsonl")
}

func Run(ctx context.Context, st *store.Store, opts Options) error {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	t := time.NewTicker(opts.Interval)
	defer t.Stop()
	sweep := time.NewTicker(time.Hour)
	defer sweep.Stop()
	_ = write.SweepStaleVirtual(st.Root)
	_, _ = safeTick(st, opts)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sweep.C:
			_ = write.SweepStaleVirtual(st.Root)
		case <-t.C:
			_, _ = safeTick(st, opts)
		}
	}
}

// testPanicTick, when set, runs inside the protected region. Tests use
// it to prove a panicking tick cannot take the daemon down.
var testPanicTick func()

// safeTick turns a panic in one tick into an error. The watcher is the
// daemon's only long-lived goroutine; one bad line must not kill it.
func safeTick(st *store.Store, opts Options) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("watch tick panic: %v", r)
			logf("%v", err)
		}
	}()
	if testPanicTick != nil {
		testPanicTick()
	}
	return Tick(st, opts)
}
