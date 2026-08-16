package write

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestReadOpenCodeSession(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE session (id TEXT, directory TEXT);
CREATE TABLE message (id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
CREATE TABLE part (id TEXT, message_id TEXT, time_created INTEGER, data TEXT);
INSERT INTO session(id, directory) VALUES('ses_1', '/Users/jay/dev/api');
INSERT INTO message(id, session_id, time_created, data) VALUES
 ('m1', 'ses_1', 1, '{"role":"user"}'),
 ('m2', 'ses_1', 2, '{"role":"assistant"}');
INSERT INTO part(id, message_id, time_created, data) VALUES
 ('p1', 'm1', 1, '{"type":"text","text":"Always use jose please."}'),
 ('p2', 'm2', 2, '{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}'),
 ('p3', 'm2', 3, '{"type":"reasoning","text":"think"}');
`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	cwd, msgs, err := ReadOpenCodeSession(dbPath, "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if cwd != "/Users/jay/dev/api" || len(msgs) != 2 {
		t.Fatalf("%q %d", cwd, len(msgs))
	}

	st := tmpStore(t)
	t.Setenv("OPENCODE_DB", dbPath)
	res, err := CatchUp(st, CatchUpRequest{
		Harness: "opencode", SessionID: "ses_1", Project: "acme/api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extracted == 0 {
		t.Fatal("expected claims from dump")
	}
	raw, _ := os.ReadFile(res.RawPath)
	if !strings.Contains(string(raw), "jose") {
		t.Fatal(string(raw))
	}
	if _, _, err := ReadOpenCodeSession(dbPath, "missing"); err == nil {
		t.Fatal("missing")
	}

	again, err := CatchUp(st, CatchUpRequest{
		Harness: "opencode", SessionID: "ses_1", Project: "acme/api",
	})
	if err != nil || !again.Noop {
		t.Fatalf("opencode dump must be byte-stable: %+v %v", again, err)
	}
}
