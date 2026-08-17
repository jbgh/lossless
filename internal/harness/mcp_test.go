package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonBaseAndMCPEndpoint(t *testing.T) {
	if DaemonBase("") != defaultDaemon {
		t.Fatal(DaemonBase(""))
	}
	if MCPEndpoint("https://home.example") != "https://home.example/mcp" {
		t.Fatal(MCPEndpoint("https://home.example"))
	}
	if MCPEndpoint("https://home.example/mcp") != "https://home.example/mcp" {
		t.Fatal("strip /mcp once")
	}
}

func TestMergeGrokMCPAuthAndUpsert(t *testing.T) {
	first := MergeGrokMCP([]byte("model = \"x\"\n"), "http://127.0.0.1:7432/mcp", false)
	if !strings.Contains(string(first), "model") || strings.Contains(string(first), "Authorization") {
		t.Fatal(string(first))
	}
	second := MergeGrokMCP(first, "https://home.example/mcp", true)
	if strings.Count(string(second), "[mcp_servers.lossless]") != 1 {
		t.Fatal(string(second))
	}
	if !strings.Contains(string(second), "https://home.example/mcp") {
		t.Fatal(string(second))
	}
	if !strings.Contains(string(second), `${LOSSLESS_TOKEN}`) {
		t.Fatal("must interpolate, not write the secret")
	}
	if strings.Contains(string(second), "sekrit") {
		t.Fatal("raw token leaked")
	}
}

func TestMergeCodexAndOpenCodeAndPiMCP(t *testing.T) {
	env := map[string]string{"LOSSLESS_URL": "https://home.example"}
	cx := MergeCodexMCP([]byte("[features]\nhooks = true\n"), "/bin/am", env)
	if !strings.Contains(string(cx), "[features]") || !strings.Contains(string(cx), `command = "/bin/am"`) {
		t.Fatal(string(cx))
	}
	if !strings.Contains(string(cx), "https://home.example") {
		t.Fatal(string(cx))
	}

	oc, err := MergeOpenCodeMCP([]byte(`{"model":"x"}`), "/bin/am", env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(oc), `"type": "local"`) || !strings.Contains(string(oc), `"model"`) {
		t.Fatal(string(oc))
	}
	if !strings.Contains(string(oc), "environment") {
		t.Fatal(string(oc))
	}

	pi, err := MergeJSONMCPServers(nil, "/bin/am", env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pi), `"mcpServers"`) {
		t.Fatal(string(pi))
	}
}

func TestInstallMCPAllHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(home, ".config", "opencode"))
	paths, err := InstallMCP(MCPConfig{
		Home: home, Exe: "/bin/am", URL: "https://home.example", Token: "sekrit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 5 {
		t.Fatal(paths)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(p, err)
		}
		if !strings.Contains(string(b), "lossless") {
			t.Fatalf("%s: %s", p, b)
		}
		if strings.Contains(string(b), "sekrit") {
			t.Fatalf("token written to %s", p)
		}
	}
	g, _ := os.ReadFile(filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(string(g), `${LOSSLESS_TOKEN}`) {
		t.Fatal(string(g))
	}
}

func TestStdioEnvOnlyWhenNonLoopback(t *testing.T) {
	if stdioEnv(MCPConfig{URL: ""}) != nil {
		t.Fatal("loopback")
	}
	got := stdioEnv(MCPConfig{URL: "https://home.example/mcp"})
	if got["LOSSLESS_URL"] != "https://home.example" {
		t.Fatal(got)
	}
}

func TestCheckDaemonURL(t *testing.T) {
	if err := CheckDaemonURL(""); err != nil {
		t.Fatal(err)
	}
	if err := CheckDaemonURL("http://127.0.0.1:7432"); err != nil {
		t.Fatal(err)
	}
	if err := CheckDaemonURL("https://home.example/mcp"); err != nil {
		t.Fatal(err)
	}
	if err := CheckDaemonURL("http://home.example"); err == nil {
		t.Fatal("cleartext remote")
	}
	if err := CheckDaemonURL("ftp://home.example"); err == nil {
		t.Fatal("ftp")
	}
}

func TestInstallHooksRequiresHome(t *testing.T) {
	if _, err := InstallHooks("", "/bin/am"); err == nil {
		t.Fatal("empty home")
	}
}

func TestUpsertTOMLKeepsNeighbors(t *testing.T) {
	src := []byte("[other]\nx = 1\n\n[mcp_servers.lossless]\nurl = \"old\"\n\n[mcp_servers.lossless.headers]\nAuthorization = \"x\"\n\n[keep]\ny = 2\n")
	got := upsertTOMLTable(src, "mcp_servers.lossless", "[mcp_servers.lossless]\nurl = \"new\"\n")
	s := string(got)
	if !strings.Contains(s, "[other]") || !strings.Contains(s, "[keep]") {
		t.Fatal(s)
	}
	if strings.Contains(s, "old") || strings.Contains(s, "[mcp_servers.lossless.headers]") {
		t.Fatal(s)
	}
	if strings.Count(s, "[mcp_servers.lossless]") != 1 {
		t.Fatal(s)
	}
	commented := upsertTOMLTable([]byte("[mcp_servers.lossless] # old\nurl = \"old\"\n"), "mcp_servers.lossless", "[mcp_servers.lossless]\nurl = \"new\"\n")
	if strings.Contains(string(commented), "old") || strings.Count(string(commented), "[mcp_servers.lossless]") != 1 {
		t.Fatal(string(commented))
	}
}
