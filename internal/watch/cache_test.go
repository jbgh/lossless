package watch

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lossless/internal/harness"
	"lossless/internal/store"
	"lossless/internal/write"
)

// countPeeks swaps every transcript peek and the catch-up for counting
// wrappers for the test's duration.
func countPeeks(t *testing.T) (peeks map[string]int, catchUps map[string]int) {
	t.Helper()
	peeks, catchUps = map[string]int{}, map[string]int{}
	pc, pk, pp, po, cu := peekClaudeCWD, peekCodexMeta, peekPiCWD, peekOmpCWD, catchUp
	peekClaudeCWD = func(p string) string { peeks[p]++; return pc(p) }
	peekCodexMeta = func(p string) (string, string) { peeks[p]++; return pk(p) }
	peekPiCWD = func(p string) string { peeks[p]++; return pp(p) }
	peekOmpCWD = func(p string) string { peeks[p]++; return po(p) }
	catchUp = func(st *store.Store, req write.CatchUpRequest) (write.CatchUpResult, error) {
		catchUps[req.JSONL]++
		return cu(st, req)
	}
	t.Cleanup(func() { peekClaudeCWD, peekCodexMeta, peekPiCWD, peekOmpCWD, catchUp = pc, pk, pp, po, cu })
	return peeks, catchUps
}

func total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// age moves a path's mtime out of the racy window, so the watcher trusts it.
func age(t *testing.T, paths ...string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	for _, p := range paths {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
}

func writeClaude(t *testing.T, path, ws, text string) {
	t.Helper()
	body := `{"type":"user","cwd":"` + ws + `","message":{"role":"user","content":"go"}}` + "\n" +
		`{"type":"assistant","content":"` + text + `"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cacheFixture(t *testing.T) (st *store.Store, opts Options, cdir, cfile string) {
	t.Helper()
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	cdir = filepath.Join(root, "claude", harness.ClaudeProjectSlug(ws))
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfile = filepath.Join(cdir, "uuid-1.jsonl")
	writeClaude(t, cfile, ws, "We decided to use jose, not jsonwebtoken, for Edge.")
	st, err := store.Open(filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, Options{ClaudeRoot: filepath.Join(root, "claude")}, cdir, cfile
}

func TestWatcherDoesNotRepeekOrRecheckAnUnchangedFile(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	peeks, catchUps := countPeeks(t)
	w := NewWatcher()
	if res, err := w.Tick(st, opts); err != nil || res.CatchUps != 1 {
		t.Fatalf("first tick: %+v %v", res, err)
	}
	if peeks[cfile] != 1 {
		t.Fatalf("first tick peeks %d", peeks[cfile])
	}
	age(t, cfile, cdir, filepath.Dir(cdir))
	for i := 0; i < 3; i++ {
		res, err := w.Tick(st, opts)
		if err != nil || res.CatchUps != 0 {
			t.Fatalf("idle tick %d: %+v %v", i, res, err)
		}
	}
	if peeks[cfile] != 1 || catchUps[cfile] != 1 {
		t.Fatalf("idle ticks re-peeked or re-caught: peeks %d catch-ups %d", peeks[cfile], catchUps[cfile])
	}
	if _, ok := w.quiet[cfile]; !ok {
		t.Fatal("caught-up file not marked quiet")
	}
}

func TestWatcherCatchesUpAGrownFileWithoutRepeek(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	peeks, _ := countPeeks(t)
	w := NewWatcher()
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	age(t, cfile, cdir, filepath.Dir(cdir))
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(cfile, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"assistant","content":"We decided to keep Postgres, not DynamoDB, for billing."}` + "\n")
	_ = f.Close()
	age(t, cfile) // a grown file is caught up whatever its mtime says
	res, err := w.Tick(st, opts)
	if err != nil || res.CatchUps != 1 {
		t.Fatalf("grown file tick: %+v %v", res, err)
	}
	fi, _ := os.Stat(cfile)
	if got := st.Cursor(cfile); got != fi.Size() {
		t.Fatalf("cursor %d, size %d", got, fi.Size())
	}
	if peeks[cfile] != 1 {
		t.Fatalf("a found cwd was peeked again: %d", peeks[cfile])
	}
}

