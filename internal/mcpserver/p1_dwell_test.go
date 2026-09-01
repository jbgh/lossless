package mcpserver

import (
	"encoding/json"
	"testing"

	"lossless/internal/store"
)

// get_record carries the caller's session so the dwell lands on that
// session's tape, not on whoever acted last.
func TestGetRecordDwellUsesSessionID(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(Local{Store: st})
	got := s.Handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"remember","arguments":{"type":"decision","text":"Use jose, not jsonwebtoken, for Edge.","project":"acme/api","session_id":"s8"}}}`))
	var wrap rpcResponse
	if err := json.Unmarshal(got, &wrap); err != nil || wrap.Error != nil {
		t.Fatal(string(got))
	}
	m, _ := wrap.Result.(map[string]any)
	sc, _ := m["structuredContent"].(map[string]any)
	ids, _ := sc["ids"].([]any)
	if len(ids) == 0 {
		t.Fatal(string(got))
	}
	id, _ := ids[0].(string)
	s.Handle([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_record","arguments":{"id":"` + id + `","project":"acme/api","session_id":"s9"}}}`))
	acts, err := st.RecentActions("acme/api", "s9", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range acts {
		if a.Kind == store.ActionGet && a.ClaimID == id {
			return
		}
	}
	t.Fatalf("dwell not recorded on the calling session: %+v", acts)
}
