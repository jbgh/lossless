package harness

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestCodexSessionIDFromPath(t *testing.T) {
	p := "/x/sessions/2026/08/14/rollout-2026-08-14T20-38-23-019ef634-9af9-72d2-b01c-97d349693335.jsonl"
	got := CodexSessionIDFromPath(p)
	if got != "019ef634-9af9-72d2-b01c-97d349693335" {
		t.Fatal(got)
	}
	if CodexSessionIDFromPath("rollout-foo.jsonl") != "foo" {
		t.Fatal(CodexSessionIDFromPath("rollout-foo.jsonl"))
	}
}

func TestLocateCodexTranscriptWins(t *testing.T) {
	loc := LocateCodex("/tmp/rollout-2026-01-01T00-00-00-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl", "", "/ws")
	if loc.SessionID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" || loc.CWD != "/ws" {
		t.Fatalf("%+v", loc)
	}
}

func TestLocateCodexBySessionAndCWD(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	t.Setenv("HOME", root)
	sid := "019ef634-9af9-72d2-b01c-97d349693335"
	dir := filepath.Join(root, "sessions", "2026", "08", "14")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Clean("/Users/jay/dev/api")
	p := filepath.Join(dir, "rollout-2026-08-14T12-00-00-"+sid+".jsonl")
	body := `{"type":"session_meta","payload":{"id":"` + sid + `","cwd":"` + ws + `"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"user_message","message":"hi"}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// older other cwd
	other := filepath.Join(dir, "rollout-2026-08-14T11-00-00-11111111-1111-1111-1111-111111111111.jsonl")
	if err := os.WriteFile(other, []byte(`{"type":"session_meta","payload":{"id":"11111111-1111-1111-1111-111111111111","cwd":"/other"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bySID := LocateCodex("", sid, "")
	if bySID.JSONL != p {
		t.Fatalf("by sid: %q", bySID.JSONL)
	}
	byCWD := LocateCodex("", "", ws)
	if byCWD.JSONL != p || byCWD.SessionID != sid {
		t.Fatalf("by cwd: %+v", byCWD)
	}
	id, cwd := PeekCodexMeta(p)
	if id != sid || cwd != ws {
		t.Fatalf("peek %q %q", id, cwd)
	}
	empty := LocateCodex("", "", "")
	if empty.JSONL != "" {
		t.Fatal(empty)
	}
	if _, cwd := PeekCodexMeta("/no/such"); cwd != "" {
		t.Fatal("missing peek")
	}
	_ = time.Now()
}

func TestPeekCodexLegacyItem(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rollout-x.jsonl")
	if err := os.WriteFile(p, []byte(`{"item":{"type":"SessionMeta","id":"abc","cwd":"/ws"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, cwd := PeekCodexMeta(p)
	if id != "abc" || cwd != "/ws" {
		t.Fatalf("%q %q", id, cwd)
	}
}

func TestCodexStateThreadsEmptyRollout(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "state_5.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE threads (
  id TEXT PRIMARY KEY,
  rollout_path TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  source TEXT NOT NULL,
  model_provider TEXT NOT NULL,
  cwd TEXT NOT NULL,
  title TEXT NOT NULL,
  sandbox_policy TEXT NOT NULL,
  approval_mode TEXT NOT NULL,
  first_user_message TEXT NOT NULL DEFAULT ''
);
INSERT INTO threads(id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode, first_user_message)
VALUES('thread-empty', '', 1, 9, 'desktop', 'openai', '/Users/jay/dev/api', 't', 'danger', 'on', 'Use jose, not jsonwebtoken.');
INSERT INTO threads(id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode, first_user_message)
VALUES('thread-missing-file', '/no/such/rollout.jsonl', 1, 8, 'desktop', 'openai', '/Users/jay/dev/api', 't', 'danger', 'on', 'Never log Authorization headers.');
`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	got := CodexStateThreads(root)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	byID := map[string]CodexThread{}
	for _, th := range got {
		byID[th.ID] = th
	}
	if byID["thread-empty"].CWD != "/Users/jay/dev/api" || byID["thread-empty"].Rollout != "" || byID["thread-empty"].FirstUser == "" {
		t.Fatalf("%+v", byID["thread-empty"])
	}
	if byID["thread-missing-file"].Rollout != "" {
		t.Fatal("missing file must not count as a rollout")
	}
	if len(CodexStateRollouts(root)) != 0 {
		t.Fatal("no live rollout files")
	}
}

func TestMergeCodexHooks(t *testing.T) {
	got, err := MergeCodexHooks([]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`), "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "hook-codex") || !strings.Contains(string(got), "echo hi") {
		t.Fatal(string(got))
	}
	again, err := MergeCodexHooks(got, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(again), "hook-codex") != strings.Count(string(got), "hook-codex") {
		t.Fatal("duplicate")
	}
	home := t.TempDir()
	dest, err := WriteCodexHooks(home, "/bin/am")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dest)
	if !strings.Contains(string(b), "PreCompact") || !strings.Contains(string(b), "PostCompact") {
		t.Fatal(string(b))
	}
}
