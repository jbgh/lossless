package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
)

func TestAskDefaultNowAndWorkspaceProject(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "N1", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	out, err := Ask(st, Request{Project: "acme/api", Question: "jose"})
	if err != nil || !strings.Contains(textsOf(out), "jose") {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestAskFutureAndBadCreatedAt(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "FUT", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2099-01-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "BAD", Type: "failed", Text: "Redis token bucket failed in staging yesterday.",
		CreatedAt: "not-a-date",
	})
	out := askAt(t, st, Request{Project: "acme/api", Question: "jose redis failed"})
	if len(out.Context) == 0 {
		t.Fatal("empty")
	}
}

func TestIsStaleEdges(t *testing.T) {
	if isStale(claim.Record{}, "/tmp") {
		t.Fatal("empty")
	}
	rec := claim.Record{
		Paths:     []string{"a.ts", "b.ts"},
		PathMtime: map[string]int64{"a.ts": 99},
	}
	ws := t.TempDir()
	if isStale(rec, ws) {
		t.Fatal("missing file is not stale")
	}
	if err := os.WriteFile(filepath.Join(ws, "a.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// stored mtime in the future vs file — not stale
	rec.PathMtime["a.ts"] = time.Now().Add(time.Hour).UnixMilli()
	if isStale(rec, ws) {
		t.Fatal("stored newer")
	}
}

func TestPackScoreCutoffDiversityAndEvict(t *testing.T) {
	keep := scored{score: 2, rec: claim.Record{ID: "A", Type: "decision", Text: "alpha unique words here", ClaimHash: "h1", CreatedAt: "2"}}
	dupHash := scored{score: 1.5, rec: claim.Record{ID: "B", Type: "decision", Text: "totally different text body", ClaimHash: "h1", CreatedAt: "1"}}
	dupText := scored{score: 1.4, rec: claim.Record{ID: "C", Type: "decision", Text: "alpha unique words here", ClaimHash: "h2", CreatedAt: "1"}}
	zero := scored{score: 0, rec: claim.Record{ID: "D", Type: "state", Text: "zero score should stop", CreatedAt: "1"}}
	got := pack([]scored{keep, dupHash, dupText, zero}, 1200, false)
	if len(got) != 1 || got[0].rec.ID != "A" {
		t.Fatalf("%+v", got)
	}

	// token budget stops after first
	big := scored{score: 3, rec: claim.Record{ID: "T1", Type: "decision", Text: strings.Repeat("word ", 40)}}
	next := scored{score: 2, rec: claim.Record{ID: "T2", Type: "decision", Text: strings.Repeat("other ", 40)}}
	got = pack([]scored{big, next}, 20, false)
	if len(got) != 1 {
		t.Fatalf("token stop: %d", len(got))
	}

	// evict: packed full of decisions, missing failed_overlap
	var packed []scored
	for i := 0; i < 5; i++ {
		packed = append(packed, scored{
			score: float64(5 - i),
			rec:   claim.Record{ID: "D" + itoa(i), Type: "decision", Text: "dec"},
		})
	}
	fail := scored{score: 0.1, failedOverlap: 1, path: 1, rec: claim.Record{ID: "FAIL", Type: "failed", Text: "failed", Paths: []string{"a.ts"}}}
	all := append(append([]scored{}, packed...), fail)
	out := evictFailed(packed, all, 1200)
	found := false
	for _, s := range out {
		if s.rec.ID == "FAIL" {
			found = true
		}
	}
	if !found || len(out) > 5 {
		t.Fatalf("evict: %+v", out)
	}

	// evict when under cap: just append
	under := evictFailed([]scored{packed[0]}, all, 1200)
	if len(under) < 2 {
		t.Fatal(under)
	}

	// nothing missing
	if len(evictFailed(all, all, 1200)) != len(all) && evictFailed(packed, packed, 1200)[0].rec.ID != packed[0].rec.ID {
		t.Fatal("noop")
	}

	// cannot evict if all packed are failed with overlap
	allFail := []scored{
		{score: 5, failedOverlap: 1, rec: claim.Record{ID: "F1", Type: "failed"}},
		{score: 4, failedOverlap: 1, rec: claim.Record{ID: "F2", Type: "failed"}},
		{score: 3, failedOverlap: 1, rec: claim.Record{ID: "F3", Type: "failed"}},
		{score: 2, failedOverlap: 1, rec: claim.Record{ID: "F4", Type: "failed"}},
		{score: 1, failedOverlap: 1, rec: claim.Record{ID: "F5", Type: "failed"}},
	}
	extra := scored{score: 0.1, failedOverlap: 1, rec: claim.Record{ID: "F6", Type: "failed"}}
	got = evictFailed(allFail, append(allFail, extra), 1200)
	for _, s := range got {
		if s.rec.ID == "F6" {
			t.Fatal("should not evict a failed for a failed")
		}
	}

	// sort ties by created_at then id
	sortScored([]scored{
		{score: 1, rec: claim.Record{ID: "b", CreatedAt: "1"}},
		{score: 1, rec: claim.Record{ID: "a", CreatedAt: "1"}},
		{score: 1, rec: claim.Record{ID: "c", CreatedAt: "2"}},
	})

	// oon state does not take a slot when a decision is available
	dec := scored{score: 3, rec: claim.Record{ID: "DEC", Type: "decision", Text: "we decided to use jose"}}
	oon := scored{score: 4, oon: 1, rec: claim.Record{ID: "SKILL", Type: "state", Text: "a model can ignore a skill"}}
	got = pack([]scored{oon, dec}, 1200, false)
	if len(got) != 1 || got[0].rec.ID != "DEC" {
		t.Fatalf("oon state packed: %+v", got)
	}

	// pathless weekday failed chatter does not sit next to a real constraint
	con := scored{score: 4.6, rec: claim.Record{ID: "CON", Type: "constraint", Text: "never run mobile-down --full"}}
	weak := scored{score: 1.8, rec: claim.Record{ID: "CHAT", Type: "failed", Text: "the prefetch window failed in grid"}}
	got = pack([]scored{weak, con}, 1200, false)
	if len(got) != 1 || got[0].rec.ID != "CON" {
		t.Fatalf("ungrounded failed packed: %+v", got)
	}
}

func itoa(i int) string {
	return string(rune('0' + i))
}

func TestAskClosedStore(t *testing.T) {
	st := tmpStore(t)
	_ = st.Close()
	if _, err := Ask(st, Request{Project: "acme/api", Question: "jose", Paths: []string{"a.ts"}}); err == nil {
		t.Fatal("expected error")
	}
	st2 := tmpStore(t)
	_ = st2.Close()
	if _, err := Ask(st2, Request{Project: "acme/api"}); err == nil {
		t.Fatal("cold error")
	}
}

func TestAskDedupSameHashKeepsNewest(t *testing.T) {
	st := tmpStore(t)
	text := "Use jose, not jsonwebtoken, for Edge."
	writeRec(t, st, claim.Record{
		ID: "OLDH", Type: "decision", Text: text, CreatedAt: "2026-07-01T00:00:00Z",
	})
	h := claim.Hash("acme/api", "decision", text)
	if _, err := st.WriteClaim(claim.Record{
		ID: "NEWH", Type: "decision", Text: text + " runtime.",
		ProjectKey: "acme/api", Harness: "grok", SessionID: "eval",
		CreatedAt: "2026-08-01T00:00:00Z", Status: "active", Source: "import",
		ClaimHash: h, Symbols: []string{"jose"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO records_fts(body, record_id) VALUES(?, 'OLDH')`, text); err != nil {
		t.Fatal(err)
	}
	out := askAt(t, st, Request{Project: "acme/api", Question: "jsonwebtoken"})
	if containsID(out, "OLDH") && containsID(out, "NEWH") {
		t.Fatal("both packed")
	}
}

func containsID(out Response, id string) bool {
	for _, h := range out.Context {
		if h.ID == id {
			return true
		}
	}
	return false
}

func TestColdAskIncludesStateWithoutPaths(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "ST1", Type: "state", Text: "Working on billing invoices export, not auth.",
		Paths: []string{"src/billing/export.ts"}, CreatedAt: "2026-08-10T00:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api"})
	if !strings.Contains(textsOf(out), "billing") {
		t.Fatalf("%+v", out)
	}
}

func TestNormBM25MinMax(t *testing.T) {
	normBM25(nil)
	cs := []scored{
		{isFTS: true, ftsRaw: 2},
		{isFTS: true, ftsRaw: 4},
		{isFTS: false},
	}
	normBM25(cs)
	if cs[0].bm25 != 1 || cs[1].bm25 != 0 || cs[2].bm25 != 0 {
		t.Fatalf("%+v", cs)
	}
	same := []scored{{isFTS: true, ftsRaw: 3}, {isFTS: true, ftsRaw: 3}}
	normBM25(same)
	if same[0].bm25 != 1 || same[1].bm25 != 1 {
		t.Fatalf("%+v", same)
	}
}

func TestShippedOverlapViaSymbol(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "SYM", Type: "decision", Text: "Picked the widget factory for Edge.",
		Symbols: []string{"widgetFactory"}, CreatedAt: "2026-08-01T00:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Question: "widgetFactory status"})
	if !hasWarn(out, "already cover") {
		t.Fatalf("expected shipped warn: %+v", out)
	}
}

func TestStaleMissingMtimeKey(t *testing.T) {
	st := tmpStore(t)
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRec(t, st, claim.Record{
		ID: "MT", Type: "decision", Text: "Keep the limiter in a.ts for now please.",
		Paths: []string{"a.ts", "b.ts"}, PathMtime: map[string]int64{"b.ts": 1},
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", WorkspaceRoot: ws, Question: "limiter"})
	if len(out.Context) == 0 {
		t.Fatal("empty")
	}
	if strings.HasPrefix(out.Context[0].Text, "[verify]") {
		t.Fatal("b.ts missing should not verify via a.ts")
	}
}

func TestAskUsesEngineNowClock(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "CLK", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	e := Engine{Store: st} // real now
	if _, err := e.Ask(Request{Project: "acme/api", Question: "jose"}); err != nil {
		t.Fatal(err)
	}
}

func TestMarkStaleCaps(t *testing.T) {
	e := Engine{}
	e.markStale(nil, "")
	cs := make([]scored, 2)
	e.markStale(cs, t.TempDir())
}

func TestEvictAppendsPastCapThenTrims(t *testing.T) {
	packed := []scored{
		{score: 3, rec: claim.Record{ID: "A", Type: "decision"}},
		{score: 2, rec: claim.Record{ID: "B", Type: "decision"}},
	}
	var all []scored
	all = append(all, packed...)
	for i := 0; i < 5; i++ {
		all = append(all, scored{score: 0.1, failedOverlap: 1, path: 1, rec: claim.Record{ID: "F" + itoa(i), Type: "failed", Paths: []string{"a.ts"}}})
	}
	out := evictFailed(packed, all, 1200)
	if len(out) > PackCap {
		t.Fatalf("len=%d", len(out))
	}
	n := 0
	for _, s := range out {
		if s.failedOverlap == 1 {
			n++
		}
	}
	if n == 0 || n > PackTypeCap {
		t.Fatalf("failed packed=%d %+v", n, out)
	}
}

func TestAskKeepsOlderHashWhenNewerAlreadySeen(t *testing.T) {
	st := tmpStore(t)
	text := "Use jose, not jsonwebtoken, for Edge."
	h := claim.Hash("acme/api", "decision", text)
	writeRec(t, st, claim.Record{
		ID: "NEWER", Type: "decision", Text: text, CreatedAt: "2026-08-10T00:00:00Z",
	})
	if _, err := st.DB.Exec(`INSERT INTO records(`+`id, type, project_key, workspace_root, harness, session_id, created_at, text, why, paths_json, symbols_json, path_mtime_json, status, supersedes, source, claim_hash`+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"OLDER", "decision", "acme/api", "", "grok", "eval", "2026-07-01T00:00:00Z", text, "", "[]", `["jose"]`, "{}", "superseded", "", "import", h,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO records_fts(body, record_id) VALUES(?, 'OLDER')`, text); err != nil {
		t.Fatal(err)
	}
	out := askAt(t, st, Request{Project: "acme/api", Question: "jsonwebtoken"})
	if containsID(out, "OLDER") && containsID(out, "NEWER") {
		t.Fatal("both")
	}
}

func TestAskSkipsInactiveCandidate(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "ACT", Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.",
		CreatedAt: "2026-08-01T00:00:00Z",
	})
	if _, err := st.DB.Exec(`INSERT INTO records_fts(body, record_id) VALUES('jsonwebtoken ghost', 'GHOST')`); err != nil {
		t.Fatal(err)
	}
	out := askAt(t, st, Request{Project: "acme/api", Question: "jsonwebtoken"})
	if containsID(out, "GHOST") {
		t.Fatal("ghost")
	}
}

func TestGetManyMissInAsk(t *testing.T) {
	// candidate id that disappeared: covered when FTS/path return nothing for other project
	st := tmpStore(t)
	out := askAt(t, st, Request{Project: "other/p", Question: "jose"})
	if len(out.Context) != 0 {
		t.Fatal(out)
	}
}

func TestAskMemoraLikeDoesNotFloodFaileds(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "HEADING", Type: "failed", SessionID: "live",
		Text: "## Investigation: why those uploads failed", CreatedAt: "2026-08-01T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "WHY", Type: "constraint", SessionID: "live",
		Text:      "the scrubbing video doesn't seem to work why don't you verify on the emulator and you'll see what i mean",
		CreatedAt: "2026-08-02T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "REACT", Type: "failed", SessionID: "live",
		Text: "Who-reacted failed in preview.", CreatedAt: "2026-08-03T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "CI", Type: "failed", SessionID: "live",
		Text:      "CI failed on PR #3088 — investigating and fixing before TestFlight.",
		CreatedAt: "2026-08-04T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "LIGHTBOX", Type: "decision", SessionID: "live",
		Text:      "Android Photos-like lightbox open uses a same-window hero overlay above NavHost.",
		CreatedAt: "2026-08-05T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "QA", Type: "constraint", SessionID: "live",
		Text:  "Android lightbox QA runs on memora-eu. Never mobile-down --full on the shared emulator.",
		Paths: []string{"scripts/mobile/mobile-up.sh"}, CreatedAt: "2026-08-06T00:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api", SessionID: "live",
		Goal:     "fix iOS lightbox swipe-down dismiss hero",
		Question: "what already failed or what did we decide about lightbox",
	})
	if containsID(out, "HEADING") || containsID(out, "WHY") {
		t.Fatalf("noise: %+v", out.Context)
	}
	if !containsID(out, "LIGHTBOX") && !containsID(out, "QA") {
		t.Fatalf("real work missed: %+v", out.Context)
	}
	nFail := 0
	for _, h := range out.Context {
		if h.Type == "failed" {
			nFail++
		}
	}
	if nFail > PackTypeCap {
		t.Fatalf("failed flood %d: %+v", nFail, out.Context)
	}
}

func TestAskDropsExtractNoiseAndKeepsRealWork(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "REALFAIL", Type: "failed", SessionID: "live",
		Text:  "If a harness rewrites chat_history.jsonl smaller, a cursor past EOF makes catch-up a no-op.",
		Paths: []string{"internal/write/catchup.go"}, CreatedAt: "2026-08-17T05:12:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "REALDEC", Type: "decision", SessionID: "manual",
		Text:  "Keep the MCP/HTTP read verb as ask. Do not say ask pack; the result is context.",
		Paths: []string{"internal/harness/skill.md"}, CreatedAt: "2026-08-17T06:20:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "NOISE1", Type: "failed", SessionID: "live",
		Text:      "| **Claims** | owner/repo | A Grok `failed` on acme/api is what Claude’s ask is supposed to see.",
		CreatedAt: "2026-08-17T05:47:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "NOISE2", Type: "failed", SessionID: "live",
		Text:      "Force in the best failed-overlap (do not repeat burned work).",
		CreatedAt: "2026-08-17T06:21:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "NOISE3", Type: "failed", SessionID: "live",
		Text:      "Raising to 8 or 10 would make recall look better with extract noise classified as `failed`.",
		CreatedAt: "2026-08-17T06:17:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api", SessionID: "live",
		Goal:     "observe ask context and improve retrieve",
		Question: "what must I not forget about retrieve, extract noise, and catch-up",
		Paths:    []string{"internal/retrieve/ask.go", "internal/write/catchup.go", "internal/write/extract.go"},
	})
	if containsID(out, "NOISE1") || containsID(out, "NOISE2") || containsID(out, "NOISE3") {
		t.Fatalf("noise in context: %+v", out.Context)
	}
	if !containsID(out, "REALFAIL") {
		t.Fatalf("real catch-up failed missing: %+v", out.Context)
	}
	nFail := 0
	for _, h := range out.Context {
		if h.Type == "failed" {
			nFail++
		}
	}
	if nFail > PackTypeCap {
		t.Fatalf("faileds=%d %+v", nFail, out.Context)
	}
}
