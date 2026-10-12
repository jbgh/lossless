package retrieve

import (
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
)

func hitTypes(out Response) string {
	var b strings.Builder
	for _, h := range out.Context {
		b.WriteString(h.Type + " ")
	}
	return b.String()
}

func saidRec(id, text string) claim.Record {
	return claim.Record{
		ID: id, Type: "said", Source: "turn", Text: text,
		SessionID: "earlier", CreatedAt: "2026-08-01T09:00:00Z",
		TranscriptRef: &claim.TranscriptRef{SessionID: "earlier", StartOffset: 100, EndOffset: 200},
	}
}

// What the user typed comes back in their words when a later task is
// about the same thing, labelled said, and it never warns.
func TestSaidPacksOnTopicAndNeverWarns(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, saidRec("01JSAID", "lets use the smaller model for all subagents since its pretty good now"))
	out := askAt(t, st, Request{Project: "acme/api", Goal: "dispatch subagents for the next task and pick their model"})
	if len(out.Context) != 1 || out.Context[0].Type != "said" || out.Context[0].ID != "01JSAID" {
		t.Fatalf("said missed: %+v", out.Context)
	}
	if len(out.Warnings) != 0 {
		t.Fatalf("a said record warned: %v", out.Warnings)
	}
}

// One shared word with a verbatim turn is not a reason to quote it.
func TestSaidNeedsStrongOverlap(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, saidRec("01JSAID", "lets use the smaller model for all subagents since its pretty good now"))
	out := askAt(t, st, Request{Project: "acme/api", Goal: "train a model for the pricing page"})
	if len(out.Context) != 0 {
		t.Fatalf("one shared word packed a said record: %+v", out.Context)
	}
}

// People write "languages" and "back ups"; the goal says "language"
// and "backup".
func TestSaidMatchesOnStemsAndJoinedWords(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, saidRec("01JLANG", "the tabs for the languages could be clearer, add a label under each one"))
	writeRec(t, st, claim.Record{
		ID: "01JPOWER", Type: "said", Source: "turn", SessionID: "earlier", CreatedAt: "2026-08-01T10:00:00Z",
		Text: "keep the back ups for thirty days, the retention is too short at seven",
	})
	lang := askAt(t, st, Request{Project: "acme/api", Goal: "restyle the language tab on the setup screen"})
	if !strings.Contains(textsOf(lang), "label under") {
		t.Fatalf("plural missed: %+v", lang.Context)
	}
	power := askAt(t, st, Request{Project: "acme/api", Goal: "change the backup retention window"})
	if !strings.Contains(textsOf(power), "back ups") {
		t.Fatalf("split compound missed: %+v", power.Context)
	}
}

// Every form of a word must land on one stem, or the goal's "embedding"
// never meets the user's "embeddings".
func TestStemIsOneStemPerWordFamily(t *testing.T) {
	for _, family := range [][]string{
		{"embed", "embeds", "embedded", "embedding", "embeddings"},
		{"setting", "settings"},
		{"language", "languages"},
		{"merge", "merges", "merged", "merging"},
		{"run", "runs", "running"},
		{"query", "queries"},
		{"fix", "fixes", "fixed", "fixing"},
		{"pass", "passes", "passed", "passing"},
		{"warning", "warnings"},
		{"backup", "backups"},
		{"move", "moves", "moved", "moving"},
	} {
		want := stem(family[0])
		for _, w := range family[1:] {
			if got := stem(w); got != want {
				t.Errorf("stem(%q) = %q, but stem(%q) = %q", w, got, family[0], want)
			}
		}
	}
	for _, w := range []string{"class", "bus", "need", "test"} {
		if got := stem(w); got != w {
			t.Errorf("stem(%q) = %q, want it unchanged", w, got)
		}
	}
}

// The sentence extract typed and the turn it came from are one memory.
// The typed one stays: it can warn.
func TestSaidYieldsToATypedRecordOfTheSameTurn(t *testing.T) {
	st := tmpStore(t)
	turn := &claim.TranscriptRef{SessionID: "earlier", StartOffset: 100, EndOffset: 200}
	writeRec(t, st, claim.Record{
		ID: "01JRULE", Type: "constraint", Source: "turn", SessionID: "earlier",
		Text:      "Never log Authorization headers in the middleware.",
		CreatedAt: "2026-08-01T09:00:00Z", TranscriptRef: turn,
	})
	said := saidRec("01JSAID", "Never log Authorization headers in the middleware. Also the dashboard is slow.")
	writeRec(t, st, said)
	out := askAt(t, st, Request{Project: "acme/api", Goal: "add request logging of headers in the middleware"})
	if got := hitTypes(out); got != "constraint " {
		t.Fatalf("want the constraint alone, got %q: %+v", got, out.Context)
	}
}

