package inspect

import (
	"path/filepath"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

// Eight live sessions were stored as harness=claude pointing at Grok's
// updates.jsonl. Prune drops those rows (session, cursor, their claims)
// and leaves the Grok chat_history session for the same id alone.
func TestPruneDropsGrokUpdatesSessions(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	base := filepath.Join("/Users/jay/.grok/sessions/%2FUsers%2Fjay%2Fdev%2Fapi", "01a0")
	upd := filepath.Join(base, "updates.jsonl")
	hist := filepath.Join(base, "chat_history.jsonl")
	for _, s := range []store.Session{
		{JSONL: upd, SessionID: "01a0", Harness: "claude", Workspace: "/Users/jay/dev/api", Project: "acme/api"},
		{JSONL: hist, SessionID: "01a0", Harness: "grok", Workspace: "/Users/jay/dev/api", Project: "acme/api"},
	} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetCursor(upd, 10); err != nil {
		t.Fatal(err)
	}
	for _, r := range []claim.Record{
		{ID: "FROMUPDATES", Type: "failed", ProjectKey: "acme/api", Text: "Redis token bucket failed in src/auth.ts staging.",
			CreatedAt: "2026-09-01T00:00:00Z", SessionID: "01a0", Status: "active", Source: "turn", Harness: "claude"},
		{ID: "FROMHISTORY", Type: "decision", ProjectKey: "acme/api", Text: "We decided to use jose, not jsonwebtoken, for Edge.",
			CreatedAt: "2026-09-01T00:00:00Z", SessionID: "01a0", Status: "active", Source: "turn", Harness: "grok"},
	} {
		if _, err := st.WriteClaim(r); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Prune(st, "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if res.DroppedSessions != 1 || res.DroppedRecords != 1 {
		t.Fatalf("%+v", res)
	}
	if _, ok := st.SessionByJSONL(upd); ok {
		t.Fatal("updates.jsonl session kept")
	}
	if _, ok := st.SessionByJSONL(hist); !ok {
		t.Fatal("chat_history session dropped")
	}
	if st.Cursor(upd) != 0 {
		t.Fatal("updates.jsonl cursor kept")
	}
	recs, _ := st.ListActive("acme/api")
	if len(recs) != 1 || recs[0].ID != "FROMHISTORY" {
		t.Fatalf("records %+v", recs)
	}
}
