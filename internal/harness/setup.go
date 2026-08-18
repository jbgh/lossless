package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"lossless/internal/store"
	"lossless/internal/version"
	"lossless/internal/write"
)

type SetupOpts struct {
	UserHome string // $HOME — harness config (hooks, MCP)
	DataHome string // LOSSLESS_HOME — store + serve --home
	Exe      string
	URL      string
	Token    string
	Service  bool
	Start    bool
}

type SetupResult struct {
	Wrote   []string
	Service string
	Daemon  string
	Hints   []string
}

func Setup(opts SetupOpts) (SetupResult, error) {
	var out SetupResult
	if opts.UserHome == "" {
		return out, fmt.Errorf("user home required")
	}
	if opts.DataHome == "" {
		return out, fmt.Errorf("data home required")
	}
	if opts.Exe == "" {
		return out, fmt.Errorf("executable required")
	}
	if err := CheckDaemonURL(opts.URL); err != nil {
		return out, err
	}
	hooks, err := InstallHooks(opts.UserHome, opts.Exe)
	if err != nil {
		return out, err
	}
	out.Wrote = append(out.Wrote, hooks...)
	mcp, err := InstallMCP(MCPConfig{Home: opts.UserHome, Exe: opts.Exe, URL: opts.URL, Token: opts.Token})
	out.Wrote = append(out.Wrote, mcp...)
	if err != nil && len(mcp) == 0 {
		return out, err
	}
	if err != nil {
		out.Hints = append(out.Hints, "mcp: "+err.Error())
	}

	if opts.Service {
		path, err := InstallUserService(opts.Exe, opts.UserHome, opts.DataHome, opts.URL, opts.Token)
		if err != nil {
			out.Hints = append(out.Hints, "service: "+err.Error())
		} else {
			out.Service = path
			out.Wrote = append(out.Wrote, path)
			if err := StartUserService(path); err != nil {
				out.Hints = append(out.Hints, "load service: "+err.Error())
			}
		}
	}

	base := DaemonBase(opts.URL)
	if opts.Start {
		if err := EnsureDaemon(opts.Exe, opts.DataHome, base); err != nil {
			out.Hints = append(out.Hints, "daemon: "+err.Error())
		}
	}
	if st, err := ProbeHealth(base); err == nil {
		out.Daemon = st
	} else {
		out.Hints = append(out.Hints, "start the daemon: "+opts.Exe+" serve --watch")
		if opts.Start {
			out.Hints = append(out.Hints, "Start a new agent session so MCP tools appear.")
			out.Hints = append(out.Hints, "Grok: /hooks then r")
			return out, fmt.Errorf("daemon did not start at %s", base)
		}
	}
	out.Hints = append(out.Hints, "Start a new agent session so MCP tools and the lossless skill appear.")
	out.Hints = append(out.Hints, "Grok: /hooks then r. Skills load from disk; /skills then r if this session is already open.")
	return out, nil
}

func (r SetupResult) Format() string {
	var b strings.Builder
	for _, p := range r.Wrote {
		fmt.Fprintf(&b, "wrote %s\n", p)
	}
	if r.Service != "" {
		fmt.Fprintf(&b, "service %s\n", r.Service)
	}
	if r.Daemon != "" {
		fmt.Fprintf(&b, "daemon %s\n", r.Daemon)
	}
	for _, h := range r.Hints {
		fmt.Fprintf(&b, "%s\n", h)
	}
	return b.String()
}

type Check struct {
	Name   string
	OK     bool
	Detail string
}

type Report struct {
	Checks []Check
}

func (r Report) Ok() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

func (r Report) Format() string {
	var b strings.Builder
	for _, c := range r.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "  %-10s %s  %s\n", c.Name, mark, c.Detail)
	}
	if r.Ok() {
		b.WriteString("\nStart a new agent session so MCP tools appear. Grok: /hooks then r\n")
	}
	return b.String()
}