func TestWatcherDropsADeletedFile(t *testing.T) {
	st, opts, _, cfile := cacheFixture(t)
	w := NewWatcher()
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.peeks[cfile]; !ok {
		t.Fatal("peek not cached")
	}
	if err := os.Remove(cfile); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.peeks[cfile]; ok {
		t.Fatal("deleted file still in the peek cache")
	}
	for _, tg := range w.discover(opts, nil) {
		if tg.JSONL == cfile {
			t.Fatal("deleted file still discovered")
		}
	}
}

func TestWatcherFindsANewFileInACachedDirectory(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	w := NewWatcher()
	age(t, cfile, cdir, filepath.Dir(cdir))
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if _, ok := w.dirs[cdir]; !ok {
		t.Fatal("an aged directory listing was not cached")
	}
	// A new session in a known project dir moves the dir's mtime.
	newFile := filepath.Join(cdir, "uuid-2.jsonl")
	writeClaude(t, newFile, "/Users/jay/dev/api", "We decided to ship the cron on Fridays.")
	res, err := w.Tick(st, opts)
	if err != nil || res.CatchUps != 1 {
		t.Fatalf("new file tick: %+v %v", res, err)
	}
	if st.Cursor(newFile) == 0 {
		t.Fatal("new file not caught up")
	}
	// A new project dir moves the root's mtime.
	ndir := filepath.Join(opts.ClaudeRoot, harness.ClaudeProjectSlug("/Users/jay/dev/web"))
	if err := os.MkdirAll(ndir, 0o755); err != nil {
		t.Fatal(err)
	}
	nfile := filepath.Join(ndir, "uuid-3.jsonl")
	writeClaude(t, nfile, "/Users/jay/dev/web", "We decided to drop the CDN.")
	if res, err := w.Tick(st, opts); err != nil || res.CatchUps != 1 {
		t.Fatalf("new dir tick: %+v %v", res, err)
	}
}

