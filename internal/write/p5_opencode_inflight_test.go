package write

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// openCodeDB is a minimal opencode.db the tests mutate between catch-ups,
// the way a live session mutates it between watcher ticks.
func openCodeDB(t *testing.T) (string, func(string)) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`
CREATE TABLE session (id TEXT, directory TEXT);
CREATE TABLE message (id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
CREATE TABLE part (id TEXT, message_id TEXT, time_created INTEGER, data TEXT);
INSERT INTO session(id, directory) VALUES('ses_1', '/Users/jay/dev/api');`)
	t.Setenv("OPENCODE_DB", dbPath)
	return dbPath, exec
}

func tapeLines(t *testing.T, rawPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func mustAllParse(t *testing.T, lines []string) {
	t.Helper()
	for _, l := range lines {
		var o map[string]any
		if err := json.Unmarshal([]byte(l), &o); err != nil {
			t.Fatalf("tape line is not JSON (front-truncated?): %q", l)
		}
	}
}

// The watcher polls opencode.db mid-turn. An assistant message that is still
// streaming must not reach the tape: its line would grow under the byte
// cursor and the next copy would start in the middle of it.
func TestOpenCodeInFlightMessageDoesNotCorruptTape(t *testing.T) {
	_, exec := openCodeDB(t)
	exec(`
INSERT INTO message(id, session_id, time_created, data) VALUES
 ('m1', 'ses_1', 1, '{"role":"user","time":{"created":1}}'),
 ('m2', 'ses_1', 2, '{"role":"assistant","time":{"created":2}}');
INSERT INTO part(id, message_id, time_created, data) VALUES
 ('p1', 'm1', 1, '{"type":"text","text":"Always use jose please."}'),
 ('p2', 'm2', 2, '{"type":"text","text":"We decided"}');`)
	st := tmpStore(t)
	req := CatchUpRequest{Harness: "opencode", SessionID: "ses_1", Project: "acme/api"}
	first, err := CatchUp(st, req)
	if err != nil {
		t.Fatal(err)
	}
	if lines := tapeLines(t, first.RawPath); len(lines) != 1 || !strings.Contains(lines[0], "Always use jose") {
		t.Fatalf("only the settled user turn belongs on the tape: %q", lines)
	}

	exec(`
UPDATE part SET data = '{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}' WHERE id = 'p2';
UPDATE message SET data = '{"role":"assistant","time":{"created":2,"completed":3},"finish":"stop"}' WHERE id = 'm2';`)
	second, err := CatchUp(st, req)
	if err != nil {
		t.Fatal(err)
	}
	lines := tapeLines(t, second.RawPath)
	mustAllParse(t, lines)
	if len(lines) != 2 || !strings.Contains(lines[1], "We decided to use jose, not jsonwebtoken, for Edge.") {
		t.Fatalf("finished reply must land whole, once: %q", lines)
	}
	if second.Extracted == 0 {
		t.Fatal("finished reply must reach extract")
	}
}

// A tool-only step has no prose. It must not leave a content:null line, and
// its output is tape, not claim prose: a file the model read that says
// "failed" is not a failed.
func TestOpenCodeToolStepIsTapeNotProse(t *testing.T) {
	_, exec := openCodeDB(t)
	exec(`
INSERT INTO message(id, session_id, time_created, data) VALUES
 ('m1', 'ses_1', 1, '{"role":"user","time":{"created":1}}'),
 ('m2', 'ses_1', 2, '{"role":"assistant","time":{"created":2,"completed":3},"finish":"tool-calls"}'),
 ('m3', 'ses_1', 4, '{"role":"assistant","time":{"created":4,"completed":5},"finish":"tool-calls"}');
INSERT INTO part(id, message_id, time_created, data) VALUES
 ('p1', 'm1', 1, '{"type":"text","text":"have a look at the limiter."}'),
 ('p2', 'm2', 2, '{"type":"step-start"}'),
 ('p3', 'm2', 3, '{"type":"tool","tool":"read","callID":"c1","state":{"status":"completed","input":{"filePath":"limiter.go"},"output":"The redis limiter failed under load in internal/limiter.go."}}'),
 ('p4', 'm3', 4, '{"type":"step-start"}'),
 ('p5', 'm3', 5, '{"type":"step-finish"}');`)
	st := tmpStore(t)
	res, err := CatchUp(st, CatchUpRequest{Harness: "opencode", SessionID: "ses_1", Project: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	lines := tapeLines(t, res.RawPath)
	mustAllParse(t, lines)
	tape := strings.Join(lines, "\n")
	if strings.Contains(tape, `"content":null`) {
		t.Fatalf("empty step must not leave a null line: %q", lines)
	}
	if len(lines) != 2 {
		t.Fatalf("want user turn + tool step, got %q", lines)
	}
	if !strings.Contains(lines[1], `"tool_result"`) || !strings.Contains(lines[1], "redis limiter failed under load") {
		t.Fatalf("tool output belongs on the tape as tool_result: %q", lines[1])
	}
	for _, id := range res.IDs {
		if rec, ok := st.Get(id); ok && strings.Contains(rec.Text, "redis limiter") {
			t.Fatalf("tool output became a claim: %+v", rec)
		}
	}
}

// A step that died without time.completed never changes again once a later
// assistant step exists. It must not hold the rest of the session back.
func TestOpenCodeAbandonedStepDoesNotBlockSession(t *testing.T) {
	_, exec := openCodeDB(t)
	exec(`
INSERT INTO message(id, session_id, time_created, data) VALUES
 ('m1', 'ses_1', 1, '{"role":"user","time":{"created":1}}'),
 ('m2', 'ses_1', 2, '{"role":"assistant","time":{"created":2}}'),
 ('m3', 'ses_1', 3, '{"role":"user","time":{"created":3}}'),
 ('m4', 'ses_1', 4, '{"role":"assistant","time":{"created":4,"completed":5},"finish":"stop"}');
INSERT INTO part(id, message_id, time_created, data) VALUES
 ('p1', 'm1', 1, '{"type":"text","text":"Always use jose please."}'),
 ('p2', 'm2', 2, '{"type":"text","text":"Looking at"}'),
 ('p3', 'm3', 3, '{"type":"text","text":"you crashed, go on."}'),
 ('p4', 'm4', 4, '{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}');`)
	st := tmpStore(t)
	res, err := CatchUp(st, CatchUpRequest{Harness: "opencode", SessionID: "ses_1", Project: "acme/api"})
	if err != nil {
		t.Fatal(err)
	}
	lines := tapeLines(t, res.RawPath)
	mustAllParse(t, lines)
	if len(lines) != 4 || !strings.Contains(lines[3], "We decided to use jose") {
		t.Fatalf("abandoned step must not block later turns: %q", lines)
	}
}

// A source rewritten in place without shrinking leaves the cursor inside a
// line. Copying from there writes a front-truncated line; reset like a shrink.
func TestCatchUpResetsWhenCursorIsOffALineBoundary(t *testing.T) {
	st := tmpStore(t)
	p := filepath.Join(t.TempDir(), "chat.jsonl")
	old := `{"type":"user","content":"Always use jose."}` + "\n" +
		`{"type":"assistant","content":null}` + "\n"
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "s-rewrite"}); err != nil {
		t.Fatal(err)
	}
	rewritten := `{"type":"user","content":"Always use jose."}` + "\n" +
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}` + "\n"
	if err := os.WriteFile(p, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "s-rewrite"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Sealed == "" {
		t.Fatalf("rewrite must seal the old tape like a shrink: %+v", second)
	}
	lines := tapeLines(t, second.RawPath)
	mustAllParse(t, lines)
	if len(lines) != 2 || !strings.Contains(lines[1], "We decided to use jose") {
		t.Fatalf("rewrite must recopy whole lines: %q", lines)
	}
	if st.Cursor(p) != int64(len(rewritten)) {
		t.Fatalf("cursor %d want %d", st.Cursor(p), len(rewritten))
	}
}
