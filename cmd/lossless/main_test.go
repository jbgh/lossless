package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/harness"
)

func TestStringsFlagAndJSONQuote(t *testing.T) {
	var s stringsFlag
	_ = s.String()
	if err := s.Set("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("b"); err != nil || len(s) != 2 {
		t.Fatal(s, err)
	}
	if jsonQuote(`say "hi"`) != `"say \"hi\""` {
		t.Fatal(jsonQuote(`say "hi"`))
	}
}

func TestUsageAndHomeFlag(t *testing.T) {
	usage()
	t.Setenv("LOSSLESS_HOME", "/tmp/am-home")
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	h := homeFlag(fs)
	if *h != "/tmp/am-home" {
		t.Fatal(*h)
	}
	t.Setenv("LOSSLESS_HOME", "")
	t.Setenv("LOSSLESS_HOME", "")
	t.Setenv("HOME", t.TempDir())
	fs = flag.NewFlagSet("t2", flag.ContinueOnError)
	h = homeFlag(fs)
	if !strings.Contains(*h, ".lossless") {
		t.Fatal(*h)
	}
}

func TestRunCatchUpRememberAsk(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(src, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runCatchUp([]string{"-bogus"}) != 2 {
		t.Fatal("parse")
	}
	badHome := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badHome, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runCatchUp([]string{"--home", badHome, "--jsonl", src, "--project", "acme/api"}) != 1 {
		t.Fatal("open")
	}
	if runCatchUp([]string{"--home", home, "--jsonl", "", "--project", "acme/api"}) != 1 {
		t.Fatal("catch-up err")
	}
	if runCatchUp([]string{"--home", home, "--jsonl", src, "--project", "acme/api", "--harness", "grok", "--session", "s1"}) != 0 {
		t.Fatal("catch-up")
	}

	if runRemember([]string{"-bogus"}) != 2 {
		t.Fatal("remember parse")
	}
	if runRemember([]string{"--home", badHome, "--type", "decision", "--text", "Use jose, not jsonwebtoken, for Edge.", "--project", "acme/api"}) != 1 {
		t.Fatal("remember open")
	}
	if runRemember([]string{"--home", home, "--type", "", "--text", ""}) != 1 {
		t.Fatal("remember valid")
	}
	if runRemember([]string{"--home", home, "--type", "failed", "--text", "Redis token bucket failed in staging yesterday.", "--project", "acme/api", "--path", "src/auth.ts"}) != 0 {
		t.Fatal("remember")
	}

	if runAsk([]string{"-bogus"}) != 2 {
		t.Fatal("ask parse")
	}
	if runAsk([]string{"--home", badHome, "--project", "acme/api"}) != 1 {
		t.Fatal("ask open")
	}
	if runAsk([]string{"--home", home}) != 1 {
		t.Fatal("ask missing project")
	}
	if runAsk([]string{"--home", home, "--project", "acme/api", "--question", "jose", "--goal", "pick lib", "--path", "src/auth.ts", "--limit-tokens", "400"}) != 0 {
		t.Fatal("ask")
	}
	if runInspect([]string{"--home", home}) != 0 {
		t.Fatal("inspect")
	}
	if runInspect([]string{"--home", home, "--project", "acme/api", "--ask", "--question", "jose", "--path", "src/auth.ts"}) != 0 {
		t.Fatal("inspect ask")
	}
}

func TestRunServe(t *testing.T) {
	if runServe([]string{"-bogus"}) != 2 {
		t.Fatal("parse")
	}
	badHome := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(badHome, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runServe([]string{"--home", badHome}) != 1 {
		t.Fatal("open")
	}
	if runServe([]string{"--home", t.TempDir(), "--listen", "0.0.0.0:9"}) != 1 {
		t.Fatal("refuse")
	}
	if runWatch([]string{"-bogus"}) != 2 {
		t.Fatal("watch parse")
	}
	if runWatch([]string{"--home", badHome}) != 1 {
		t.Fatal("watch open")
	}
	if runMCP([]string{"-bogus"}) != 2 {
		t.Fatal("mcp parse")
	}
	if runInstallMCP([]string{"-bogus"}) != 2 {
		t.Fatal("install-mcp parse")
	}
	if runSetup([]string{"-bogus"}) != 2 {
		t.Fatal("setup parse")
	}
	if runDoctor([]string{"-bogus"}) != 2 {
		t.Fatal("doctor parse")
	}
	if runInspect([]string{"-bogus"}) != 2 {
		t.Fatal("inspect parse")
	}
	if runToken([]string{"-bogus"}) != 2 {
		t.Fatal("token parse")
	}
}

func TestSetupStaysLocalDespiteEnv(t *testing.T) {
	user := t.TempDir()
	data := filepath.Join(user, ".lossless")
	t.Setenv("HOME", user)
	t.Setenv("LOSSLESS_HOME", data)
	t.Setenv("LOSSLESS_URL", "https://home.example")
	t.Setenv("LOSSLESS_TOKEN", "sekrit")
	if runSetup([]string{"--home", data, "--no-service", "--no-start"}) != 0 {
		t.Fatal("setup")
	}
	b, err := os.ReadFile(filepath.Join(user, ".grok", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "home.example") || strings.Contains(s, "sekrit") || strings.Contains(s, "Authorization") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "127.0.0.1") {
		t.Fatal(s)
	}
}

func TestRunInstallHooks(t *testing.T) {
	if runInstallHooks([]string{"-bogus"}) != 2 {
		t.Fatal("parse")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	fileHome := filepath.Join(t.TempDir(), "filehome")
	if err := os.WriteFile(fileHome, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", fileHome)
	if runInstallHooks(nil) != 1 {
		t.Fatal("mkdir")
	}
	t.Setenv("HOME", home)
	hooksDir := filepath.Join(home, ".grok", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(hooksDir, "lossless.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runInstallHooks(nil) != 1 {
		t.Fatal("write file")
	}
	_ = os.RemoveAll(filepath.Join(hooksDir, "lossless.json"))
	if runInstallMCP(nil) != 0 {
		t.Fatal("install-mcp")
	}
	gb, err := os.ReadFile(filepath.Join(home, ".grok", "config.toml"))
	if err != nil || !strings.Contains(string(gb), "lossless") {
		t.Fatalf("grok mcp %s %v", gb, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatal("codex mcp", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi", "agent", "mcp.json")); err != nil {
		t.Fatal("pi mcp", err)
	}
	if runInstallHooks(nil) != 0 {
		t.Fatal("install")
	}
	p := filepath.Join(home, ".grok", "hooks", "lossless.json")
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "hook-grok") {
		t.Fatalf("%s %v", b, err)
	}
	cb, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(cb), "hook-claude") {
		t.Fatalf("claude settings %s %v", cb, err)
	}
	xb, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil || !strings.Contains(string(xb), "hook-codex") {
		t.Fatalf("codex hooks %s %v", xb, err)
	}
	pb, err := os.ReadFile(filepath.Join(home, ".pi", "agent", "extensions", "lossless.ts"))
	if err != nil || !strings.Contains(string(pb), "hook-pi") {
		t.Fatalf("pi ext %s %v", pb, err)
	}
	ob, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "plugins", "lossless.ts"))
	if err != nil || !strings.Contains(string(ob), "session.idle") {
		t.Fatalf("opencode plugin %s %v", ob, err)
	}
}

func TestRunHookGrok(t *testing.T) {
	old := os.Stdin
	t.Cleanup(func() { os.Stdin = old })
	// Isolated home + no sidecar. Default sidecar is the live daemon.
	t.Setenv("LOSSLESS_SIDECAR", "off")
	t.Setenv("LOSSLESS_HOME", t.TempDir())

	setStdin := func(s string) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, s); err != nil {
			t.Fatal(err)
		}
		_ = w.Close()
		os.Stdin = r
	}

	setStdin("")
	if runHookClaude() != 0 {
		t.Fatal("claude empty")
	}
	setStdin("not-json")
	if runHookClaude() != 0 {
		t.Fatal("claude bad json")
	}
	setStdin(`{"session_id":"s","cwd":"/tmp/nope","hook_event_name":"Stop"}`)
	if runHookClaude() != 0 {
		t.Fatal("claude missing file")
	}

	setStdin("")
	if runHookGrok() != 0 {
		t.Fatal("empty")
	}
	setStdin("not-json")
	if runHookGrok() != 0 {
		t.Fatal("bad json")
	}
	setStdin(`{"sessionId":"s","cwd":"/tmp/nope","hookEventName":"PreCompact"}`)
	if runHookGrok() != 0 {
		t.Fatal("missing file")
	}

	root := t.TempDir()
	t.Setenv("GROK_HOME", root)
	mem := t.TempDir()
	t.Setenv("LOSSLESS_HOME", mem)
	ws := t.TempDir()
	sid := "sess1"
	loc := harness.LocateGrok(ws, sid)
	if err := os.MkdirAll(filepath.Dir(loc.JSONL), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(loc.JSONL, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	setStdin(`{"sessionId":"` + sid + `","workspaceRoot":` + jsonStr(ws) + `,"hookEventName":"Stop"}`)
	if runHookGrok() != 0 {
		t.Fatal("stop")
	}
	setStdin(`{"sessionId":"` + sid + `","cwd":` + jsonStr(ws) + `,"hookEventName":"SessionEnd"}`)
	if runHookGrok() != 0 {
		t.Fatal("end")
	}
	setStdin(`{"sessionId":"` + sid + `","cwd":` + jsonStr(ws) + `}`)
	if runHookGrok() != 0 {
		t.Fatal("default compact")
	}
	setStdin(`{"cwd":` + jsonStr(ws) + `,"hookEventName":"pre_compact"}`)
	t.Setenv("GROK_SESSION_ID", sid)
	if runHookGrok() != 0 {
		t.Fatal("pre_compact env session")
	}
	t.Setenv("GROK_SESSION_ID", "")
	setStdin(`{"sessionId":"` + sid + `","cwd":` + jsonStr(ws) + `,"hookEventName":"PostCompact"}`)
	if runHookGrok() != 0 {
		t.Fatal("post compact")
	}

	croot := t.TempDir()
	t.Setenv("CLAUDE_HOME", croot)
	cloc := harness.LocateClaude("", "csess", ws)
	if err := os.MkdirAll(filepath.Dir(cloc.JSONL), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cloc.JSONL, []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}]}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setStdin(`{"session_id":"csess","transcript_path":` + jsonStr(cloc.JSONL) + `,"cwd":` + jsonStr(ws) + `,"hook_event_name":"Stop"}`)
	if runHookClaude() != 0 {
		t.Fatal("claude stop")
	}
	setStdin(`{"sessionId":"csess","cwd":` + jsonStr(ws) + `,"hookEventName":"SessionEnd"}`)
	if runHookClaude() != 0 {
		t.Fatal("claude end")
	}

	xroot := t.TempDir()
	t.Setenv("CODEX_HOME", xroot)
	xsid := "019ef634-9af9-72d2-b01c-97d349693335"
	xdir := filepath.Join(xroot, "sessions", "2026", "08", "14")
	if err := os.MkdirAll(xdir, 0o755); err != nil {
		t.Fatal(err)
	}
	xp := filepath.Join(xdir, "rollout-2026-08-14T12-00-00-"+xsid+".jsonl")
	if err := os.WriteFile(xp, []byte(`{"type":"session_meta","payload":{"id":"`+xsid+`","cwd":`+jsonStr(ws)+`}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"agent_message","message":"We decided to use jose, not jsonwebtoken, for Edge."}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setStdin("")
	if runHookCodex() != 0 {
		t.Fatal("codex empty")
	}
	setStdin("not-json")
	if runHookCodex() != 0 {
		t.Fatal("codex bad json")
	}
	setStdin(`{"session_id":"` + xsid + `","transcript_path":` + jsonStr(xp) + `,"cwd":` + jsonStr(ws) + `,"hook_event_name":"Stop"}`)
	if runHookCodex() != 0 {
		t.Fatal("codex stop")
	}
	setStdin(`{"session_id":"` + xsid + `","cwd":` + jsonStr(ws) + `,"hook_event_name":"SessionEnd"}`)
	if runHookCodex() != 0 {
		t.Fatal("codex end")
	}
	setStdin(`{"session_id":"` + xsid + `","transcript_path":` + jsonStr(xp) + `,"cwd":` + jsonStr(ws) + `,"hook_event_name":"PreCompact"}`)
	if runHookCodex() != 0 {
		t.Fatal("codex precompact")
	}

	t.Setenv("LOSSLESS_HOME", "")
	t.Setenv("HOME", t.TempDir())
	setStdin(`{"sessionId":"` + sid + `","cwd":` + jsonStr(ws) + `,"hookEventName":"stop"}`)
	if runHookGrok() != 0 {
		t.Fatal("home default")
	}

	// empty workspace falls through to Getwd
	setStdin(`{"sessionId":"` + sid + `","hookEventName":"PreCompact"}`)
	if runHookGrok() != 0 {
		t.Fatal("getwd")
	}

	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOSSLESS_HOME", blocked)
	setStdin(`{"sessionId":"` + sid + `","cwd":` + jsonStr(ws) + `,"hookEventName":"Stop"}`)
	if runHookGrok() != 0 {
		t.Fatal("open fail-open")
	}

	mem = t.TempDir()
	t.Setenv("LOSSLESS_HOME", mem)
	t.Setenv("LOSSLESS_SIDECAR", "http://127.0.0.1:1")
	proot := t.TempDir()
	t.Setenv("PI_HOME", proot)
	psid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	pdir := filepath.Join(proot, "agent", "sessions", harness.PiSessionSlug(ws))
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	pp := filepath.Join(pdir, "2024-12-03T14-00-00_"+psid+".jsonl")
	if err := os.WriteFile(pp, []byte(`{"type":"session","version":3,"id":"`+psid+`","cwd":`+jsonStr(ws)+`}`+"\n"+
		`{"type":"message","id":"a1","parentId":null,"message":{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setStdin("")
	if runHookPi() != 0 {
		t.Fatal("pi empty")
	}
	setStdin("not-json")
	if runHookPi() != 0 {
		t.Fatal("pi bad")
	}
	setStdin(`{"session_id":"` + psid + `","transcript_path":` + jsonStr(pp) + `,"cwd":` + jsonStr(ws) + `,"hook_event_name":"turn"}`)
	if runHookPi() != 0 {
		t.Fatal("pi turn")
	}
	setStdin(`{"session_id":"` + psid + `","cwd":` + jsonStr(ws) + `,"hook_event_name":"session_end"}`)
	if runHookPi() != 0 {
		t.Fatal("pi end")
	}
	setStdin(`{"session_id":"` + psid + `","transcript_path":` + jsonStr(pp) + `,"cwd":` + jsonStr(ws) + `,"hook_event_name":"compact"}`)
	if runHookPi() != 0 {
		t.Fatal("pi compact")
	}
	setStdin("")
	if runHookOpenCode() != 0 {
		t.Fatal("oc empty")
	}
	setStdin("not-json")
	if runHookOpenCode() != 0 {
		t.Fatal("oc bad")
	}
	setStdin(`{"session_id":"ses_x","cwd":` + jsonStr(ws) + `,"hook_event_name":"session.idle"}`)
	if runHookOpenCode() != 0 {
		t.Fatal("oc idle")
	}
	if runEnsure([]string{"-bogus"}) != 2 {
		t.Fatal("ensure parse")
	}
	if runEnsure([]string{"--home", mem}) != 0 {
		t.Fatal("ensure")
	}
}

func TestHookSource(t *testing.T) {
	if hookSource("pre_compact", "turn") != "compact" || hookSource("PostCompact", "turn") != "compact" {
		t.Fatal("compact")
	}
	if hookSource("stop", "compact") != "turn" || hookSource("session.idle", "compact") != "turn" {
		t.Fatal("turn")
	}
	if hookSource("session_end", "turn") != "session_end" {
		t.Fatal("end")
	}
	if hookSource("", "compact") != "compact" || hookSource("other", "") != "turn" {
		t.Fatal("fallback")
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestMainDispatch(t *testing.T) {
	if os.Getenv("AM_BE_MAIN") == "1" {
		os.Args = append([]string{"lossless"}, strings.Fields(os.Getenv("AM_ARGS"))...)
		main()
		return
	}
	run := func(args string) (string, int) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=TestMainDispatch")
		cmd.Env = append(os.Environ(), "AM_BE_MAIN=1", "AM_ARGS="+args, "HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		return string(out), code
	}
	if _, code := run(""); code != 2 {
		t.Fatalf("no args code=%d", code)
	}
	out, code := run("help")
	if code != 0 || !strings.Contains(out, "catch-up") {
		t.Fatalf("help %d %s", code, out)
	}
	if _, code := run("-h"); code != 0 {
		t.Fatal(code)
	}
	if _, code := run("--help"); code != 0 {
		t.Fatal(code)
	}
	out, code = run("nope")
	if code != 2 || !strings.Contains(out, "unknown command") {
		t.Fatalf("unknown %d %s", code, out)
	}
	home := t.TempDir()
	_, code = run("ask --home " + home + " --project acme/api --question jose")
	if code != 0 {
		t.Fatalf("ask via main %d", code)
	}
	_, code = run("remember --home " + home + " --type decision --text Use-jose-not-jsonwebtoken-please --project acme/api")
	if code != 0 {
		t.Fatalf("remember via main %d", code)
	}
	src := filepath.Join(t.TempDir(), "c.jsonl")
	if err := os.WriteFile(src, []byte(`{"type":"user","content":"Always use jose now."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, code = run("catch-up --home " + home + " --jsonl " + src + " --project acme/api")
	if code != 0 {
		t.Fatalf("catch-up via main %d", code)
	}
	_, code = run("serve --home " + home + " --listen 0.0.0.0:9")
	if code != 1 {
		t.Fatalf("serve via main %d", code)
	}
	_, code = run("hook-grok")
	if code != 0 {
		t.Fatalf("hook-grok via main %d", code)
	}
	_, code = run("hook-claude")
	if code != 0 {
		t.Fatalf("hook-claude via main %d", code)
	}
	_, code = run("hook-codex")
	if code != 0 {
		t.Fatalf("hook-codex via main %d", code)
	}
	_, code = run("mcp")
	if code != 0 {
		t.Fatalf("mcp via main %d", code)
	}
	_, code = run("install-mcp")
	if code != 0 {
		t.Fatalf("install-mcp via main %d", code)
	}
	_, code = run("install-hooks")
	if code != 0 {
		t.Fatalf("install-hooks via main %d", code)
	}
	_, code = run("ensure --home " + home)
	if code != 0 {
		t.Fatalf("ensure via main %d", code)
	}
	_, code = run("hook-pi")
	if code != 0 {
		t.Fatalf("hook-pi via main %d", code)
	}
	_, code = run("hook-opencode")
	if code != 0 {
		t.Fatalf("hook-opencode via main %d", code)
	}
}
