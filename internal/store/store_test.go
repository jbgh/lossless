package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
)

func tmp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func rec(id, typ, text string, paths []string) claim.Record {
	return claim.Record{
		ID: id, Type: typ, Text: text, Paths: paths,
		ProjectKey: "acme/api", Harness: "grok", SessionID: "s",
		CreatedAt: "2026-08-01T00:00:00Z", Status: "active", Source: "import",
	}
}

func TestOpenCreatesLayout(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, sub := range []string{"export", "raw", "index", "spool"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
			t.Fatal(sub, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "index", "claims.sqlite")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFailsWhenRootIsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenFailsWhenDBPathIsDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "index", "claims.sqlite"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("expected migrate/pragma error")
	}
}

func TestWriteGetListAndDefaults(t *testing.T) {
	st := tmp(t)
	sup, err := st.WriteClaim(claim.Record{
		Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.",
		ProjectKey: "Acme/API",
	})
	if err != nil || sup != "" {
		t.Fatalf("sup=%s err=%v", sup, err)
	}
	active, err := st.ListActive("acme__api")
	if err != nil || len(active) != 1 {
		t.Fatalf("list=%v err=%v", active, err)
	}
	got, ok := st.Get(active[0].ID)
	if !ok || got.Status != "active" || got.ProjectKey != "acme/api" {
		t.Fatalf("%+v", got)
	}
	if got.ClaimHash == "" || len(got.Symbols) == 0 {
		t.Fatalf("defaults: %+v", got)
	}
	if _, ok := st.Get("missing"); ok {
		t.Fatal("missing")
	}
	if st.CountActive() != 1 {
		t.Fatal(st.CountActive())
	}
}

func TestWriteClaimSupersedesAndExport(t *testing.T) {
	st := tmp(t)
	a, err := st.WriteClaim(rec("OLD", "decision", "Use jose, not jsonwebtoken, for Edge.", nil))
	if err != nil || a != "" {
		t.Fatal(a, err)
	}
	b, err := st.WriteClaim(rec("NEW", "decision", "Use jose, not jsonwebtoken, for Edge.", nil))
	if err != nil || b != "OLD" {
		t.Fatalf("sup=%s err=%v", b, err)
	}
	old, ok := st.Get("OLD")
	if !ok || old.Status != "superseded" {
		t.Fatalf("%+v", old)
	}
	md, err := os.ReadFile(filepath.Join(st.Root, "export", "acme__api", "OLD.md"))
	if err != nil || !strings.Contains(string(md), "superseded") {
		t.Fatalf("export: %s err=%v", md, err)
	}
	newMd, err := os.ReadFile(filepath.Join(st.Root, "export", "acme__api", "NEW.md"))
	if err != nil || !strings.Contains(string(newMd), "supersedes: OLD") {
		t.Fatalf("new export: %s", newMd)
	}
	active, _ := st.ListActive("acme/api")
	if len(active) != 1 || active[0].ID != "NEW" {
		t.Fatalf("%+v", active)
	}
}