// The verify pass is the safety net under the mtime shortcuts: a listing
// whose directory mtime was put back (or a clock too coarse to move it)
// is read again within verifyEvery.
func TestWatcherVerifyPassRereadsAStaleListing(t *testing.T) {
	st, opts, cdir, cfile := cacheFixture(t)
	w := NewWatcher()
	age(t, cfile, cdir, filepath.Dir(cdir))
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(cdir)
	hidden := filepath.Join(cdir, "uuid-hidden.jsonl")
	writeClaude(t, hidden, "/Users/jay/dev/api", "We decided to pin Go 1.25.")
	age(t, hidden)
	if err := os.Chtimes(cdir, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if st.Cursor(hidden) != 0 {
		t.Fatal("expected the cached listing to hide the file until the verify pass")
	}
	w.lastVerify = time.Now().Add(-verifyEvery)
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if st.Cursor(hidden) == 0 {
		t.Fatal("verify pass did not find the file")
	}
}

// A file whose catch-up fails (a Pi subagent transcript has no cwd) is
// tried again when it changes, not on every tick.
func TestWatcherRetriesAFailedCatchUpOnlyWhenTheFileChanges(t *testing.T) {
	root := t.TempDir()
	pdir := filepath.Join(root, "pi", "--Users-jay-dev-api--", "subagent-artifacts")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	pfile := filepath.Join(pdir, "070b5091_worker_transcript.jsonl")
	if err := os.WriteFile(pfile, []byte(`{"type":"message","message":{"role":"assistant","content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	age(t, pfile, pdir, filepath.Dir(pdir), filepath.Dir(filepath.Dir(pdir)))
	st, err := store.Open(filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	opts := Options{PiRoot: filepath.Join(root, "pi")}
	peeks, catchUps := countPeeks(t)
	w := NewWatcher()
	for i := 0; i < 3; i++ {
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
	}
	if catchUps[pfile] != 1 || peeks[pfile] != 1 {
		t.Fatalf("catch-ups %d peeks %d over three ticks, want 1 and 1", catchUps[pfile], peeks[pfile])
	}
	f, _ := os.OpenFile(pfile, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"message","message":{"role":"assistant","content":"again"}}` + "\n")
	_ = f.Close()
	if _, err := w.Tick(st, opts); err != nil {
		t.Fatal(err)
	}
	if catchUps[pfile] != 2 || peeks[pfile] != 2 {
		t.Fatalf("changed file: catch-ups %d peeks %d, want 2 and 2", catchUps[pfile], peeks[pfile])
	}
}

// The cached walk keeps the Adv191 exclusions on every tick: Claude
// subagents/ and agent-*.jsonl dumps stay out, nested non-agent JSONL and
// Grok child chat_history.jsonl stay in.
func TestWatcherKeepsSubagentExclusionsAcrossTicks(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	cdir := filepath.Join(root, "claude", harness.ClaudeProjectSlug(ws))
	files := map[string]bool{ // path → discovered
		filepath.Join(cdir, "uuid-parent.jsonl"):                                                        true,
		filepath.Join(cdir, "agent", "nested-ok.jsonl"):                                                 true,
		filepath.Join(cdir, "uuid-parent", "subagents", "workflows", "wf_dead", "agent-deadbeef.jsonl"): false,
		filepath.Join(cdir, "uuid-parent", "subagents", "plain.jsonl"):                                  false,
		filepath.Join(cdir, "Agent-Foo.jsonl"):                                                          false,
	}
	for p := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeClaude(t, p, ws, "We decided to use jose, not jsonwebtoken, for Edge.")
	}
	gfile := filepath.Join(root, "grok", harness.EncodeCWD(ws), "child-sid", "chat_history.jsonl")
	if err := os.MkdirAll(filepath.Dir(gfile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gfile, []byte(`{"type":"assistant","content":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files[gfile] = true
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, _ error) error { age(t, p); return nil })

	opts := Options{GrokRoot: filepath.Join(root, "grok"), ClaudeRoot: filepath.Join(root, "claude")}
	w := NewWatcher()
	for tick := 0; tick < 3; tick++ {
		w.beginTick(time.Now())
		got := map[string]bool{}
		for _, tg := range w.discover(opts, nil) {
			got[tg.JSONL] = true
		}
		w.endTick()
		for p, want := range files {
			if got[p] != want {
				t.Fatalf("tick %d: %s discovered=%v, want %v", tick, p, got[p], want)
			}
		}
		if len(got) != 3 {
			t.Fatalf("tick %d: %d targets", tick, len(got))
		}
	}
}

// An OpenCode session is listed twice once ingested (the session table's
// virtual path and opencode.db), and each listing goes quiet on its own
// key; an unchanged opencode.db is not queried again.
func TestWatcherQuietsBothListingsOfAnOpenCodeSession(t *testing.T) {
	root := t.TempDir()
	ws := "/Users/jay/dev/api"
	dbPath := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE session (id TEXT, directory TEXT, time_updated INTEGER);
CREATE TABLE message (id TEXT, session_id TEXT, time_created INTEGER, data TEXT);
CREATE TABLE part (id TEXT, message_id TEXT, time_created INTEGER, data TEXT);
INSERT INTO session(id, directory, time_updated) VALUES('ses_watch', ?, 42);
INSERT INTO message(id, session_id, time_created, data) VALUES
 ('m1', 'ses_watch', 1, '{"role":"assistant","time":{"created":1,"completed":2}}');
INSERT INTO part(id, message_id, time_created, data) VALUES
 ('p1', 'm1', 1, '{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}');
`, ws)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	age(t, dbPath)
	t.Setenv("OPENCODE_DB", dbPath)
	st, err := store.Open(filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	opts := Options{OpenCodeDB: dbPath}
	_, catchUps := countPeeks(t)
	w := NewWatcher()
	for i := 0; i < 3; i++ {
		if _, err := w.Tick(st, opts); err != nil {
			t.Fatal(err)
		}
	}
	if total(catchUps) != 1 {
		t.Fatalf("catch-ups %v, want one", catchUps)
	}
	key := sqliteCursorKey("opencode", "ses_watch")
	var quiet int
	for k := range w.quiet {
		if len(k) > len(key) && k[:len(key)] == key {
			quiet++
		}
	}
	if quiet != 2 {
		t.Fatalf("quiet listings %d, want 2: %v", quiet, w.quiet)
	}
	if !w.ocOK {
		t.Fatal("an aged opencode.db listing was not cached")
	}
}
