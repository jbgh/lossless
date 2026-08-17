package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"context"

	"lossless/internal/bench"
	"lossless/internal/claim"
	"lossless/internal/env"
	"lossless/internal/harness"
	"lossless/internal/mcpserver"
	"lossless/internal/retrieve"
	"lossless/internal/serve"
	"lossless/internal/store"
	"lossless/internal/watch"
	"lossless/internal/write"
)

type stringsFlag []string

func (s *stringsFlag) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringsFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "catch-up":
		os.Exit(runCatchUp(args))
	case "remember":
		os.Exit(runRemember(args))
	case "ask":
		os.Exit(runAsk(args))
	case "serve":
		os.Exit(runServe(args))
	case "hook-grok":
		os.Exit(runHookGrok())
	case "hook-claude":
		os.Exit(runHookClaude())
	case "hook-codex":
		os.Exit(runHookCodex())
	case "watch":
		os.Exit(runWatch(args))
	case "mcp":
		os.Exit(runMCP(args))
	case "install-mcp":
		os.Exit(runInstallMCP(args))
	case "install-hooks":
		os.Exit(runInstallHooks(args))
	case "setup":
		os.Exit(runSetup(args))
	case "doctor":
		os.Exit(runDoctor(args))
	case "token":
		os.Exit(runToken(args))
	case "ensure":
		os.Exit(runEnsure(args))
	case "hook-pi":
		os.Exit(runHookPi())
	case "hook-opencode":
		os.Exit(runHookOpenCode())
	case "bench":
		os.Exit(runBench(args))
	case "embed-backfill":
		os.Exit(runEmbedBackfill(args))
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `lossless — work log for coding agents (keep the tape, check out five records)

  lossless bench [--root testdata/bench] [--home DIR]
  lossless catch-up --jsonl FILE [--project KEY] [--workspace DIR] [--harness grok] [--session ID] [--home DIR]
  lossless remember --type decision --text "..." [--project KEY] [--workspace DIR] [--home DIR]
  lossless ask --project KEY [--question "..."] [--goal "..."] [--path FILE] [--session ID] [--workspace DIR]
  lossless serve [--listen 127.0.0.1:7432] [--token TOKEN] [--watch]
  lossless mcp                # stdio MCP client of the daemon (ask, remember, get_record)
  lossless watch              # poll harness session files
  lossless hook-grok          # stdin: Grok hook JSON; fail-open
  lossless hook-claude        # stdin: Claude hook JSON; fail-open
  lossless hook-codex         # stdin: Codex hook JSON; fail-open
  lossless setup              # local hooks + MCP + skill + optional user service
  lossless doctor             # daemon, hooks, MCP, service
  lossless token              # optional: print a random bearer
  lossless install-hooks      # Grok + Claude + Codex + Pi + OpenCode
  lossless install-mcp        # MCP for every supported harness (or point any MCP client at /mcp)
  lossless ensure             # replay spool after sidecar was down
  lossless embed-backfill     # embed active claims if an on-box model is configured
  lossless hook-pi            # stdin: Pi extension JSON; fail-open
  lossless hook-opencode      # stdin: OpenCode plugin JSON; fail-open

Env: LOSSLESS_HOME (default ~/.lossless)
     LOSSLESS_URL (default http://127.0.0.1:7432) — stdio mcp talks to the daemon
     LOSSLESS_TOKEN (required if --listen is not loopback)
     LOSSLESS_EMBED_CMD (optional local embedder: stdin JSON texts, stdout JSON vectors)
     LOSSLESS_EMBED_MODEL (optional model dir; in-process MiniLM not required)
Default install is local (127.0.0.1, no token, nothing leaves the machine).
MCP is a façade over the same JSON as /v1/ask.
`)
}

