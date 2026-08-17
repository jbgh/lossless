package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeProjectSlug(t *testing.T) {
	if ClaudeProjectSlug("/Users/jaybyoun/developer") != "-Users-jaybyoun-developer" {
		t.Fatal(ClaudeProjectSlug("/Users/jaybyoun/developer"))
	}
}

func TestLocateClaudeTranscriptWins(t *testing.T) {
	loc := LocateClaude("/tmp/sess.jsonl", "", "/ws")
	if loc.JSONL != "/tmp/sess.jsonl" || loc.SessionID != "sess" || loc.CWD != "/ws" {
		t.Fatalf("%+v", loc)
	}
}

func TestLocateClaudeFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_HOME", root)
	ws := "/Users/jay/dev/api"
	sid := "abc-123"
	dir := filepath.Join(root, "projects", ClaudeProjectSlug(ws))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, sid+".jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc := LocateClaude("", sid, ws)
	if loc.JSONL != p {
		t.Fatalf("got %q want %q", loc.JSONL, p)
	}
	empty := LocateClaude("", "", "")
	if empty.JSONL != "" {
		t.Fatal(empty)
	}
}

func TestDecodeGrokSessionDir(t *testing.T) {
	if DecodeGrokSessionDir("%2FUsers%2Fjay%2Fdev") != "/Users/jay/dev" {
		t.Fatal(DecodeGrokSessionDir("%2FUsers%2Fjay%2Fdev"))
	}
	if DecodeGrokSessionDir("plain") != "plain" {
		t.Fatal("plain")
	}
	if DecodeGrokSessionDir("%2fUsers%ZZ") != "/Users%ZZ" && DecodeGrokSessionDir("%2fUsers%ZZ") == "" {
		t.Fatal("bad hex")
	}
}

func TestMergeClaudeSettingsPreservesExisting(t *testing.T) {
	existing := []byte(`{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`)
	got, err := MergeClaudeSettings(existing, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatal(err)
	}
	if root["model"] != "opus" {
		t.Fatal(root["model"])
	}
	s := string(got)
	if !strings.Contains(s, "hook-claude") || !strings.Contains(s, "echo hi") {
		t.Fatal(s)
	}
	again, err := MergeClaudeSettings(got, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(again), "hook-claude") != strings.Count(s, "hook-claude") {
		t.Fatal("duplicate install")
	}
}

func TestMergeClaudeSettingsEmptyAndInvalid(t *testing.T) {
	got, err := MergeClaudeSettings(nil, "/bin/am")
	if err != nil || !strings.Contains(string(got), "PreCompact") {
		t.Fatal(string(got), err)
	}
	if _, err := MergeClaudeSettings([]byte("{"), "/bin/am"); err == nil {
		t.Fatal("invalid")
	}
}

func TestMergeMCPConfigs(t *testing.T) {
	got, err := MergeClaudeMCP([]byte(`{"model":"opus"}`), "/bin/am", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"lossless"`) || !strings.Contains(string(got), `"mcp"`) {
		t.Fatal(string(got))
	}
	if !strings.Contains(string(got), `"model"`) {
		t.Fatal("lost model")
	}
	again, err := MergeClaudeMCP(got, "/bin/am", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(again), `"command"`) != 1 {
		t.Fatal("duplicate mcp")
	}
	if _, err := MergeClaudeMCP([]byte("{"), "/bin/am", nil); err == nil {
		t.Fatal("invalid")
	}

	toml := MergeGrokMCP(nil, "http://127.0.0.1:7432/mcp", false)
	if !strings.Contains(string(toml), "[mcp_servers.lossless]") {
		t.Fatal(string(toml))
	}
	againT := MergeGrokMCP(toml, "http://127.0.0.1:7432/mcp", false)
	if strings.Count(string(againT), "[mcp_servers.lossless]") != 1 {
		t.Fatal(string(againT))
	}
	merged := MergeGrokMCP([]byte("model = \"x\""), "http://127.0.0.1:7432/mcp", false)
	if !strings.Contains(string(merged), "model") || !strings.Contains(string(merged), "lossless") {
		t.Fatal(string(merged))
	}
}

func TestWriteMCP(t *testing.T) {
	home := t.TempDir()
	g, err := WriteGrokMCP(home, "http://127.0.0.1:7432/mcp", false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(g)
	if !strings.Contains(string(b), "url") {
		t.Fatal(string(b))
	}
	c, err := WriteClaudeMCP(home, "/bin/am", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(c)
	if !strings.Contains(string(b), "hook") && !strings.Contains(string(b), `"mcp"`) {
		t.Fatal(string(b))
	}
}

func TestMergeCodexFeatures(t *testing.T) {
	got := MergeCodexFeatures(nil)
	if !strings.Contains(string(got), "hooks = true") {
		t.Fatal(string(got))
	}
	again := MergeCodexFeatures(got)
	if string(again) != string(got) {
		t.Fatal("idempotent")
	}
	keep := MergeCodexFeatures([]byte("model = \"x\"\n"))
	if !strings.Contains(string(keep), "model") || !strings.Contains(string(keep), "[features]") {
		t.Fatal(string(keep))
	}
}

func TestWriteHooks(t *testing.T) {
	home := t.TempDir()
	g, err := WriteGrokHooks(home, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(g)
	if !strings.Contains(string(b), "hook-grok") {
		t.Fatal(string(b))
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"model":"x"}`), 0o644); err != nil {
		// dir may not exist
		_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
		if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"model":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := WriteClaudeHooks(home, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(c)
	if !strings.Contains(string(b), `"model": "x"`) && !strings.Contains(string(b), `"model":"x"`) {
		t.Fatal(string(b))
	}
	if !strings.Contains(string(b), "hook-claude") {
		t.Fatal(string(b))
	}
}