// The asking session's own fresh turns are still in its window. An hour
// later they may have been compacted away, and then they are memory.
func TestSaidSkipsTheAskingSessionsFreshTurns(t *testing.T) {
	st := tmpStore(t)
	fresh := saidRec("01JFRESH", "lets use the smaller model for all subagents since its pretty good now")
	fresh.SessionID = "now-session"
	fresh.CreatedAt = "2026-08-14T11:55:00Z" // askAt's clock is 12:00
	writeRec(t, st, fresh)
	req := Request{Project: "acme/api", SessionID: "now-session", Goal: "dispatch subagents for the next task and pick their model"}
	if out := askAt(t, st, req); len(out.Context) != 0 {
		t.Fatalf("echoed the session's own fresh turn: %+v", out.Context)
	}
	other := req
	other.SessionID = "child-session"
	if out := askAt(t, st, other); len(out.Context) != 1 {
		t.Fatalf("a child session must see the parent's turn: %+v", out.Context)
	}
	old := saidRec("01JOLD", "the nightly report should list every skipped migration by name")
	old.SessionID = "now-session"
	old.CreatedAt = "2026-08-14T09:00:00Z"
	writeRec(t, st, old)
	out := askAt(t, st, Request{Project: "acme/api", SessionID: "now-session", Goal: "change the nightly report for skipped migrations"})
	if len(out.Context) != 1 || out.Context[0].ID != "01JOLD" {
		t.Fatalf("an older turn of this session is memory: %+v", out.Context)
	}
}

// A said record ranks with typed records and never ahead of one that
// carries a warning.
func TestSaidDoesNotOutrankAWarning(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JDEC", Type: "decision", Source: "remember",
		Text:      "Chose the fixed-window limiter for login rate limiting.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-08-01T09:00:00Z",
	})
	said := saidRec("01JSAID", "the login rate limiting should use a fixed window limiter, keep it simple")
	said.Paths = []string{"src/middleware/auth.ts"}
	writeRec(t, st, said)
	out := askAt(t, st, Request{
		Project: "acme/api", Goal: "change login rate limiting to a sliding window limiter",
		Paths: []string{"src/middleware/auth.ts"},
	})
	if len(out.Context) < 2 || out.Context[0].ID != "01JDEC" {
		t.Fatalf("the warning-bearing decision must lead: %+v", out.Context)
	}
	if !hasWarn(out, "01JDEC") || hasWarn(out, "01JSAID") {
		t.Fatalf("warnings: %v", out.Warnings)
	}
}

// A stored machine turn (an older build, another gate) stops packing.
func TestSaidReadTimeGateDropsMachineTurns(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, saidRec("01JBRIEF", "Task: review the limiter change in the middleware and list the limiter bugs"))
	out := askAt(t, st, Request{Project: "acme/api", Goal: "review the limiter change in the middleware"})
	if len(out.Context) != 0 {
		t.Fatalf("machine turn packed: %+v", out.Context)
	}
}

// An echo or an off-goal said record is out of the pool for good: the
// path hop must not bring it back through a file an on-topic record names.
func TestSaidEchoIsNotRevivedByTheHop(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JDEC", Type: "decision", Source: "remember",
		Text:      "Chose exponential backoff with jitter for the retry logic.",
		Paths:     []string{"src/net/client.go"},
		CreatedAt: "2026-08-01T09:00:00Z",
	})
	echo := saidRec("01JECHO", "fix the retry logic in src/net/client.go, it gives up too early")
	echo.Paths = []string{"src/net/client.go"}
	echo.SessionID = "now-session"
	echo.CreatedAt = "2026-08-14T11:58:00Z"
	writeRec(t, st, echo)
	stray := saidRec("01JSTRAY", "the footer in src/net/client.go has the wrong year in the license header")
	stray.Paths = []string{"src/net/client.go"}
	writeRec(t, st, stray)
	out := askAt(t, st, Request{Project: "acme/api", SessionID: "now-session", Goal: "make the retry logic more patient"})
	if got := hitTypes(out); got != "decision " {
		t.Fatalf("want the decision alone, got %q: %+v", got, out.Context)
	}
}

