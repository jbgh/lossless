package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"lossless/internal/env"
)

const defaultDaemon = "http://127.0.0.1:7432"

// MCPConfig is the user-level MCP install for every harness we support.
// The raw bearer is never written to a config file. Grok interpolates
// ${LOSSLESS_TOKEN}; stdio servers inherit the process environment.
type MCPConfig struct {
	Home  string
	Exe   string
	URL   string // daemon base or .../mcp
	Token string // if set, wire header/env refs — value stays out of files
}

func DaemonBase(raw string) string {
	s := env.CanonicalURL(raw)
	if s == "" {
		return defaultDaemon
	}
	return s
}

func MCPEndpoint(raw string) string {
	return DaemonBase(raw) + "/mcp"
}

func stdioEnv(cfg MCPConfig) map[string]string {
	base := DaemonBase(cfg.URL)
	if base == defaultDaemon || base == "http://localhost:7432" {
		return nil
	}
	return map[string]string{"LOSSLESS_URL": base}
}

func CheckDaemonURL(raw string) error {
	base := DaemonBase(raw)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid daemon URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("daemon URL must be http or https")
	}
	if remoteHTTP(base) && u.Scheme != "https" {
		return fmt.Errorf("remote URL must be https")
	}
	return nil
}

func needsMCPAuth(cfg MCPConfig) bool {
	return strings.TrimSpace(cfg.Token) != "" || remoteHTTP(DaemonBase(cfg.URL))
}

func remoteHTTP(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" || host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}
	return true
}

func InstallMCP(cfg MCPConfig) ([]string, error) {
	if cfg.Home == "" {
		return nil, fmt.Errorf("home required")
	}
	if cfg.Exe == "" {
		return nil, fmt.Errorf("executable required")
	}
	if err := CheckDaemonURL(cfg.URL); err != nil {
		return nil, err
	}
	env := stdioEnv(cfg)
	auth := needsMCPAuth(cfg)
	var out []string
	var errs []error
	writers := []func() (string, error){
		func() (string, error) { return WriteGrokMCP(cfg.Home, MCPEndpoint(cfg.URL), auth) },
		func() (string, error) { return WriteClaudeMCP(cfg.Home, cfg.Exe, env) },
		func() (string, error) { return WriteCodexMCP(cfg.Home, cfg.Exe, env) },
		func() (string, error) { return WritePiMCP(cfg.Home, cfg.Exe, env) },
		func() (string, error) { return WriteOpenCodeMCP(cfg.Home, cfg.Exe, env) },
	}
	for _, w := range writers {
		p, err := w()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	return out, errors.Join(errs...)
}

func MergeClaudeMCP(existing []byte, exe string, env map[string]string) ([]byte, error) {
	root := map[string]any{}
	trim := bytes.TrimSpace(existing)
	if len(trim) > 0 {
		if err := json.Unmarshal(trim, &root); err != nil {
			return nil, err
		}
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	entry := map[string]any{
		"command": exe,
		"args":    []string{"mcp"},
	}
	if len(env) > 0 {
		entry["env"] = env
	}
	servers["lossless"] = entry
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func WriteClaudeMCP(home, exe string, env map[string]string) (string, error) {
	dest := filepath.Join(home, ".claude.json")
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	merged, err := MergeClaudeMCP(existing, exe, env)
	if err != nil {
		return "", err
	}
	if err := writeUserConfig(dest, merged, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func MergeGrokMCP(existing []byte, url string, auth bool) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.lossless]\nurl = %q\n", url)
	if auth {
		b.WriteString("headers = { Authorization = \"Bearer ${LOSSLESS_TOKEN}\" }\n")
	}
	return upsertTOMLTable(existing, "mcp_servers.lossless", b.String())
}

func WriteGrokMCP(home, url string, auth bool) (string, error) {
	destDir := filepath.Join(home, ".grok")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, "config.toml")
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	if err := writeUserConfig(dest, MergeGrokMCP(existing, url, auth), 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func MergeCodexMCP(existing []byte, exe string, env map[string]string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.lossless]\ncommand = %q\nargs = [\"mcp\"]\n", exe)
	if len(env) > 0 {
		b.WriteString("\n[mcp_servers.lossless.env]\n")
		for k, v := range env {
			fmt.Fprintf(&b, "%s = %q\n", k, v)
		}
	}
	return upsertTOMLTable(existing, "mcp_servers.lossless", b.String())
}

func WriteCodexMCP(home, exe string, env map[string]string) (string, error) {
	dest := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	if err := writeUserConfig(dest, MergeCodexMCP(existing, exe, env), 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func MergeJSONMCPServers(existing []byte, exe string, env map[string]string) ([]byte, error) {
	root := map[string]any{}
	trim := bytes.TrimSpace(existing)
	if len(trim) > 0 {
		if err := json.Unmarshal(trim, &root); err != nil {
			return nil, err
		}
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	entry := map[string]any{"command": exe, "args": []string{"mcp"}}
	if len(env) > 0 {
		entry["env"] = env
	}
	servers["lossless"] = entry
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func WritePiMCP(home, exe string, env map[string]string) (string, error) {
	dest := filepath.Join(home, ".pi", "agent", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	merged, err := MergeJSONMCPServers(existing, exe, env)
	if err != nil {
		return "", err
	}
	if err := writeUserConfig(dest, merged, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

func MergeOpenCodeMCP(existing []byte, exe string, env map[string]string) ([]byte, error) {
	root := map[string]any{}
	trim := bytes.TrimSpace(existing)
	if len(trim) > 0 {
		if err := json.Unmarshal(trim, &root); err != nil {
			return nil, err
		}
	}
	mcp, _ := root["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
		root["mcp"] = mcp
	}
	entry := map[string]any{
		"type":    "local",
		"command": []string{exe, "mcp"},
		"enabled": true,
	}
	if len(env) > 0 {
		entry["environment"] = env
	}
	mcp["lossless"] = entry
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func WriteOpenCodeMCP(home, exe string, env map[string]string) (string, error) {
	cfg := os.Getenv("OPENCODE_CONFIG")
	if cfg == "" {
		cfg = filepath.Join(home, ".config", "opencode")
	}
	dest := filepath.Join(cfg, "opencode.json")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if _, err := os.Stat(dest + "c"); err == nil {
			dest = dest + "c"
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	merged, err := MergeOpenCodeMCP(existing, exe, env)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dest, err)
	}
	if err := writeUserConfig(dest, merged, 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

// upsertTOMLTable replaces table `name` and any `name.*` children, then appends body.
func upsertTOMLTable(existing []byte, name, body string) []byte {
	lines := strings.Split(string(existing), "\n")
	var keep []string
	skip := false
	for _, line := range lines {
		if n, ok := tomlTableName(line); ok {
			skip = n == name || strings.HasPrefix(n, name+".")
		}
		if skip {
			continue
		}
		keep = append(keep, line)
	}
	out := strings.TrimRight(strings.Join(keep, "\n"), "\n")
	body = strings.TrimSpace(body)
	var b strings.Builder
	if out != "" {
		b.WriteString(out)
		b.WriteString("\n\n")
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
