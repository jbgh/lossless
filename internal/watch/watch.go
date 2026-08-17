package watch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lossless/internal/harness"
	"lossless/internal/projectkey"
	"lossless/internal/store"
	"lossless/internal/write"
)

type Options struct {
	GrokRoot   string
	ClaudeRoot string
	CodexRoot  string
	PiRoot     string
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
}

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
		Interval:   time.Second,
	}
}

func Discover(opts Options, known []store.Session) []Target {
	seen := map[string]bool{}
	var out []Target
	add := func(t Target) {
		if t.JSONL == "" || seen[t.JSONL] {
			return
		}
		seen[t.JSONL] = true
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
			// project-level session files only, not nested agent dumps
			rel, _ := filepath.Rel(opts.ClaudeRoot, path)
			if strings.Count(filepath.ToSlash(rel), "/") != 1 {
				return nil
			}
			sid := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			add(Target{JSONL: path, Harness: "claude", SessionID: sid})
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
		for _, p := range harness.CodexStateRollouts(filepath.Dir(opts.CodexRoot)) {
			sid := harness.CodexSessionIDFromPath(p)
			_, cwd := harness.PeekCodexMeta(p)
			add(Target{JSONL: p, Harness: "codex", SessionID: sid, Workspace: cwd})
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

func Tick(st *store.Store, opts Options) (Result, error) {
	known, err := st.ListSessions()
	if err != nil {
		return Result{}, err
	}
	targets := Discover(opts, known)
	var res Result
	res.Seen = len(targets)
	for _, t := range targets {
		if t.Workspace == "" && t.Project == "" && t.Harness == "codex" {
			_, t.Workspace = harness.PeekCodexMeta(t.JSONL)
		}
		if t.Workspace == "" && t.Project == "" {
			// Claude files we have never hooked: do not guess a project
			if t.Harness == "claude" {
				continue
			}
		}
		if !needsCatchUp(st, t.JSONL) {
			if sealed := idleSeal(st, t, opts); sealed {
				res.Sealed++
			}
			continue
		}
		out, err := write.CatchUp(st, write.CatchUpRequest{
			JSONL: t.JSONL, Project: t.Project, WorkspaceRoot: t.Workspace,
			Harness: t.Harness, SessionID: t.SessionID, Source: "turn",
		})
		if err != nil {
			continue
		}
		if !out.Noop && out.Copied > 0 {
			res.CatchUps++
			_, _ = write.FlushPush(st.Root)
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
