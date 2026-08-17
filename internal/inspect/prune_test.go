package inspect

import (
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
)

func TestPruneDropsTestIngestKeepsLive(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ws := "/var/folders/xx/T/TestRunHookGrok123456789/003"
	src := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(src, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "TESTJOSE", Type: "decision", ProjectKey: "path-deadbeefdeadbee",
		Text:      "We decided to use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-08-01T00:00:00Z", SessionID: "sess1", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSession(store.Session{
		JSONL: src, SessionID: "sess1", Harness: "grok",
		Workspace: ws, Project: "path-deadbeefdeadbee",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.WriteClaim(claim.Record{
		ID: "LIVEJOSE", Type: "decision", ProjectKey: "acme/api",
		Text:  "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-07-01T00:00:00Z",
		SessionID: "01a003db-f4a6-7f43-a694-082428bbff32", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "NOISE", Type: "state", ProjectKey: "acme/api",
		Text:  "This is still not a guarantee — a model can ignore a skill — but it is no longer one sentence on a tool next to grep.",
		Paths: []string{".claude/skills/lossless/SKILL.md"}, CreatedAt: "2026-08-17T04:41:01Z",
		SessionID: "01a003db-f4a6-7f43-a694-082428bbff32", Status: "active",
		Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(claim.Record{
		ID: "FIX", Type: "decision", ProjectKey: "acme/api",
		Text: "We decided to use jose from a fixture.", SessionID: "grok-auth",
		CreatedAt: "2026-08-01T00:00:00Z", Status: "active", Source: "import", Harness: "grok",
	}); err != nil {
		t.Fatal(err)
	}

	res, err := Prune(st)
	if err != nil {
		t.Fatal(err)
	}
	if res.DroppedRecords < 1 || res.SupersededNoise < 1 {
		t.Fatalf("%+v", res)
	}
	if _, ok := st.Get("TESTJOSE"); ok {
		t.Fatal("test ingest remained")
	}
	if _, ok := st.Get("FIX"); ok {
		t.Fatal("fixture claim remained")
	}
	live, ok := st.Get("LIVEJOSE")
	if !ok || live.Status != "active" {
		t.Fatal("live claim")
	}
	noise, ok := st.Get("NOISE")
	if !ok || noise.Status != "superseded" {
		t.Fatalf("noise %+v %v", noise, ok)
	}
}
