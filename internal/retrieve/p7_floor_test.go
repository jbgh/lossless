package retrieve

import (
	"strings"
	"testing"

	"lossless/internal/claim"
)

// Five slots were a quota: the packer filled them by type and age when
// nothing in the project matched the goal, so an ask about a settings page
// came back with a warehouse timeout and a rule about deploys. A replay of
// real asks found about half the packed records shared no file, no
// identifier, and at most one ordinary word with a thirty-word goal.

func TestTopicNeedScalesWithGoalSize(t *testing.T) {
	for _, c := range []struct{ n, want int }{{0, 1}, {1, 1}, {16, 1}, {17, 2}, {32, 2}, {33, 3}, {55, 4}} {
		if got := topicNeed(c.n); got != c.want {
			t.Fatalf("topicNeed(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestAskOffEveryRecordsTopicPacksNothing(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JWARE", Type: "failed",
		Text:      "The warehouse export failed after the cursor timed out on the nightly run.",
		CreatedAt: "2026-08-10T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JQUEUE", Type: "decision", Source: "remember",
		Text:      "Chose the durable queue over cron for the invoice mailer after the outage.",
		CreatedAt: "2026-08-11T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JDEPLOY", Type: "constraint", Source: "remember",
		Text:      "Never deploy on a Friday after the freeze starts.",
		CreatedAt: "2026-08-12T09:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal:    "add a dark mode toggle to the settings page after the redesign",
	})
	if len(out.Context) != 0 {
		t.Fatalf("records that share only function words packed: %s", textsOf(out))
	}
	if len(out.Warnings) != 0 {
		t.Fatalf("warnings on an off-topic pack: %v", out.Warnings)
	}
}

func TestShortGoalOneSharedWordStillPacks(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JQUEUE", Type: "decision", Source: "remember",
		Text:      "Chose the durable queue over cron for the invoice mailer after the outage.",
		CreatedAt: "2026-08-11T09:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "retry the mailer on a timeout"})
	if !strings.Contains(textsOf(out), "durable queue") {
		t.Fatalf("one shared topic word on a short goal must pack: %+v", out.Context)
	}
}

// One ordinary word in common with a long goal is chance, two is a topic.
func TestLongGoalNeedsTwoSharedWords(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JONE", Type: "decision", Source: "remember",
		Text:      "Chose a serif face for the printed invoice footer.",
		CreatedAt: "2026-08-11T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JTWO", Type: "decision", Source: "remember",
		Text:      "Chose keyset paging for the orders table so the export stays stable.",
		CreatedAt: "2026-08-11T10:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal: "rebuild the orders screen with sticky headers, column sorting, saved filters, bulk select, " +
			"inline editing, keyboard shortcuts, an empty state, loading skeletons, and a csv export button " +
			"beside the invoice link in the toolbar",
	})
	texts := textsOf(out)
	if strings.Contains(texts, "serif") {
		t.Fatalf("one shared word on a long goal packed: %s", texts)
	}
	if !strings.Contains(texts, "keyset") {
		t.Fatalf("two shared words on a long goal must pack: %s", texts)
	}
}

// A code span is the most specific word a record has: the floor reads it,
// though the warning route does not.
func TestFloorCountsWordsInsideCodeSpans(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JTICK", Type: "decision", Source: "remember",
		Text:      "Went with `backoff` jitter over a fixed delay.",
		CreatedAt: "2026-08-11T09:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "tune the backoff schedule"})
	if !strings.Contains(textsOf(out), "jitter") {
		t.Fatalf("a word shared inside a code span must pack: %+v", out.Context)
	}
}

// The hop reaches a record with no word in common through a file an
// on-topic record names. A file named only by filler carries nothing.
func TestHopNeedsAnOnTopicSeed(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JLIMITER", Type: "decision", Source: "remember",
		Text:      "Chose a fixed-window counter for rate limiting on login.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-08-02T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JREDIS", Type: "failed",
		Text:      "Redis token bucket failed in staging; connection pool exhausted.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-08-01T18:12:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JFILLER", Type: "decision", Source: "remember",
		Text:      "Chose to add the footer on the receipt.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-03T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JWARE", Type: "failed",
		Text:      "Warehouse query failed because the cursor timed out.",
		Paths:     []string{"src/billing/export.ts"},
		CreatedAt: "2026-08-10T09:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "add rate limiting on the api"})
	texts := textsOf(out)
	if !strings.Contains(texts, "fixed-window") || !strings.Contains(texts, "Redis") {
		t.Fatalf("hop from the on-topic decision lost the failure on its file: %s", texts)
	}
	if strings.Contains(texts, "footer") || strings.Contains(texts, "Warehouse") {
		t.Fatalf("hop through a filler record packed: %s", texts)
	}
}