// The twin rule is judged on the pack that ships. A typed sentence of the
// same turn that does not pack takes nothing with it.
func TestSaidStaysWhenItsTypedTwinDoesNotPack(t *testing.T) {
	st := tmpStore(t)
	turn := &claim.TranscriptRef{SessionID: "earlier", StartOffset: 100, EndOffset: 200}
	writeRec(t, st, claim.Record{
		ID: "01JOFF", Type: "constraint", Source: "turn", SessionID: "earlier",
		Text:      "Never rename the billing tables.",
		CreatedAt: "2026-08-01T09:00:00Z", TranscriptRef: turn,
	})
	writeRec(t, st, saidRec("01JSAID", "Never rename the billing tables. Also use the smaller model for all subagents from now on."))
	out := askAt(t, st, Request{Project: "acme/api", Goal: "dispatch subagents for the next task and pick their model"})
	if got := hitTypes(out); got != "said " {
		t.Fatalf("want the said record, got %q: %+v", got, out.Context)
	}
}

// Same session and offset is not the same turn across parts of a sealed
// tape: the sentence must be in the turn.
func TestSaidTwinNeedsTheSentenceInTheTurn(t *testing.T) {
	a := scored{rec: claim.Record{ID: "typed", Type: "decision", Text: "Chose keyset paging for the orders table.",
		TranscriptRef: &claim.TranscriptRef{SessionID: "s", StartOffset: 0}}}
	b := scored{rec: claim.Record{ID: "said", Type: "said", Text: "keep the orders export stable, people diff it every morning",
		TranscriptRef: &claim.TranscriptRef{SessionID: "s", StartOffset: 0}}}
	kept, dropped := dropSaidTwins([]scored{a, b})
	if len(kept) != 2 || len(dropped) != 0 {
		t.Fatalf("unrelated records at one offset treated as twins: kept %d dropped %d", len(kept), len(dropped))
	}
	b.rec.Text = "we Chose keyset paging for the   orders table. keep the export stable"
	kept, dropped = dropSaidTwins([]scored{a, b})
	if len(kept) != 1 || kept[0].rec.ID != "typed" || len(dropped) != 1 {
		t.Fatalf("twin kept: kept %d dropped %d", len(kept), len(dropped))
	}
}

// Typed and said hits come from different MATCH queries. Each route is
// scaled on its own, and a said hit is not a second typed FTS hit.
func TestBM25IsNormalizedPerRoute(t *testing.T) {
	cs := []scored{
		{rec: claim.Record{ID: "t1"}, isFTS: true, ftsRaw: -8},
		{rec: claim.Record{ID: "s1"}, saidFTS: true, ftsRaw: -2},
		{rec: claim.Record{ID: "s2"}, saidFTS: true, ftsRaw: -1},
		{rec: claim.Record{ID: "p1"}},
	}
	normBM25(cs)
	if cs[0].bm25 != 1 || cs[1].bm25 != 1 || cs[2].bm25 != 0 || cs[3].bm25 != 0 {
		t.Fatalf("bm25 = %v %v %v %v", cs[0].bm25, cs[1].bm25, cs[2].bm25, cs[3].bm25)
	}
	n := 0
	for _, c := range cs {
		if c.isFTS {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("said hits counted as typed FTS hits: %d", n)
	}
}

// A user turn names files as often as typed records do. It has its own
// path route, so the newest postings for a hot file stay typed.
func TestSaidDoesNotCrowdThePostingRoutes(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JOLD", Type: "decision", Source: "remember",
		Text: "Chose the fixed-window limiter.", Paths: []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-07-01T09:00:00Z",
	})
	said := saidRec("01JSAID", "the limiter in src/middleware/auth.ts should log every rejection")
	said.Paths = []string{"src/middleware/auth.ts"}
	writeRec(t, st, said)
	keys := claim.PathKeys([]string{"src/middleware/auth.ts"})
	typed, err := st.IDsByPath("acme/api", keys, 1, 1)
	if err != nil || len(typed) != 1 || typed[0] != "01JOLD" {
		t.Fatalf("typed path route = %v %v", typed, err)
	}
	saidIDs, err := st.SaidIDsByPath("acme/api", keys, 5, 5)
	if err != nil || len(saidIDs) != 1 || saidIDs[0] != "01JSAID" {
		t.Fatalf("said path route = %v %v", saidIDs, err)
	}
	if bySym, _ := st.IDsBySymbol("acme/api", []string{"limiter", "rejection"}, 5, 5); len(bySym) != 1 || bySym[0] != "01JOLD" {
		t.Fatalf("a said record is in the symbol postings: %v", bySym)
	}
}

