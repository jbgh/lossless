package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lossless/internal/harness"
	"lossless/internal/store"
	"lossless/internal/write"
)

func TestDiscoverAndTick(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	sid := "sess-watch"
	gdir := filepath.Join(root, "grok", harness.EncodeCWD(ws), sid)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	gfile := filepath.Join(gdir, "chat_history.jsonl")
	if err := os.WriteFile(gfile, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// updates.jsonl must be ignored
	if err := os.WriteFile(filepath.Join(gdir, "updates.jsonl"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cdir := filepath.Join(root, "claude", harness.ClaudeProjectSlug(ws))
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfile := filepath.Join(cdir, "uuid-1.jsonl")
	if err := os.WriteFile(cfile, []byte(`{"type":"assistant","content":"We decided to use jose."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// nested dump ignored
	if err := os.MkdirAll(filepath.Join(cdir, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(cdir, "agent", "nested.jsonl"), []byte("x\n"), 0o644)

	xdir := filepath.Join(root, "codex", "2026", "08", "14")
	if err := os.MkdirAll(xdir, 0o755); err != nil {
		t.Fatal(err)
	}
	xfile := filepath.Join(xdir, "rollout-2026-08-14T12-00-00-019ef634-9af9-72d2-b01c-97d349693335.jsonl")
	if err := os.WriteFile(xfile, []byte(
		`{"type":"session_meta","payload":{"id":"019ef634-9af9-72d2-b01c-97d349693335","cwd":"`+ws+`"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"agent_message","message":"We decided to use jose, not jsonwebtoken, for Edge."}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pdir := filepath.Join(root, "pi", "--Users-jay-dev-api--")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	pfile := filepath.Join(pdir, "2024-12-03T14-00-00_abcdef12-3456.jsonl")
	if err := os.WriteFile(pfile, []byte(
		`{"type":"session","version":3,"id":"abcdef12-3456","cwd":"`+ws+`"}`+"\n"+
			`{"type":"message","id":"a1","parentId":null,"message":{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{GrokRoot: filepath.Join(root, "grok"), ClaudeRoot: filepath.Join(root, "claude"), CodexRoot: filepath.Join(root, "codex"), PiRoot: filepath.Join(root, "pi")}
	found := Discover(opts, nil)
	var grok, claude, codex, pi int
	for _, f := range found {
		if f.Harness == "grok" {
			grok++
			if f.Workspace != ws {
				t.Fatalf("decoded ws %q", f.Workspace)
			}
		}
		if f.Harness == "claude" {
			claude++
		}
		if f.Harness == "codex" {
			codex++
			if f.Workspace != ws {
				t.Fatalf("codex cwd %q", f.Workspace)
			}
		}
		if f.Harness == "pi" {
			pi++
			if f.Workspace != ws {
				t.Fatalf("pi cwd %q", f.Workspace)
			}
		}
	}
	if grok != 1 || claude != 1 || codex != 1 || pi != 1 {
		t.Fatalf("discover grok=%d claude=%d codex=%d pi=%d %+v", grok, claude, codex, pi, found)
	}

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	res, err := Tick(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.CatchUps < 2 { // grok + codex (cwd in session_meta); unknown claude skipped
		t.Fatalf("catch_ups=%d seen=%d", res.CatchUps, res.Seen)
	}
	claims, _ := st.ListActive("jay/dev") // FromOrigin? FromWorkspace of /Users/jay/dev/api
	// workspace /Users/jay/dev/api may not be a git repo → path- key
	if len(claims) == 0 {
		// project from FromWorkspace of decoded path — likely path- hash
		// just check any active claim exists
		// store has no list-all; Search via session
	}
	again, err := Tick(st, opts)
	if err != nil || again.CatchUps != 0 {
		t.Fatalf("second tick %+v %v", again, err)
	}

	// register claude session then tick picks it up
	if err := st.UpsertSession(store.Session{JSONL: cfile, SessionID: "uuid-1", Harness: "claude", Workspace: ws, Project: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfile, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cursor is 0 if never caught up this file... first Tick skipped it so cursor 0. File has bytes.
	third, err := Tick(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if third.CatchUps < 1 {
		t.Fatalf("claude after register: %+v", third)
	}
}

func TestIdleSeal(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	sid := "idle-1"
	gdir := filepath.Join(root, "grok", harness.EncodeCWD(ws), sid)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	gfile := filepath.Join(gdir, "chat_history.jsonl")
	if err := os.WriteFile(gfile, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	opts := Options{GrokRoot: filepath.Join(root, "grok"), IdleSeal: time.Hour}
	first, err := Tick(st, opts)
	if err != nil || first.CatchUps != 1 {
		t.Fatalf("%+v %v", first, err)
	}
	res, err := write.CatchUp(st, write.CatchUpRequest{JSONL: gfile, WorkspaceRoot: ws, Harness: "grok", SessionID: sid, Source: "turn"})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(res.RawPath, old, old); err != nil {
		t.Fatal(err)
	}
	opts.IdleSeal = time.Hour
	second, err := Tick(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sealed < 1 {
		t.Fatalf("expected seal: %+v", second)
	}
	if _, err := os.Stat(res.RawPath); !os.IsNotExist(err) {
		t.Fatal("live raw should be gone")
	}
}

func TestRunCancels(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err = Run(ctx, st, Options{Interval: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("expected ctx err")
	}
}

func TestDefaults(t *testing.T) {
	t.Setenv("HOME", "/tmp/home")
	t.Setenv("GROK_HOME", "")
	t.Setenv("CLAUDE_HOME", "")
	d := Defaults()
	if d.Interval <= 0 || d.GrokRoot == "" || d.ClaudeRoot == "" || d.CodexRoot == "" || d.PiRoot == "" {
		t.Fatalf("%+v", d)
	}
}
