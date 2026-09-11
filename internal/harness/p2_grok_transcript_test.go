package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// Grok 1.0.13 runs Claude-scope hooks and hands hook-claude its own
// updates.jsonl (a JSON-RPC event stream, never a tape). The Claude
// locate must hand that session back to the Grok adapter: the sibling
// chat_history.jsonl, harness grok.
func TestLocateClaudeRoutesGrokTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(root, ".grok"))
	ws := "/Users/jay/dev/api"
	sid := "01a0557f-15b5-70a0-971c-efb01960423d"
	dir := filepath.Join(root, ".grok", "sessions", EncodeCWD(ws), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(dir, "chat_history.jsonl")
	upd := filepath.Join(dir, "updates.jsonl")
	for _, p := range []string{hist, upd} {
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loc := LocateClaude(upd, sid, ws)
	if loc.JSONL != hist || loc.Harness != "grok" || loc.SessionID != sid {
		t.Fatalf("grok updates.jsonl not rerouted: %+v", loc)
	}
	claude := LocateClaude("/Users/jay/.claude/projects/-Users-jay-dev-api/abc.jsonl", "abc", ws)
	if claude.Harness != "" || claude.JSONL != "/Users/jay/.claude/projects/-Users-jay-dev-api/abc.jsonl" {
		t.Fatalf("claude transcript changed: %+v", claude)
	}
}
