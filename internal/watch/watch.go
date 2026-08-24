package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/harness"
	"lossless/internal/projectkey"
	"lossless/internal/retrieve"
	"lossless/internal/store"
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

func idleSeal(st *store.Store, t Target, opts Options) bool {
	idle := opts.IdleSeal
	if idle <= 0 {
		idle = 24 * time.Hour
	}
	project := t.Project
	if project == "" && t.Workspace != "" {
		project = projectkey.FromWorkspace(t.Workspace)
	}
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

func Tick(st *store.Store, opts Options) (Result, error) {
	known, err := st.ListSessions()
	if err != nil {
		return Result{}, err
	}
	targets := Discover(opts, known)
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
				if sealed := idleSeal(st, t, opts); sealed {
					res.Sealed++
				}
				continue
			}
			if sqliteCatchUps >= sqliteTickCap {
				continue
			}
		} else if !needsCatchUp(st, t.JSONL) {
			if sealed := idleSeal(st, t, opts); sealed {
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

func Run(ctx context.Context, st *store.Store, opts Options) error {
	if opts.Interval <= 0 {
		opts.Interval = time.Second
	}
	t := time.NewTicker(opts.Interval)
	defer t.Stop()
	_, _ = Tick(st, opts)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			_, _ = Tick(st, opts)
		}
	}
}