// With an embedder attached a said record still gets no vector: not at
// write, and not when the daemon backfills vectors at start.
func TestSaidGetsNoVector(t *testing.T) {
	st := tmpStore(t)
	st.Embedder = throttleEmbedder()
	writeRec(t, st, saidRec("01JSAID", "the login throttle should slow repeated attempts, not block them"))
	writeRec(t, st, claim.Record{
		ID: "01JDEC", Type: "decision", Source: "remember",
		Text: "Chose a throttle for repeated login attempts.", CreatedAt: "2026-08-01T09:00:00Z",
	})
	if _, err := st.BackfillVectors(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM claim_vectors WHERE record_id = '01JSAID'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("said record has %d vectors (%v)", n, err)
	}
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM claim_vectors WHERE record_id = '01JDEC'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("typed record has %d vectors (%v)", n, err)
	}
}

// A compact takes the session's earlier turns out of the window: from
// then on they are memory, however recent.
func TestSaidEchoEndsAtACompact(t *testing.T) {
	st := tmpStore(t)
	fresh := saidRec("01JFRESH", "lets use the smaller model for all subagents since its pretty good now")
	fresh.SessionID = "now-session"
	fresh.CreatedAt = "2026-08-14T11:55:00Z"
	writeRec(t, st, fresh)
	req := Request{Project: "acme/api", SessionID: "now-session", Goal: "dispatch subagents for the next task and pick their model"}
	if out := askAt(t, st, req); len(out.Context) != 0 {
		t.Fatalf("setup: echo not suppressed: %+v", out.Context)
	}
	// Another session's compact says nothing about this one's window.
	markCompact(st, "acme/api", "some-other-session")
	if out := askAt(t, st, req); len(out.Context) != 0 {
		t.Fatalf("another session's compact lifted the echo rule: %+v", out.Context)
	}
	markCompact(st, "acme/api", "now-session") // stamped now, after the turn
	if out := askAt(t, st, req); len(out.Context) != 1 {
		t.Fatalf("after a compact the turn is memory: %+v", out.Context)
	}
}

// A harness that sends no session id cannot be matched by session. Its
// thin asks are compiled from the last user turn, so a fresh turn that
// reads like the ask itself is the echo.
func TestSaidEchoWithoutASessionID(t *testing.T) {
	st := tmpStore(t)
	fresh := saidRec("01JFRESH", "use the smaller model for all subagents")
	fresh.CreatedAt = "2026-08-14T11:55:00Z"
	writeRec(t, st, fresh)
	if out := askAt(t, st, Request{Project: "acme/api", Question: "use the smaller model for all subagents"}); len(out.Context) != 0 {
		t.Fatalf("the ask's own turn came back as memory: %+v", out.Context)
	}
	if out := askAt(t, st, Request{Project: "acme/api", Goal: "dispatch the subagents and choose a smaller model for the reviewers"}); len(out.Context) != 1 {
		t.Fatalf("a different ask must still get the turn: %+v", out.Context)
	}
}

// When only said records are on the goal, two pack, not five.
func TestSaidCapIsHard(t *testing.T) {
	st := tmpStore(t)
	for i, text := range []string{
		"the subagents should all run the smaller model for builds",
		"keep the subagents on the smaller model when the task is a review",
		"smaller model for subagents unless the change touches billing",
		"subagents get the smaller model, the lead keeps the larger one",
	} {
		r := saidRec("01JSAID"+string(rune('A'+i)), text)
		r.TranscriptRef = nil
		writeRec(t, st, r)
	}
	out := askAt(t, st, Request{Project: "acme/api", Goal: "pick the model for the subagents: smaller or larger"})
	if len(out.Context) != PackTypeCap {
		t.Fatalf("want %d said records, got %d: %+v", PackTypeCap, len(out.Context), out.Context)
	}
}

// A one-sentence turn gives a typed record and a said record with the
// same words. The typed one must pack, with its warning.
func TestTypedRecordWinsOverItsSaidDuplicate(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JDEC", Type: "decision", Source: "turn", SessionID: "other",
		Text:      "Going with sqlite for the store instead of postgres.",
		Paths:     []string{"internal/store/store.go"},
		CreatedAt: "2026-08-01T09:00:00Z",
	})
	said := saidRec("01JSAID", "going with sqlite for the store instead of postgres")
	said.TranscriptRef = nil
	writeRec(t, st, said)
	out := askAt(t, st, Request{
		Project: "acme/api", Goal: "move the store from sqlite to postgres",
		Paths: []string{"internal/store/store.go"},
	})
	if got := hitTypes(out); got != "decision " {
		t.Fatalf("want the decision alone, got %q: %+v", got, out.Context)
	}
	if !hasWarn(out, "01JDEC") {
		t.Fatalf("the decision's warning was lost: %v", out.Warnings)
	}
}

