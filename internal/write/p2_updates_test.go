package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updates.jsonl is Grok's event stream, not a session tape. No adapter
// may ingest it, whatever harness the caller claims.
func TestCatchUpRefusesGrokUpdatesStream(t *testing.T) {
	st := tmpStore(t)
	dir := filepath.Join(t.TempDir(), ".grok", "sessions", "%2FUsers%2Fjay%2Fdev%2Fapi", "01a0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := writeJSONL(t, dir, "updates.jsonl",
		`{"timestamp":1,"method":"session/update","params":{"sessionId":"01a0","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Redis token bucket failed in src/auth.ts staging."}}}}`+"\n")
	_, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", Harness: "claude", SessionID: "01a0"})
	if err == nil || !strings.Contains(err.Error(), "updates.jsonl") {
		t.Fatalf("expected refusal naming updates.jsonl, got %v", err)
	}
	if _, ok := st.SessionByJSONL(src); ok {
		t.Fatal("session row written for an event stream")
	}
}