func openStore(home string) (*store.Store, error) {
	st, err := store.Open(home)
	if err != nil {
		return nil, err
	}
	store.AttachEmbedder(st, home)
	return st, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func homeFlag(fs *flag.FlagSet) *string {
	return fs.String("home", env.Home(), "data dir")
}

func runCatchUp(args []string) int {
	fs := flag.NewFlagSet("catch-up", flag.ContinueOnError)
	home := homeFlag(fs)
	jsonl := fs.String("jsonl", "", "harness session JSONL")
	project := fs.String("project", "", "owner/repo")
	ws := fs.String("workspace", "", "workspace root")
	harnessName := fs.String("harness", "other", "grok|claude|pi|opencode|codex")
	session := fs.String("session", "", "session id")
	source := fs.String("source", "compact", "turn|compact|session_end|import")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	res, err := write.CatchUp(st, write.CatchUpRequest{
		JSONL: *jsonl, Project: *project, WorkspaceRoot: *ws,
		Harness: *harnessName, SessionID: *session, Source: *source,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
	return 0
}

func runRemember(args []string) int {
	fs := flag.NewFlagSet("remember", flag.ContinueOnError)
	home := homeFlag(fs)
	typ := fs.String("type", "", "failed|decision|constraint|state|thread")
	text := fs.String("text", "", "durable claim")
	project := fs.String("project", "", "owner/repo")
	ws := fs.String("workspace", "", "workspace root")
	path := fs.String("path", "", "repo-relative path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var recPaths []string
	if *path != "" {
		recPaths = []string{*path}
	}
	rec := claim.Record{
		Type: *typ, Text: *text, ProjectKey: *project, WorkspaceRoot: *ws, Paths: recPaths, Harness: "other",
	}
	if write.HomeIsRemote() {
		res, err := mcpserver.HTTP{BaseURL: write.HomeURL(), Token: env.Token()}.Remember(rec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		return 0
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	res, err := write.Remember(st, rec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
	return 0
}

func runAsk(args []string) int {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	home := homeFlag(fs)
	project := fs.String("project", "", "owner/repo")
	ws := fs.String("workspace", "", "workspace root")
	question := fs.String("question", "", "natural-language question")
	goal := fs.String("goal", "", "what the agent is about to do")
	limit := fs.Int("limit-tokens", retrieve.DefaultLimit, "packed token budget")
	session := fs.String("session", "", "session id for the action tape")
	var paths stringsFlag
	fs.Var(&paths, "path", "repo-relative path (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	req := retrieve.Request{
		Question: *question, Project: *project, WorkspaceRoot: *ws,
		Goal: *goal, Paths: paths, SessionID: *session, LimitTokens: *limit,
	}
	if write.HomeIsRemote() {
		out, err := mcpserver.HTTP{BaseURL: write.HomeURL(), Token: env.Token()}.Ask(req)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	out, err := retrieve.Ask(st, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return 0
}

func runBench(args []string) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	home := homeFlag(fs)
	root := fs.String("root", "testdata/bench", "benchmark case directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := os.MkdirAll(*home, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dir, err := os.MkdirTemp(*home, "bench-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rep, err := bench.RunDir(*root, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(bench.FormatReport(rep))
	if rep.CasePass != rep.CaseTotal || rep.AskPass != rep.AskTotal {
		return 1
	}
	return 0
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	home := homeFlag(fs)
	listen := fs.String("listen", serve.DefaultAddr, "bind address")
	token := fs.String("token", env.Token(), "bearer token (required if not loopback)")
	doWatch := fs.Bool("watch", true, "poll harness session files while serving")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	if st.Embedder != nil {
		go func() {
			n, err := st.BackfillVectors()
			if err != nil {
				fmt.Fprintf(os.Stderr, "embed backfill: %v\n", err)
				return
			}
			if n > 0 {
				fmt.Fprintf(os.Stderr, "embed backfill: %d claims (%s)\n", n, st.EmbedderName())
			}
		}()
	}
	fmt.Fprintf(os.Stderr, "lossless serve %s (home %s) watch=%v embedder=%s\n", *listen, *home, *doWatch, orNone(st.EmbedderName()))
	if err := serve.Listen(serve.Options{Addr: *listen, Token: *token, Watch: *doWatch}, st); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runWatch(args []string) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	home := homeFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	fmt.Fprintf(os.Stderr, "lossless watch (home %s)\n", *home)
	if err := watch.Run(context.Background(), st, watch.Defaults()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runMCP(args []string) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	url := fs.String("url", env.URL(), "daemon base URL")
	token := fs.String("token", env.Token(), "bearer token")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *url == "" {
		*url = "http://127.0.0.1:7432"
	}
	s := mcpserver.New(mcpserver.HTTP{BaseURL: *url, Token: *token})
	if err := s.ServeStdio(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func resolveExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		return r, nil
	}
	return exe, nil
}

func runInstallMCP(args []string) int {
	fs := flag.NewFlagSet("install-mcp", flag.ContinueOnError)
	url := fs.String("url", env.URL(), "daemon URL (Grok uses /mcp; others spawn lossless mcp)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	exe, err := resolveExe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	paths, err := harness.InstallMCP(harness.MCPConfig{
		Home: os.Getenv("HOME"), Exe: exe, URL: *url, Token: env.Token(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, p := range paths {
		fmt.Println("wrote", p)
	}
	fmt.Println("start the daemon: lossless serve --watch")
	fmt.Println("or: lossless setup")
	return 0
}

func runSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	home := homeFlag(fs)
	noService := fs.Bool("no-service", false, "do not install launchd/systemd user service")
	noStart := fs.Bool("no-start", false, "do not start the daemon")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	exe, err := resolveExe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	res, err := harness.Setup(harness.SetupOpts{
		UserHome: os.Getenv("HOME"), DataHome: *home, Exe: exe,
		Service: !*noService, Start: !*noStart,
	})
	fmt.Print(res.Format())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	home := homeFlag(fs)
	url := fs.String("url", env.URL(), "daemon URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	exe, err := resolveExe()
	if err != nil {
		exe = ""
	}
	rep := harness.Doctor(os.Getenv("HOME"), *home, exe, *url, env.Token())
	fmt.Print(rep.Format())
	if !rep.Ok() {
		return 1
	}
	return 0
}

func runToken(args []string) int {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	home := homeFlag(fs)
	writeEnv := fs.Bool("write", false, "also write LOSSLESS_TOKEN into service.env (0600)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tok, err := env.NewToken()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *writeEnv {
		if err := harness.WriteServiceEnv(*home, env.BaseURL(), tok); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintln(os.Stderr, "wrote", filepath.Join(*home, "service.env"))
	}
	fmt.Println(tok)
	return 0
}

const hookStdinLimit = 1 << 20

func readHookStdin() ([]byte, error) {
	return io.ReadAll(io.LimitReader(os.Stdin, hookStdinLimit))
}

func runHookGrok() int {
	// Fail-open: any error exits 0 so compact is never blocked.
	defer func() { _ = recover() }()
	raw, err := readHookStdin()
	if err != nil || len(raw) == 0 {
		return 0
	}
	var ev struct {
		SessionID     string `json:"sessionId"`
		CWD           string `json:"cwd"`
		WorkspaceRoot string `json:"workspaceRoot"`
		HookEventName string `json:"hookEventName"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return 0
	}
	sid := ev.SessionID
	if sid == "" {
		sid = os.Getenv("GROK_SESSION_ID")
	}
	ws := ev.WorkspaceRoot
	if ws == "" {
		ws = ev.CWD
	}
	if ws == "" {
		ws = os.Getenv("GROK_WORKSPACE_ROOT")
	}
	if ws == "" {
		ws, _ = os.Getwd()
	}
	loc := harness.LocateGrok(ws, sid)
	if _, err := os.Stat(loc.JSONL); err != nil {
		return 0
	}
	source := hookSource(ev.HookEventName, "compact")
	write.SubmitCatchUp(write.CatchUpRequest{
		JSONL: loc.JSONL, WorkspaceRoot: ws, Harness: "grok",
		SessionID: sid, Source: source,
	})
	return 0
}

func runHookClaude() int {
	defer func() { _ = recover() }()
	raw, err := readHookStdin()
	if err != nil || len(raw) == 0 {
		return 0
	}
	var ev struct {
		SessionID      string `json:"session_id"`
		SessionIDCamel string `json:"sessionId"`
		Transcript     string `json:"transcript_path"`
		CWD            string `json:"cwd"`
		HookEvent      string `json:"hook_event_name"`
		HookEventCamel string `json:"hookEventName"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return 0
	}
	sid := ev.SessionID
	if sid == "" {
		sid = ev.SessionIDCamel
	}
	name := ev.HookEvent
	if name == "" {
		name = ev.HookEventCamel
	}
	ws := ev.CWD
	if ws == "" {
		ws, _ = os.Getwd()
	}
	loc := harness.LocateClaude(ev.Transcript, sid, ws)
	if loc.JSONL == "" {
		return 0
	}
	if _, err := os.Stat(loc.JSONL); err != nil {
		return 0
	}
	source := hookSource(name, "compact")
	write.SubmitCatchUp(write.CatchUpRequest{
		JSONL: loc.JSONL, WorkspaceRoot: ws, Harness: "claude",
		SessionID: loc.SessionID, Source: source,
	})
	return 0
}

func runHookCodex() int {
	defer func() { _ = recover() }()
	defer func() { fmt.Println(`{"continue":true}`) }()
	raw, err := readHookStdin()
	if err != nil || len(raw) == 0 {
		return 0
	}
	var ev struct {
		SessionID      string `json:"session_id"`
		SessionIDCamel string `json:"sessionId"`
		Transcript     string `json:"transcript_path"`
		CWD            string `json:"cwd"`
		HookEvent      string `json:"hook_event_name"`
		HookEventCamel string `json:"hookEventName"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return 0
	}
	sid := ev.SessionID
	if sid == "" {
		sid = ev.SessionIDCamel
	}
	name := ev.HookEvent
	if name == "" {
		name = ev.HookEventCamel
	}
	ws := ev.CWD
	if ws == "" {
		ws, _ = os.Getwd()
	}
	loc := harness.LocateCodex(ev.Transcript, sid, ws)
	if loc.JSONL == "" {
		return 0
	}
	if _, err := os.Stat(loc.JSONL); err != nil {
		return 0
	}
	if ws == "" {
		_, ws = harness.PeekCodexMeta(loc.JSONL)
	}
	source := hookSource(name, "turn")
	write.SubmitCatchUp(write.CatchUpRequest{
		JSONL: loc.JSONL, WorkspaceRoot: ws, Harness: "codex",
		SessionID: loc.SessionID, Source: source,
	})
	return 0
}

func runInstallHooks(args []string) int {
	fs := flag.NewFlagSet("install-hooks", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	exe, err := resolveExe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	wrote, err := harness.InstallHooks(os.Getenv("HOME"), exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, d := range wrote {
		fmt.Println("wrote", d)
	}
	return 0
}

func runEnsure(args []string) int {
	fs := flag.NewFlagSet("ensure", flag.ContinueOnError)
	home := homeFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	res, err := write.Ensure(st, *home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
	return 0
}

func runEmbedBackfill(args []string) int {
	fs := flag.NewFlagSet("embed-backfill", flag.ContinueOnError)
	home := homeFlag(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := openStore(*home)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer st.Close()
	if st.Embedder == nil {
		fmt.Fprintln(os.Stderr, "no embedder (set LOSSLESS_EMBED_CMD or install a model)")
		return 1
	}
	n, err := st.BackfillVectors()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("embedded %d claims with %s\n", n, st.EmbedderName())
	return 0
}

func runHookPi() int {
	defer func() { _ = recover() }()
	raw, err := readHookStdin()
	if err != nil || len(raw) == 0 {
		return 0
	}
	var ev struct {
		SessionID  string `json:"session_id"`
		Transcript string `json:"transcript_path"`
		CWD        string `json:"cwd"`
		HookEvent  string `json:"hook_event_name"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return 0
	}
	ws := ev.CWD
	if ws == "" {
		ws, _ = os.Getwd()
	}
	loc := harness.LocatePi(ev.Transcript, ev.SessionID, ws)
	if loc.JSONL == "" {
		return 0
	}
	if _, err := os.Stat(loc.JSONL); err != nil {
		return 0
	}
	source := hookSource(ev.HookEvent, "turn")
	write.SubmitCatchUp(write.CatchUpRequest{
		JSONL: loc.JSONL, WorkspaceRoot: ws, Harness: "pi",
		SessionID: loc.SessionID, Source: source,
	})
	return 0
}

func runHookOpenCode() int {
	defer func() { _ = recover() }()
	raw, err := readHookStdin()
	if err != nil || len(raw) == 0 {
		return 0
	}
	var ev struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Directory string `json:"directory"`
		HookEvent string `json:"hook_event_name"`
		Source    string `json:"source"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return 0
	}
	ws := ev.CWD
	if ws == "" {
		ws = ev.Directory
	}
	source := ev.Source
	if source == "" {
		source = hookSource(ev.HookEvent, "turn")
	}
	write.SubmitCatchUp(write.CatchUpRequest{
		WorkspaceRoot: ws, Harness: "opencode",
		SessionID: ev.SessionID, Source: source,
	})
	return 0
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func hookSource(name, fallback string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case n == "session_end" || n == "sessionend" || n == "session_shutdown" || n == "session.deleted":
		return "session_end"
	case n == "stop" || n == "turn" || n == "turn_end" || n == "agent_settled" || n == "session.idle":
		return "turn"
	case strings.Contains(n, "compact"):
		return "compact"
	default:
		if fallback != "" {
			return fallback
		}
		return "turn"
	}
}