// An ask with no content words of its own has nothing to judge by.
func TestFloorNeedsAVocabulary(t *testing.T) {
	rec := claim.Record{ID: "01JWARE", Text: "Warehouse query failed because the cursor timed out."}
	if belowFloor(query{}, nil, false, rec) {
		t.Fatal("a bare continue must not be floored")
	}
	if belowFloor(query{}, []string{"limiter"}, true, rec) {
		t.Fatal("strong overlap is above the floor whatever the words")
	}
	if !belowFloor(query{}, []string{"limiter"}, false, rec) {
		t.Fatal("no shared word, no file, no identifier: below the floor")
	}
	opened := query{Dwell: map[string]bool{"01JWARE": true}, Continue: true}
	if belowFloor(opened, []string{"limiter"}, false, rec) {
		t.Fatal("a record the agent opened stays while the work continues")
	}
	opened.Continue = false
	if !belowFloor(opened, []string{"limiter"}, false, rec) {
		t.Fatal("an opened record does not ride into a new topic")
	}
}

// bm25 counts function words. The constraint rescue reserved a slot for
// the top lexical match even when every matched word was one of them.
func TestConstraintRescueStaysAboveTheFloor(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JSHOW", Type: "constraint", Source: "remember",
		Text:      "Do not commit or deploy any of it, just show what is there and what is not.",
		CreatedAt: "2026-08-12T09:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "01JPALETTE", Type: "decision", Source: "remember",
		Text:      "It is the warm palette.",
		CreatedAt: "2026-08-11T09:00:00Z",
	})
	e := Engine{Store: st}
	p, err := e.prepare(Request{
		Project: "acme/api",
		Goal:    "what is known about the shield pickup and any flicker of it when it is not active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.out.Context) != 0 {
		t.Fatalf("function-word match rescued: %+v", p.out.Context)
	}
	var reason string
	for _, d := range p.drops {
		if d.rec.ID == "01JSHOW" {
			reason = d.reason
		}
	}
	if reason != "off-topic" {
		t.Fatalf("inspect must say why it dropped, got %q", reason)
	}
}

// The floor counts stems too: "rate limits" is on topic for a long goal
// about rate limiting.
func TestFloorCountsStems(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JLIMIT", Type: "decision", Source: "remember",
		Text:      "Chose the fixed-window counter for login rate limits and retries.",
		CreatedAt: "2026-08-11T09:00:00Z",
	})
	out := askAt(t, st, Request{
		Project: "acme/api",
		Goal: "rework the public api gateway: request validation, rate limiting per tenant, retry budgets, " +
			"structured error bodies, tracing headers, idempotency keys, pagination cursors, and a deprecation " +
			"notice for the legacy version",
	})
	if !strings.Contains(textsOf(out), "fixed-window") {
		t.Fatalf("stem match below the floor: %+v", out.Context)
	}
}

// When nothing in the store matches at all, ask still offers the most
// recent failures. That net is not a match, and the floor leaves it be.
func TestRecentFailedNetSurvivesTheFloor(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JWARE", Type: "failed", Source: "remember",
		Text:      "Warehouse export failed: cursor timeout.",
		CreatedAt: "2026-08-10T09:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "zzyzx quuxing frobnicate"})
	if len(out.Context) != 1 || out.Context[0].ID != "01JWARE" {
		t.Fatalf("net: %+v", out.Context)
	}
}

// A decision filed on pack.go is about "pack ordering" whatever words it
// uses: the floor reads the names of a record's files.
func TestFloorReadsFileNames(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "01JWALK", Type: "decision", Source: "remember",
		Text:      "Keep the walk coverage-first, then by score.",
		Paths:     []string{"internal/retrieve/pack.go"},
		CreatedAt: "2026-08-11T09:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "change pack ordering"})
	if !strings.Contains(textsOf(out), "coverage-first") {
		t.Fatalf("a record tied by its file name fell below the floor: %+v", out.Context)
	}
}