func TestStemOverlapDoesNotConflateShortStems(t *testing.T) {
	if n := stemOverlap([]string{"cors", "upload"}, "the core upload logic should live in one file"); n != 1 {
		t.Fatalf("cors met core: %d", n)
	}
	if n := stemOverlap([]string{"runs", "nightly"}, "run it nightly"); n != 2 {
		t.Fatalf("runs must meet run: %d", n)
	}
	if n := stemOverlap([]string{"embedding", "setting"}, "put the embeddings behind a flag and keep the settings page simple"); n != 2 {
		t.Fatalf("word families must meet: %d", n)
	}
}

// The lexical route must fetch what stemOverlap would accept, short
// stems included: "embedding" has to find "embeddings".
func TestSaidRouteFindsShortStemFamilies(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, saidRec("01JEMB", "the embeddings are slow on my laptop, add a setting to turn them off please"))
	out := askAt(t, st, Request{Project: "acme/api", Goal: "tune embedding settings"})
	if len(out.Context) != 1 || out.Context[0].ID != "01JEMB" {
		t.Fatalf("short-stem family not fetched: %+v (match %q)", out.Context, ftsStemMatch([]string{"tune", "embedding", "settings"}))
	}
}

// A typed claim on a same-named file elsewhere is worth a look. A whole
// user turn that happens to name one is not.
func TestSaidFileMatchIsTheSameFile(t *testing.T) {
	st := tmpStore(t)
	other := saidRec("01JOTHER", "open web/admin/index.ts and tell me what the init function does there today")
	other.Paths = []string{"web/admin/index.ts"}
	writeRec(t, st, other)
	same := saidRec("01JSAME", "the consumer in services/billing/index.ts must never drop a message it cannot parse")
	same.Paths = []string{"services/billing/index.ts"}
	same.TranscriptRef = nil
	writeRec(t, st, same)
	out := askAt(t, st, Request{
		Project: "acme/api", Goal: "add retry backoff to the webhook handler",
		Paths: []string{"services/billing/index.ts"},
	})
	if len(out.Context) != 1 || out.Context[0].ID != "01JSAME" {
		t.Fatalf("want the turn about this file only: %+v", out.Context)
	}
}

// Nothing typed matches: the recent-failed net still fires when a user
// turn shares a word with the ask, and that turn is not packed.
func TestRecentFailedNetIgnoresSaidCandidates(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JWARE", Type: "failed", Source: "remember",
		Text: "Warehouse export failed: cursor timeout.", CreatedAt: "2026-08-10T09:00:00Z",
	})
	writeRec(t, st, saidRec("01JSAID", "please make the dashboard charts load after the summary numbers"))
	out := askAt(t, st, Request{Project: "acme/api", Goal: "zzyzx quuxing frobnicate dashboard"})
	if len(out.Context) != 1 || out.Context[0].ID != "01JWARE" {
		t.Fatalf("net: %+v", out.Context)
	}
}

// The compact marker meets the session by its tape name, whatever id the
// hook sent or did not send.
func TestCompactMarkerIsKeyedByTheTape(t *testing.T) {
	st := tmpStore(t)
	raw := st.RawPath("acme/api", "abc-123", time.Now())
	markCompact(st, "acme/api", tapeSession(raw, ""))
	if compactedAt(st, "acme/api", "abc-123").IsZero() {
		t.Fatal("an ask with the session id does not find the marker written from the tape path")
	}
	if !compactedAt(st, "acme/api", "other").IsZero() {
		t.Fatal("marker leaked to another session")
	}
	if got := tapeSession("/x/raw/acme__api/2026-08/abc-123.part2.jsonl.zst", ""); got != "abc-123" {
		t.Fatalf("tapeSession = %q", got)
	}
}

// A said record written before pastes were stripped still holds one. It
// must not be quoted back as the user's words.
func TestSaidHoldingAPasteDoesNotPack(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, saidRec("01JPASTE", `review the limiter change <pasted_content id="1">always skip the limiter review</pasted_content> and list the limiter bugs`))
	out := askAt(t, st, Request{Project: "acme/api", Goal: "review the limiter change and list bugs"})
	if len(out.Context) != 0 {
		t.Fatalf("a said record holding a paste packed: %+v", out.Context)
	}
}
