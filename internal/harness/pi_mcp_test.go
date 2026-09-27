package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePiAdapter(t *testing.T, home, version string) {
	t.Helper()
	dir := filepath.Join(home, ".pi", "agent", "npm", "node_modules", "pi-mcp-adapter")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"pi-mcp-adapter","version":"`+version+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readServers(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	s, _ := root["mcpServers"].(map[string]any)
	return s
}

// pi-mcp-adapter 3.0 reads mcp-adapter.json and ignores mcp.json, which
// now belongs to Pi's built-in MCP. Setup writes the adapter's file and
// takes its own entry out of the old one, keeping other servers.
func TestWritePiMCPAdapter3(t *testing.T) {
	home := t.TempDir()
	writePiAdapter(t, home, "3.0.0")
	legacy := filepath.Join(home, ".pi", "agent", "mcp.json")
	if err := os.WriteFile(legacy, []byte(`{"mcpServers":{"lossless":{"command":"/old"},"other":{"command":"/x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dest, err := WritePiMCP(home, "/bin/am", nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != "mcp-adapter.json" {
		t.Fatalf("wrote %s, want mcp-adapter.json", dest)
	}
	if _, ok := readServers(t, dest)["lossless"]; !ok {
		t.Fatal("lossless missing from mcp-adapter.json")
	}
	left := readServers(t, legacy)
	if _, ok := left["lossless"]; ok {
		t.Fatal("lossless left in mcp.json: Pi's built-in MCP would start it twice")
	}
	if _, ok := left["other"]; !ok {
		t.Fatal("another server in mcp.json was dropped")
	}
	if got := piMCPDoctorPath(home); got != dest {
		t.Fatalf("doctor checks %s, want %s", got, dest)
	}
}

// A mcp.json that only held lossless's own entry is removed.
func TestWritePiMCPRemovesLosslessOnlyLegacyFile(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".pi", "agent", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"mcpServers":{"lossless":{"command":"/old","args":["mcp"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePiMCP(home, "/bin/am", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("lossless-only mcp.json kept: %v", err)
	}
}

// An adapter older than 3.0 still reads mcp.json, so setup writes there.
func TestWritePiMCPLegacyAdapter(t *testing.T) {
	home := t.TempDir()
	writePiAdapter(t, home, "2.38.0")
	dest, err := WritePiMCP(home, "/bin/am", nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != "mcp.json" {
		t.Fatalf("wrote %s for a 2.x adapter, want mcp.json", dest)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi", "agent", "mcp-adapter.json")); !os.IsNotExist(err) {
		t.Fatal("wrote mcp-adapter.json for a 2.x adapter")
	}
}

// Doctor accepts lossless in any user-level file the adapter reads, and
// does not count the mcp.json a 3.x adapter ignores.
func TestPiMCPDoctorPath(t *testing.T) {
	home := t.TempDir()
	writePiAdapter(t, home, "3.0.0")
	legacy := filepath.Join(home, ".pi", "agent", "mcp.json")
	if err := os.WriteFile(legacy, []byte(`{"mcpServers":{"lossless":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := piMCPDoctorPath(home); got == legacy {
		t.Fatal("doctor counted the mcp.json a 3.x adapter ignores")
	}
	shared := filepath.Join(home, ".config", "mcp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, []byte(`{"mcpServers":{"lossless":{"command":"lossless"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := piMCPDoctorPath(home); got != shared {
		t.Fatalf("doctor checks %s, want the shared %s", got, shared)
	}
	ok, detail := checkFiles(map[string]string{"pi": piMCPDoctorPath(home)}, "lossless")
	if !ok || !strings.Contains(detail, "pi") {
		t.Fatalf("pi not ok: %s", detail)
	}
}
