package watch

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lossless/internal/harness"
	"lossless/internal/inspect"
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
	OmpRoot    string
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
		OmpRoot:    harness.OmpSessionsRoot(),
		OpenCodeDB: harness.OpenCodeDB(),
		Interval:   DefaultInterval,
	}
}

// Discover lists every session target under opts' roots plus the known
// sessions, with no cache: each call walks and peeks everything. The
// watch loop uses a Watcher, which keeps what it learned between ticks.
func Discover(opts Options, known []store.Session) []Target {
	w := NewWatcher()
	w.beginTick(time.Now())
	return w.discover(opts, known)
}

func (w *Watcher) discover(opts Options, known []store.Session) []Target {
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
		w.walk(opts.GrokRoot, func(path string, d fs.DirEntry) {
			if filepath.Base(path) != "chat_history.jsonl" {
				return
			}
			rel, _ := filepath.Rel(opts.GrokRoot, path)
			parts := strings.Split(filepath.ToSlash(rel), "/")
			ws, sid := "", ""
			if len(parts) >= 2 {
				ws = harness.DecodeGrokSessionDir(parts[0])
				sid = parts[1]
			}
			add(Target{JSONL: path, Harness: "grok", SessionID: sid, Workspace: ws})
		})
	}
	if opts.ClaudeRoot != "" {
		w.walk(opts.ClaudeRoot, func(path string, d fs.DirEntry) {
			if !strings.HasSuffix(path, ".jsonl") {
				return
			}
			// Task/workflow dumps are instruction chrome, not product tape.
			// Nested JSONL with cwd that is not under subagents/ still catch-up.
			if claudeSubagentDump(path) {
				return
			}
			cwd := w.peek(path, d, peekClaudeCWD)
			if cwd == "" {
				return
			}
			sid := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			add(Target{JSONL: path, Harness: "claude", SessionID: sid, Workspace: cwd})
		})
	}
	if opts.CodexRoot != "" {
		w.walk(opts.CodexRoot, func(path string, d fs.DirEntry) {
			base := filepath.Base(path)
			if !strings.HasPrefix(base, "rollout-") || !strings.HasSuffix(base, ".jsonl") {
				return
			}
			sid := harness.CodexSessionIDFromPath(path)
			cwd := w.peek(path, d, codexCWD)
			add(Target{JSONL: path, Harness: "codex", SessionID: sid, Workspace: cwd})
		})
		for _, th := range w.codexThreads(filepath.Dir(opts.CodexRoot)) {
			if th.Rollout != "" {
				sid := th.ID
				if sid == "" {
					sid = harness.CodexSessionIDFromPath(th.Rollout)
				}
				cwd := th.CWD
				if cwd == "" {
					cwd = w.peek(th.Rollout, nil, codexCWD)
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
		w.walk(opts.PiRoot, func(path string, d fs.DirEntry) {
			if !strings.HasSuffix(path, ".jsonl") {
				return
			}
			sid := harness.PiSessionIDFromPath(path)
			cwd := w.peek(path, d, peekPiCWD)
			add(Target{JSONL: path, Harness: "pi", SessionID: sid, Workspace: cwd})
		})
	}
	if opts.OmpRoot != "" {
		w.walk(opts.OmpRoot, func(path string, d fs.DirEntry) {
			// Only omp's own session files (<ts>_<uuid>.jsonl). A session's
			// artifact directory holds subagent transcripts and bash logs,
			// which are chrome, like Claude's agent-*.jsonl dumps.
			if !harness.OmpSessionFile(path) {
				return
			}
			sid := harness.OmpSessionIDFromPath(path)
			cwd := w.peek(path, d, peekOmpCWD)
			add(Target{JSONL: path, Harness: "omp", SessionID: sid, Workspace: cwd})
		})
	}
	if opts.OpenCodeDB != "" {
		for _, s := range w.openCodeSessions(opts.OpenCodeDB) {
			if s.Directory == "" {
				continue
			}
			add(Target{Harness: "opencode", SessionID: s.ID, Workspace: s.Directory, UpdatedAt: s.Updated})
		}
	}
	return out
}

// The transcript peeks open a file and parse its head. Tests replace them
// to count calls.
var (
	peekClaudeCWD = harness.PeekClaudeCWD
	peekCodexMeta = harness.PeekCodexMeta
	peekPiCWD     = harness.PeekPiCWD
	peekOmpCWD    = harness.PeekOmpCWD
)

// statFile is the per-target stat. Tests replace it to count stats.
var statFile = os.Stat

// listSessions reads the session table. Tests replace it to count reads.
var listSessions = (*store.Store).ListSessions

// codexStateRows reads the Codex state databases. Tests replace it to
// count reads.
var codexStateRows = harness.CodexStateRows

// catchUp copies a target's new lines to its tape. Tests replace it to
// count attempts.
var catchUp = write.CatchUp

func codexCWD(path string) string {
	_, cwd := peekCodexMeta(path)
	return cwd
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
	// Seal the tape this session actually wrote. Resolving with
	// time.Now() looked in the current month only, so a session whose
	// tape lives in an earlier month's folder was never sealed once
	// the month rolled over. The catch-up's RawPath is exact when the
	// tick has one; here the session's own month comes from its source
	// file's mtime.
	return idleSealPath(st.LiveRawPath(project, t.SessionID, tapeMonth(t)), idle)
}

// tapeMonth is the month a session's tape lives in: the month its
// source file last changed. A target with no source file (an OpenCode
// or Codex-desktop session tracked in the harness database) uses the
// harness's own update time.
func tapeMonth(t Target) time.Time {
	if t.JSONL != "" {
		if fi, err := os.Stat(t.JSONL); err == nil {
			return fi.ModTime()
		}
	}
	if t.UpdatedAt > 0 {
		return time.Unix(t.UpdatedAt, 0)
	}
	return time.Now()
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

// Tick runs one full pass with no cache. The watch loop runs a Watcher's
// Tick, which skips what has not changed since its last pass.
func Tick(st *store.Store, opts Options) (Result, error) {
	return NewWatcher().Tick(st, opts)
}

func (w *Watcher) Tick(st *store.Store, opts Options) (Result, error) {
	now := time.Now()
	w.beginTick(now)
	defer w.endTick()
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
	known, projects, err := w.knownSessions(st)
	if err != nil {
		return Result{}, err
	}
	targets := w.discover(opts, known)
	var res Result
	res.Seen = len(targets)
	sqliteCatchUps := 0
	for _, t := range targets {
		if t.Workspace == "" && t.Project == "" && t.Harness == "codex" && t.JSONL != "" {
			t.Workspace = w.peek(t.JSONL, nil, codexCWD)
		}
		if t.Workspace == "" && t.Project == "" && t.Harness == "claude" && t.JSONL != "" {
			t.Workspace = w.peek(t.JSONL, nil, peekClaudeCWD)
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
		// quietKey names this target (an OpenCode session can be listed
		// twice: once from the session table under its virtual path, once
		// from opencode.db), sig is its file's state.
		var quietKey string
		var sig fileSig
		var fi os.FileInfo
		if sqliteKey != "" {
			quietKey = sqliteKey + "\x00" + t.JSONL
			// Quiet: caught up and seal-checked at this update time.
			if w.isQuiet(quietKey, sig, t.UpdatedAt) {
				continue
			}
			if !needsSQLiteCatchUp(st, sqliteKey, t.UpdatedAt) {
				if sealed := idleSeal(st, t, opts, projects); sealed {
					res.Sealed++
				}
				w.markQuiet(quietKey, sig, t.UpdatedAt)
				continue
			}
			if sqliteCatchUps >= sqliteTickCap {
				continue
			}
		} else {
			// One stat per file target per tick. A file whose size and
			// mtime match the pass that last found it caught up (and
			// seal-checked) costs nothing more: no cursor query, no
			// idle-seal stats.
			quietKey = t.JSONL
			if quietKey == "" {
				quietKey = t.Harness + ":" + t.SessionID
			}
			if w.coldQuiet(quietKey, t.UpdatedAt) {
				continue
			}
			if t.JSONL != "" {
				if f, err := statFile(t.JSONL); err == nil {
					fi, sig = f, sigOf(f)
				}
			}
			if w.isQuiet(quietKey, sig, t.UpdatedAt) {
				continue
			}
			// Its file moved on (or is new): drop the old state, so the
			// next look stats it and sees it hot, not cold at its old mtime.
			delete(w.quiet, quietKey)
			if fi == nil || st.Cursor(t.JSONL) == fi.Size() {
				if sealed := idleSeal(st, t, opts, projects); sealed {
					res.Sealed++
				}
				if fi == nil || !racy(fi, now) {
					w.markQuiet(quietKey, sig, t.UpdatedAt)
				}
				continue
			}
		}
		req := write.CatchUpRequest{
			JSONL: t.JSONL, Project: t.Project, WorkspaceRoot: t.Workspace,
			Harness: t.Harness, SessionID: t.SessionID, Source: "turn",
			Messages: t.Messages,
		}
		out, err := catchUp(st, req)
		if err != nil {
			// A file that failed at this state is tried again when it
			// changes or at the next verify pass, not every tick: a Pi
			// subagent transcript has no cwd, and its catch-up fails
			// ("project or workspace_root required") for as long as the
			// file stands.
			if fi != nil && !racy(fi, now) {
				w.markQuiet(quietKey, sig, t.UpdatedAt)
			}
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

// DefaultInterval is how often the watcher polls the harness roots. An
// idle tick is a stat per known file and per directory; 0.1.42 polled
// every second and peeked every transcript each time.
const DefaultInterval = 2 * time.Second

func Run(ctx context.Context, st *store.Store, opts Options) error {
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	w := NewWatcher()
	t := time.NewTicker(opts.Interval)
	defer t.Stop()
	sweep := time.NewTicker(time.Hour)
	defer sweep.Stop()
	_ = write.SweepStaleVirtual(st.Root)
	_, _ = safeTick(w, st, opts)
	var sweeping atomic.Bool
	var sweeps sync.WaitGroup
	// Run returns only after a running sweep has stopped, so nothing
	// seals under a store the caller is about to close.
	defer sweeps.Wait()
	sweepRawTapesAsync(ctx, st, opts, &sweeping, &sweeps)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sweep.C:
			_ = write.SweepStaleVirtual(st.Root)
			sweepRawTapesAsync(ctx, st, opts, &sweeping, &sweeps)
		case <-t.C:
			_, _ = safeTick(w, st, opts)
		}
	}
}

// sweepRawTapesAsync runs the raw sweep off the watcher goroutine, so the
// first sweep after an upgrade (hundreds of MB of plain tapes to compress
// and verify) does not stall capture ticks. At most one sweep runs at a
// time; a sweep still running when the next hour comes is not doubled. A
// panic in the sweep is logged, not fatal, like a tick's.
func sweepRawTapesAsync(ctx context.Context, st *store.Store, opts Options, running *atomic.Bool, wg *sync.WaitGroup) {
	if !running.CompareAndSwap(false, true) {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer running.Store(false)
		defer func() {
			if r := recover(); r != nil {
				logf("raw seal sweep panic: %v", r)
			}
		}()
		sweepRawTapes(ctx, st, opts)
	}()
}

// sweepRawTapes seals plain tapes that no watch target reaches: a
// Claude Code subagent dump ingested before it was excluded, a session
// whose source transcript is gone, or a month the watcher never revisits.
// It runs here so it shares the process (and the lock) with the watcher
// and the appends, and it seals through write.SealRaw like every other
// path: verified round-trip, never a bare delete.
func sweepRawTapes(ctx context.Context, st *store.Store, opts Options) {
	idle := opts.IdleSeal
	res, err := inspect.SealRawSweepCtx(ctx, st, idle, true)
	if err != nil {
		logf("raw seal sweep: %v", err)
		return
	}
	if res.Sealed > 0 {
		logf("sealed %d raw tapes (idle >= %s), saved %d bytes",
			res.Sealed, res.Idle, res.BytesSaved)
	}
}

// testPanicTick, when set, runs inside the protected region. Tests use
// it to prove a panicking tick cannot take the daemon down.
var testPanicTick func()

// safeTick turns a panic in one tick into an error. The watcher is the
// daemon's only long-lived goroutine; one bad line must not kill it.
func safeTick(w *Watcher, st *store.Store, opts Options) (res Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("watch tick panic: %v", r)
			logf("%v", err)
		}
	}()
	if testPanicTick != nil {
		testPanicTick()
	}
	return w.Tick(st, opts)
}
