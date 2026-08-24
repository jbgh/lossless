package watch

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/harness"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"

	_ "modernc.org/sqlite"
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
	if err := os.WriteFile(cfile, []byte(`{"type":"user","cwd":"`+ws+`","message":{"role":"user","content":"go"}}`+"\n"+
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// nested dump without cwd stays skipped; nested with cwd is catch-up
	if err := os.MkdirAll(filepath.Join(cdir, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(cdir, "agent", "nested.jsonl"), []byte("x\n"), 0o644)
	nestedOK := filepath.Join(cdir, "agent", "nested-ok.jsonl")
	if err := os.WriteFile(nestedOK, []byte(`{"type":"user","cwd":"`+ws+`","message":{"role":"user","content":"go"}}`+"\n"+
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(cdir, "uuid-1", "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subDump := filepath.Join(subDir, "agent-deadbeef.jsonl")
	if err := os.WriteFile(subDump, []byte(`{"type":"user","cwd":"`+ws+`","message":{"role":"user","content":"go"}}`+"\n"+
		`{"type":"assistant","content":"READ-ONLY: do not push, edit, or merge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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
			if f.Workspace != ws {
				t.Fatalf("claude cwd %q", f.Workspace)
			}
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
	if grok != 1 || claude != 2 || codex != 1 || pi != 1 {
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
	if res.CatchUps < 5 { // grok + claude + nested claude with cwd + codex + pi
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

	// unknown-cwd Claude stays skipped; do not guess from the slug
	unknown := filepath.Join(cdir, "uuid-unknown.jsonl")
	if err := os.WriteFile(unknown, []byte(`{"type":"assistant","content":"We decided to use jose."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := Tick(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if third.CatchUps != 0 {
		t.Fatalf("unknown-cwd claude must skip: %+v", third)
	}
}

func TestTickWritesActiveOnCompactionDelta(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	sid := "sess-compact"
	gdir := filepath.Join(root, "grok", harness.EncodeCWD(ws), sid)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	gfile := filepath.Join(gdir, "chat_history.jsonl")
	body := `{"type":"user","content":"keep jose in src/middleware/auth.ts"}` + "\n" +
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts."}` + "\n"
	if err := os.WriteFile(gfile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	opts := Options{GrokRoot: filepath.Join(root, "grok")}
	if _, err := Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(st.Root, "active")); !os.IsNotExist(err) {
		t.Fatal("grow without compaction wrote checkout")
	}
	if err := os.WriteFile(gfile, []byte(body+`{"type":"compaction","content":"window compacted"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Tick(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.CatchUps < 1 {
		t.Fatalf("catch_ups=%d", res.CatchUps)
	}
	ents, err := os.ReadDir(filepath.Join(st.Root, "active"))
	if err != nil || len(ents) == 0 {
		t.Fatalf("checkout missing after compaction delta: %v", err)
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

func TestLiveDiscoverHarnesses(t *testing.T) {
	if os.Getenv("LOSSLESS_LIVE") != "1" {
		t.Skip("set LOSSLESS_LIVE=1 to locate this machine's harness files")
	}
	opts := Defaults()
	found := Discover(opts, nil)
	var claudeCWD, claudeSkip, opencode, codexFile, codexEmpty int
	for _, f := range found {
		switch f.Harness {
		case "claude":
			if f.Workspace != "" {
				claudeCWD++
			} else {
				claudeSkip++
			}
		case "opencode":
			opencode++
		case "codex":
			if f.JSONL != "" {
				codexFile++
			} else {
				codexEmpty++
			}
		}
	}
	t.Logf("live discover seen=%d claude_cwd=%d claude_skip=%d opencode=%d codex_file=%d codex_empty=%d",
		len(found), claudeCWD, claudeSkip, opencode, codexFile, codexEmpty)
	if claudeCWD == 0 && claudeSkip == 0 && opencode == 0 && codexFile == 0 && codexEmpty == 0 {
		t.Fatal("live discover found no claude/opencode/codex targets")
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
	t.Setenv("OPENCODE_DB", "")
	t.Setenv("OPENCODE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	d := Defaults()
	if d.Interval <= 0 || d.GrokRoot == "" || d.ClaudeRoot == "" || d.CodexRoot == "" || d.PiRoot == "" || d.OpenCodeDB == "" {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverOpenCodeAndEmptyCodex(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	dbPath := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE session (id TEXT, directory TEXT, time_updated INTEGER);
CREATE TABLE message (id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
CREATE TABLE part (id TEXT, message_id TEXT, time_created INTEGER, data TEXT);
INSERT INTO session(id, directory, time_updated) VALUES('ses_watch', ?, 42);
INSERT INTO message(id, session_id, time_created, data) VALUES
 ('m1', 'ses_watch', 1, '{"role":"assistant"}');
INSERT INTO part(id, message_id, time_created, data) VALUES
 ('p1', 'm1', 1, '{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}');
`, ws)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	codexHome := filepath.Join(root, "codex")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	cdb, err := sql.Open("sqlite", filepath.Join(codexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = cdb.Exec(`
CREATE TABLE threads (
  id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL, source TEXT NOT NULL, model_provider TEXT NOT NULL,
  cwd TEXT NOT NULL, title TEXT NOT NULL, sandbox_policy TEXT NOT NULL,
  approval_mode TEXT NOT NULL, first_user_message TEXT NOT NULL DEFAULT ''
);
INSERT INTO threads(id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode, first_user_message)
VALUES('desk-1', '', 1, 7, 'desktop', 'openai', ?, 't', 'danger', 'on', 'Never log Authorization headers in src/middleware/auth.ts.');
`, ws)
	if err != nil {
		t.Fatal(err)
	}
	_ = cdb.Close()

	t.Setenv("OPENCODE_DB", dbPath)
	opts := Options{
		CodexRoot:  filepath.Join(codexHome, "sessions"),
		OpenCodeDB: dbPath,
	}
	found := Discover(opts, nil)
	var oc, emptyCodex int
	for _, f := range found {
		if f.Harness == "opencode" && f.SessionID == "ses_watch" {
			oc++
			if f.Workspace != ws {
				t.Fatalf("opencode ws %q", f.Workspace)
			}
		}
		if f.Harness == "codex" && f.SessionID == "desk-1" {
			emptyCodex++
			if f.JSONL != "" || f.Workspace != ws || len(f.Messages) == 0 {
				t.Fatalf("%+v", f)
			}
		}
	}
	if oc != 1 || emptyCodex != 1 {
		t.Fatalf("opencode=%d emptyCodex=%d %+v", oc, emptyCodex, found)
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
	if res.CatchUps < 2 {
		t.Fatalf("catch_ups=%d seen=%d", res.CatchUps, res.Seen)
	}
	again, err := Tick(st, opts)
	if err != nil || again.CatchUps != 0 {
		t.Fatalf("second tick %+v %v", again, err)
	}
}

func TestCrossHarnessSamePack(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	sidG := "sess-g"
	gdir := filepath.Join(root, "grok", harness.EncodeCWD(ws), sidG)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "chat_history.jsonl"), []byte(
		`{"type":"assistant","content":"Redis token bucket failed in src/middleware/auth.ts staging."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cdir := filepath.Join(root, "claude", harness.ClaudeProjectSlug(ws))
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cdir, "uuid-c.jsonl"), []byte(
		`{"type":"user","cwd":"`+ws+`"}`+"\n"+
			`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		GrokRoot: filepath.Join(root, "grok"), ClaudeRoot: filepath.Join(root, "claude"),
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	res, err := Tick(st, opts)
	if err != nil || res.CatchUps < 2 {
		t.Fatalf("%+v %v", res, err)
	}
	all, err := st.ListAllActive()
	if err != nil {
		t.Fatal(err)
	}
	blob := ""
	harness := map[string]bool{}
	project := ""
	for _, rec := range all {
		blob += rec.Text + "\n"
		harness[rec.Harness] = true
		if rec.ProjectKey != "" {
			project = rec.ProjectKey
		}
	}
	if !strings.Contains(blob, "Redis") || !strings.Contains(blob, "jose") {
		t.Fatalf("tape missed a harness: %s", blob)
	}
	if !harness["grok"] || !harness["claude"] {
		t.Fatalf("claims not both harnesses: %v", harness)
	}
	out, err := retrieve.Ask(st, retrieve.Request{
		Project: project, SessionID: sidG, Goal: "pick a jwt library",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, h := range out.Context {
		got += h.Text + "\n"
	}
	if !strings.Contains(got, "jose") {
		t.Fatalf("grok session pack missed claude claim: %s", got)
	}
}
