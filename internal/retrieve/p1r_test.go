package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
)

// Constraints are user-typed by construction; an arrow in one is memory.
func TestAskKeepsUserArrowConstraint(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{ID: "C_ARROW", Type: "constraint", Source: "turn",
		Text: "Never rename queue → jobs without a migration.", CreatedAt: "2026-08-10T00:00:00Z"})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "rename the queue table to jobs"})
	if !strings.Contains(textsOf(out), "queue → jobs") {
		t.Fatalf("user arrow constraint dropped at read time: %s", textsOf(out))
	}
}

// A path the extractor attached (own or neighbor) is trusted at read time.
func TestAskTrustsAttachedDecisionPath(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{ID: "D_TB", Type: "decision", Source: "turn",
		Text: "We decided to keep the token bucket.", Paths: []string{"src/limiter.go"}, CreatedAt: "2026-08-10T00:00:00Z"})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "rework the token bucket", Paths: []string{"src/limiter.go"}})
	if !strings.Contains(textsOf(out), "token bucket") {
		t.Fatalf("neighbor-attached decision unretrievable: %s", textsOf(out))
	}
}

// evictFailed's diversity gate is against packed faileds only: a job-1
// failed that restates a packed decision still evicts it and warns.
func TestEvictFailedEvictsDecisionForNearDuplicateFailed(t *testing.T) {
	mk := func(id, typ, text string, fail, score float64) scored {
		return scored{rec: claim.Record{ID: id, Type: typ, Text: text, Paths: []string{"src/limiter.ts"}},
			failedOverlap: fail, path: 1, score: score}
	}
	f1 := mk("F1", "failed", "Redis pool exhausted in src/cache/redis.go under the burst test.", 1, 6)
	d := mk("D", "decision", "Token bucket refill under burst load in src/limiter.ts after the cap change.", 0, 5)
	f := mk("F", "failed", "Token bucket refill failed under burst load in src/limiter.ts after the cap change.", 1, 4)
	out := evictFailed([]scored{f1, d}, []scored{f1, d, f}, DefaultLimit)
	for _, c := range out {
		if c.rec.ID == "F" {
			return
		}
	}
	t.Fatalf("job-1 failed lost to a near-duplicate decision: %v", scoredIDs(out))
}

// Continue detection: identical root-level files count; derived basenames don't.
func TestContinueTapeRootFilesAndBasenames(t *testing.T) {
	q := query{PathKeys: []string{"Makefile"}, QuestionTokens: []string{"bump", "toolchain"}}
	if !continueTape(q, []string{"pin", "versions"}, []string{"Makefile"}) {
		t.Fatal("identical root-level file must continue")
	}
	q2 := query{PathKeys: claim.PathKeys([]string{"cmd/main.go"}), QuestionTokens: []string{"add", "invoice"}}
	if continueTape(q2, []string{"fix", "redis"}, []string{"tools/main.go", "main.go"}) {
		t.Fatal("shared basename must not continue")
	}
}

// Previous-month probe must be the previous month even on the 31st.
func TestLocateSessionForPreviousMonthOnThe31st(t *testing.T) {
	st := tmpStore(t)
	sep := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	p := st.LiveRawPath("acme/api", "sess-a", sep)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"type":"user","content":"rewrite src/limiter.go"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oct := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	decoy := st.LiveRawPath("acme/api", "sess-b", oct)
	if err := os.MkdirAll(filepath.Dir(decoy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decoy, []byte(`{"type":"user","content":"rewrite src/billing/invoice.go"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(decoy, oct, oct)
	e := Engine{Store: st, Now: func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }}
	if got := e.locateSessionFor("acme/api", "", "sess-a"); got != p {
		t.Fatalf("locateSessionFor = %q, want %q", got, p)
	}
}