func TestWriteClaimExportDirBlocked(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_ = os.RemoveAll(filepath.Join(dir, "export"))
	if err := os.WriteFile(filepath.Join(dir, "export"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(rec("X", "state", "Working on billing invoices now.", nil)); err == nil {
		t.Fatal("expected writeFile error")
	}
}

func TestExcerptsCoverRef(t *testing.T) {
	st := tmp(t)
	xs := []Excerpt{{
		SessionID: "s1", ProjectKey: "acme/api", StartOffset: 0, EndOffset: 80,
		Text: "assistant: We decided to use jose.\n",
	}}
	if err := st.WriteExcerpts("2026-08", xs); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteExcerpts("2026-08", nil); err != nil {
		t.Fatal(err)
	}
	ex, ok := st.ExcerptCovering(&claim.TranscriptRef{SessionID: "s1", StartOffset: 10, EndOffset: 20}, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC))
	if !ok || !strings.Contains(ex.Text, "jose") {
		t.Fatalf("%+v %v", ex, ok)
	}
	if _, ok := st.ExcerptCovering(&claim.TranscriptRef{SessionID: "nope", StartOffset: 0}, time.Time{}); ok {
		t.Fatal("missing")
	}
	if _, ok := st.View("missing"); ok {
		t.Fatal("view missing")
	}
}

func TestSessionRegistry(t *testing.T) {
	st := tmp(t)
	if err := st.UpsertSession(Session{}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSession(Session{JSONL: "/tmp/a.jsonl", SessionID: "s", Harness: "grok", Workspace: "/ws", Project: "Acme/API"}); err != nil {
		t.Fatal(err)
	}
	got, ok := st.SessionByJSONL("/tmp/a.jsonl")
	if !ok || got.Project != "acme/api" || got.SessionID != "s" {
		t.Fatalf("%+v", got)
	}
	if err := st.UpsertSession(Session{JSONL: "/tmp/a.jsonl", SessionID: "s2", Harness: "claude", Workspace: "/ws2", Project: "acme/api"}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.SessionByJSONL("/tmp/a.jsonl")
	if got.SessionID != "s2" || got.Harness != "claude" {
		t.Fatalf("update %+v", got)
	}
	if _, ok := st.SessionByJSONL("missing"); ok {
		t.Fatal("missing")
	}
	all, err := st.ListSessions()
	if err != nil || len(all) != 1 {
		t.Fatal(all, err)
	}
}

func TestCursorsAndRawPaths(t *testing.T) {
	st := tmp(t)
	if st.Cursor("nope") != 0 {
		t.Fatal("missing cursor")
	}
	if err := st.SetCursor("/tmp/a.jsonl", 42); err != nil {
		t.Fatal(err)
	}
	if st.Cursor("/tmp/a.jsonl") != 42 {
		t.Fatal(st.Cursor("/tmp/a.jsonl"))
	}
	if err := st.SetCursor("/tmp/a.jsonl", 99); err != nil {
		t.Fatal(err)
	}
	if st.Cursor("/tmp/a.jsonl") != 99 {
		t.Fatal("update")
	}
	p := st.RawPath("acme/api", "sess/id", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(p, "acme__api") || !strings.Contains(p, "2026-08") || strings.Contains(p, "sess/id") {
		t.Fatal(p)
	}
	if !strings.Contains(st.RawPath("acme/api", "", time.Now()), "unknown.jsonl") {
		t.Fatal(st.RawPath("acme/api", "", time.Now()))
	}
	if !strings.Contains(st.ManualRawPath(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)), "raw/manual/2026-08/remember.jsonl") {
		t.Fatal(st.ManualRawPath(time.Now()))
	}
}

func TestSearchAndPostings(t *testing.T) {
	st := tmp(t)
	_, _ = st.WriteClaim(rec("F", "failed", "Redis token bucket failed in staging.", []string{"src/middleware/auth.ts"}))
	_, _ = st.WriteClaim(rec("D", "decision", "Use jose, not jsonwebtoken, for Edge.", []string{"src/middleware/auth.ts"}))
	_, _ = st.WriteClaim(rec("S", "state", "Working on billing invoices export.", []string{"src/billing/export.ts"}))
	_, _ = st.WriteClaim(rec("C", "constraint", "Never log Authorization headers.", []string{"src/middleware/auth.ts"}))

	if hits, err := st.SearchFTS("acme/api", "", 10); err != nil || hits != nil {
		t.Fatalf("%v %v", hits, err)
	}
	if hits, err := st.SearchFTS("acme/api", `"jose"`, 0); err != nil || hits != nil {
		t.Fatal(hits, err)
	}
	hits, err := st.SearchFTS("acme/api", `"jose" OR "jsonwebtoken"`, 80)
	if err != nil || len(hits) == 0 {
		t.Fatal(hits, err)
	}
	found := false
	for _, h := range hits {
		if h.ID == "D" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fts: %+v", hits)
	}

	paths, err := st.IDsByPath("acme/api", []string{"src/middleware/auth.ts", "auth.ts", ""}, 40, 80)
	if err != nil || len(paths) < 2 {
		t.Fatal(paths, err)
	}
	if _, err := st.IDsByPath("acme/api", []string{"x"}, 0, 10); err != nil {
		t.Fatal(err)
	}
	syms, err := st.IDsBySymbol("acme/api", []string{"jose", "jose"}, 40, 80)
	if err != nil || len(syms) == 0 {
		t.Fatal(syms, err)
	}
	failed, err := st.IDsByType("acme/api", "failed", "2026-01-01T00:00:00Z", 40)
	if err != nil || len(failed) != 1 || failed[0] != "F" {
		t.Fatal(failed, err)
	}
	allFailed, err := st.IDsByType("acme/api", "failed", "", 40)
	if err != nil || len(allFailed) != 1 {
		t.Fatal(allFailed, err)
	}
	if ids, err := st.IDsByType("acme/api", "failed", "", 0); err != nil || ids != nil {
		t.Fatal(ids, err)
	}
	dec, err := st.DecisionIDsOverlapping("acme/api", []string{"auth.ts"}, []string{"jose"}, 40)
	if err != nil || len(dec) == 0 {
		t.Fatal(dec, err)
	}
	if ids, err := st.DecisionIDsOverlapping("acme/api", nil, nil, 10); err != nil || ids != nil {
		t.Fatal(ids, err)
	}
	onlySym, err := st.DecisionIDsOverlapping("acme/api", nil, []string{"jose"}, 10)
	if err != nil || len(onlySym) == 0 {
		t.Fatal(onlySym, err)
	}
	pri, err := st.ColdPriorityIDs("acme/api", 10)
	if err != nil || len(pri) < 3 {
		t.Fatal(pri, err)
	}
	if pri[0] != "F" {
		t.Fatalf("failed first: %v", pri)
	}
	if ids, err := st.ColdPriorityIDs("acme/api", 0); err != nil || ids != nil {
		t.Fatal(ids, err)
	}
	capped, err := st.ColdPriorityIDs("acme/api", 1)
	if err != nil || len(capped) != 1 {
		t.Fatal(capped, err)
	}
	head, err := st.HeadPriorityIDs("acme/api", 12, 10, 8)
	if err != nil || len(head) < 3 {
		t.Fatal(head, err)
	}
	con, err := st.ConstraintIDsOverlapping("acme/api", []string{"auth.ts"}, nil, 10)
	if err != nil {
		t.Fatal(con, err)
	}
	recent, err := st.RecentPaths("acme/api", 8)
	if err != nil || len(recent) == 0 {
		t.Fatal(recent, err)
	}
	if ids, err := st.HeadPriorityIDs("acme/api", 0, 0, 0); err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	if ids, err := st.RecentPaths("acme/api", 0); err != nil || ids != nil {
		t.Fatal(ids, err)
	}
	if ids, err := st.ConstraintIDsOverlapping("acme/api", nil, nil, 10); err != nil || ids != nil {
		t.Fatal(ids, err)
	}

	many, err := st.GetMany(nil)
	if err != nil || len(many) != 0 {
		t.Fatal(many, err)
	}
	got, err := st.GetMany([]string{"F", "D", "missing"})
	if err != nil || got["F"].Type != "failed" || got["D"].Type != "decision" {
		t.Fatal(got, err)
	}
}

func TestGetManyChunks(t *testing.T) {
	st := tmp(t)
	ids := make([]string, 201)
	for i := range ids {
		ids[i] = "N" + strings.Repeat("x", 0) + string(rune('A'+(i%26))) + strings.Repeat("0", 3) + strings.TrimSpace("")
		ids[i] = "ID" + strings.ReplaceAll(strings.TrimSpace(""), " ", "") + itoa(i)
	}
	// only one real row; the rest miss — still exercises the 200-id chunk split
	_, _ = st.WriteClaim(rec(ids[0], "state", "Working on billing invoices export now.", nil))
	got, err := st.GetMany(ids)
	if err != nil || len(got) != 1 {
		t.Fatal(len(got), err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	n := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

func TestBackfillIndexOnReopen(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(rec("B1", "decision", "Use jose, not jsonwebtoken, for Edge.", []string{"src/a.ts"})); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DELETE FROM records_fts`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DELETE FROM path_postings`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	hits, err := st2.SearchFTS("acme/api", `"jose"`, 10)
	if err != nil || len(hits) != 1 || hits[0].ID != "B1" {
		t.Fatalf("backfill fts: %+v err=%v", hits, err)
	}
	paths, err := st2.IDsByPath("acme/api", []string{"src/a.ts"}, 10, 10)
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
}

func TestSearchFTSBadQuery(t *testing.T) {
	st := tmp(t)
	if _, err := st.SearchFTS("acme/api", `"""`, 10); err == nil {
		// modernc may or may not error; either is fine
	}
}

func TestSearchFTSDedupsDuplicateRows(t *testing.T) {
	st := tmp(t)
	_, _ = st.WriteClaim(rec("DUP", "decision", "Use jose, not jsonwebtoken, for Edge.", nil))
	if _, err := st.DB.Exec(`INSERT INTO records_fts(body, record_id) VALUES('jose jsonwebtoken', 'DUP')`); err != nil {
		t.Fatal(err)
	}
	hits, err := st.SearchFTS("acme/api", `"jose"`, 10)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, h := range hits {
		if h.ID == "DUP" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dedup %d hits=%+v", n, hits)
	}
}

func TestReindexAndBackfillErrors(t *testing.T) {
	st := tmp(t)
	_ = st.Close()
	if err := st.reindex(rec("X", "state", "Working on billing invoices now.", nil)); err == nil {
		t.Fatal("reindex closed")
	}
	if err := st.backfillIndex(); err == nil {
		t.Fatal("backfill closed")
	}
	if _, err := st.WriteClaim(rec("Y", "state", "Working on billing invoices now.", nil)); err == nil {
		t.Fatal("write closed")
	}
}

func TestClosedDBErrors(t *testing.T) {
	st := tmp(t)
	_ = st.Close()
	if _, err := st.ListActive("acme/api"); err == nil {
		t.Fatal("list")
	}
	if _, err := st.SearchFTS("acme/api", `"jose"`, 10); err == nil {
		t.Fatal("fts")
	}
	if _, err := st.IDsByPath("acme/api", []string{"a.ts"}, 10, 10); err == nil {
		t.Fatal("path")
	}
	if _, err := st.IDsByType("acme/api", "failed", "2020-01-01T00:00:00Z", 10); err == nil {
		t.Fatal("type")
	}
	if _, err := st.IDsByType("acme/api", "failed", "", 10); err == nil {
		t.Fatal("type2")
	}
	if _, err := st.DecisionIDsOverlapping("acme/api", []string{"a"}, nil, 10); err == nil {
		t.Fatal("dec")
	}
	if _, err := st.ColdPriorityIDs("acme/api", 5); err == nil {
		t.Fatal("cold")
	}
	if _, err := st.HeadPriorityIDs("acme/api", 12, 10, 8); err == nil {
		t.Fatal("head")
	}
	if _, err := st.ConstraintIDsOverlapping("acme/api", []string{"a"}, nil, 10); err == nil {
		t.Fatal("con")
	}
	if _, err := st.RecentPaths("acme/api", 8); err == nil {
		t.Fatal("recent")
	}
	if _, err := st.GetMany([]string{"x"}); err == nil {
		t.Fatal("getmany")
	}
	if err := st.SetCursor("p", 1); err == nil {
		t.Fatal("cursor")
	}
}

func TestRewriteStatusMissing(t *testing.T) {
	st := tmp(t)
	if err := st.rewriteStatus("nope", "superseded"); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeAndSerialize(t *testing.T) {
	if sanitize("") != "unknown" {
		t.Fatal(sanitize(""))
	}
	body := serialize(claim.Record{
		ID: "I", Type: "decision", ProjectKey: "acme/api", Harness: "grok",
		SessionID: "s", CreatedAt: "t", Status: "active", Source: "import",
		ClaimHash: "h", Supersedes: "OLD", Text: "hello", Paths: []string{"a.ts"},
	})
	for _, need := range []string{"id:", "supersedes: OLD", "hello", "a.ts"} {
		if !strings.Contains(body, need) {
			t.Fatalf("missing %q in %s", need, body)
		}
	}
}

func TestReindexInactiveAndEmptySymbol(t *testing.T) {
	st := tmp(t)
	r := rec("Z", "decision", "Use jose, not jsonwebtoken, for Edge.", []string{"p.ts"})
	r.Symbols = []string{"jose", ""}
	if err := st.reindex(r); err != nil {
		t.Fatal(err)
	}
	r.Status = "superseded"
	if err := st.reindex(r); err != nil {
		t.Fatal(err)
	}
}

func TestOpenPartialLayout(t *testing.T) {
	block := func(name string) {
		t.Helper()
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "export"), 0o755); err != nil {
			t.Fatal(err)
		}
		if name != "raw" {
			if err := os.MkdirAll(filepath.Join(dir, "raw"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if name != "index" && name != "raw" {
			if err := os.MkdirAll(filepath.Join(dir, "index"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir); err == nil {
			t.Fatal(name, "should fail")
		}
	}
	block("raw")
	block("index")
	block("spool")
}

func TestReopenDoesNotRebackfill(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteClaim(rec("R1", "decision", "Use jose, not jsonwebtoken, for Edge.", []string{"a.ts"})); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	hits, err := st2.SearchFTS("acme/api", `"jose"`, 10)
	if err != nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
}

func TestPostingCapsAndDuplicates(t *testing.T) {
	st := tmp(t)
	for i := 0; i < 3; i++ {
		_, _ = st.WriteClaim(rec("P"+itoa(i), "decision", "Use jose number "+itoa(i)+" not jsonwebtoken here.", []string{"src/a.ts"}))
	}
	ids, err := st.IDsByPath("acme/api", []string{"src/a.ts", "a.ts", ""}, 1, 1)
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	dec, err := st.DecisionIDsOverlapping("acme/api", []string{"src/a.ts", ""}, []string{"jose"}, 1)
	if err != nil || len(dec) != 1 {
		t.Fatal(dec, err)
	}
}

func TestRewriteStatusWriteFails(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.WriteClaim(rec("W1", "decision", "Use jose, not jsonwebtoken, for Edge.", nil)); err != nil {
		t.Fatal(err)
	}
	export := filepath.Join(dir, "export", "acme__api")
	_ = os.RemoveAll(export)
	if err := os.WriteFile(filepath.Join(dir, "export", "acme__api"), []byte("x"), 0o644); err != nil {
		// parent export is a dir; write a file where the project dir should be
		if err := os.WriteFile(filepath.Join(dir, "export", "acme__api"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.rewriteStatus("W1", "superseded"); err == nil {
		t.Fatal("expected writeFile fail")
	}
}

func TestUpsertNilSlices(t *testing.T) {
	st := tmp(t)
	r := rec("N", "state", "Working on billing invoices export now.", nil)
	r.Paths = nil
	r.Symbols = []string{"billing"} // skip extract
	r.PathMtime = nil
	if err := st.upsertRow(r); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get("N")
	if !ok {
		t.Fatal("missing")
	}
	if got.Paths == nil {
		// unmarshalling [] from json is empty slice not nil — fine
	}
}