func Doctor(userHome, dataHome, exe, url, token string) Report {
	var r Report
	add := func(name string, ok bool, detail string) {
		r.Checks = append(r.Checks, Check{Name: name, OK: ok, Detail: detail})
	}
	if exe == "" {
		add("binary", false, "lossless executable not found")
	} else {
		add("binary", true, exe)
	}
	add("version", true, version.Version)
	if dataHome == "" {
		add("home", false, "unset")
	} else if st, err := os.Stat(dataHome); err != nil || !st.IsDir() {
		add("home", false, dataHome+" (missing — run lossless setup)")
	} else {
		add("home", true, dataHome)
	}
	base := DaemonBase(url)
	daemonOK := false
	if st, err := ProbeHealth(base); err != nil {
		add("daemon", false, base+" not reachable")
	} else {
		add("daemon", true, st)
		daemonOK = true
	}
	if remoteHTTP(base) && !strings.HasPrefix(base, "https://") {
		add("url", false, base+" remote home must be https")
	} else if remoteHTTP(base) && token == "" {
		add("url", false, base+" remote home needs LOSSLESS_TOKEN")
	} else if remoteHTTP(base) {
		if err := write.ProbeHome(base, token); err != nil {
			add("url", false, err.Error())
		} else {
			add("url", true, base+" (https, token ok)")
		}
		if st, err := ProbeHealth("http://127.0.0.1:7432"); err != nil {
			add("sidecar", false, "local serve not up — hooks still need lossless serve")
		} else {
			add("sidecar", true, st)
		}
	} else {
		add("url", true, "loopback")
	}

	hookOK, hookDetail := checkFiles(map[string]string{
		"grok":     filepath.Join(userHome, ".grok", "hooks", "lossless.json"),
		"claude":   filepath.Join(userHome, ".claude", "settings.json"),
		"codex":    filepath.Join(userHome, ".codex", "hooks.json"),
		"pi":       filepath.Join(userHome, ".pi", "agent", "extensions", "lossless.ts"),
		"opencode": opencodePluginPath(userHome),
	}, "hook")
	add("hooks", hookOK, hookDetail)

	mcpOK, mcpDetail := checkFiles(map[string]string{
		"grok":     filepath.Join(userHome, ".grok", "config.toml"),
		"claude":   filepath.Join(userHome, ".claude.json"),
		"codex":    filepath.Join(userHome, ".codex", "config.toml"),
		"pi":       filepath.Join(userHome, ".pi", "agent", "mcp.json"),
		"opencode": opencodeJSONPath(userHome),
	}, "lossless")
	add("mcp", mcpOK, mcpDetail)

	skillOK, skillDetail := checkFiles(SkillDests(userHome), "ask")
	add("skills", skillOK, skillDetail)

	ruleOK, ruleDetail := checkFiles(RuleDests(userHome), "ask")
	add("rules", ruleOK, ruleDetail)

	if dataHome != "" {
		if _, err := os.Stat(filepath.Join(dataHome, "index", "claims.sqlite")); err == nil {
			if st, err := store.Open(dataHome); err == nil {
				n := st.CountActive()
				last := st.LastAskAt("")
				_ = st.Close()
				switch {
				case last != "":
					add("ask", true, "last "+last)
				case n > 0:
					add("ask", true, fmt.Sprintf("none yet; tape has %d claims", n))
				default:
					add("ask", true, "no tape yet")
				}
			}
		}
	}

	svc := ServicePath(userHome)
	if svc != "" {
		if _, err := os.Stat(svc); err == nil {
			add("service", true, svc)
		} else if daemonOK {
			add("service", true, "no unit; daemon is running")
		} else {
			add("service", false, "not installed — lossless setup")
		}
	} else if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		add("service", true, "no user service on "+runtime.GOOS)
	}
	return r
}

func checkFiles(files map[string]string, needle string) (bool, string) {
	var ok, missing []string
	order := []string{"grok", "claude", "codex", "pi", "opencode", "agents"}
	for _, name := range order {
		p, exists := files[name]
		if !exists {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil || (!strings.Contains(string(b), needle) && !strings.Contains(string(b), "lossless")) {
			missing = append(missing, name)
			continue
		}
		ok = append(ok, name)
	}
	if len(missing) == 0 {
		return true, strings.Join(ok, " ")
	}
	if len(ok) == 0 {
		return false, "none — lossless setup"
	}
	return false, "ok " + strings.Join(ok, " ") + "; missing " + strings.Join(missing, " ")
}

func opencodePluginPath(home string) string {
	cfg := os.Getenv("OPENCODE_CONFIG")
	if cfg == "" {
		cfg = filepath.Join(home, ".config", "opencode")
	}
	return filepath.Join(cfg, "plugins", "lossless.ts")
}

func opencodeJSONPath(home string) string {
	cfg := os.Getenv("OPENCODE_CONFIG")
	if cfg == "" {
		cfg = filepath.Join(home, ".config", "opencode")
	}
	p := filepath.Join(cfg, "opencode.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if _, err := os.Stat(p + "c"); err == nil {
		return p + "c"
	}
	return p
}

func ProbeHealth(base string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid daemon URL")
	}
	u := strings.TrimRight(base, "/") + "/health"
	client := &http.Client{
		Timeout: 400 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("redirects disabled")
		},
	}
	res, err := client.Get(u)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("status %d", res.StatusCode)
	}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	var body struct {
		OK       *bool  `json:"ok"`
		Records  *int   `json:"records"`
		Embedder string `json:"embedder"`
		Version  string `json:"version"`
	}
	if json.Unmarshal(raw, &body) != nil || body.OK == nil || !*body.OK || body.Records == nil {
		return "", fmt.Errorf("not a lossless daemon")
	}
	emb := body.Embedder
	if emb == "" {
		emb = "none"
	}
	if body.Version != "" {
		return fmt.Sprintf("%s records=%d embedder=%s version=%s", base, *body.Records, emb, body.Version), nil
	}
	return fmt.Sprintf("%s records=%d embedder=%s", base, *body.Records, emb), nil
}

func EnsureDaemon(exe, home, base string) error {
	if _, err := ProbeHealth(base); err == nil {
		return nil
	}
	_ = StartUserService("")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := ProbeHealth(base); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := spawnServe(exe, home); err != nil {
		return err
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := ProbeHealth(base); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not come up at %s", base)
}

func spawnServe(exe, home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(home, "serve.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "serve", "--watch", "--home", home)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Env = os.Environ()
	detach(cmd)
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return err
	}
	go func() {
		_ = cmd.Wait()
		_ = log.Close()
	}()
	return nil
}
