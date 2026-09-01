package retrieve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
)

func warnsAbout(out Response, id string) bool {
	for _, w := range out.Warnings {
		if strings.Contains(w, id) {
			return true
		}
	}
	return false
}

// A shared generic basename (cmd/main.go) between two asks is not a
// "continue": the old topic's symbols must not fire job 1 on the new one.
func TestContinueRequiresFullPathOverlapAndOwnSymbols(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "F_REDIS", Type: "failed", Source: "turn",
		Text:  "RedisClient retry backoff failed under load; connections leaked.",
		Paths: []string{"src/cache/redis.go"}, CreatedAt: "2026-08-10T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "D_INV", Type: "decision", Source: "turn",
		Text:  "Invoice export renders PDFs in src/billing/invoice.go with gotenberg.",
		Paths: []string{"src/billing/invoice.go", "cmd/main.go"}, CreatedAt: "2026-08-11T00:00:00Z",
	})
	_ = askAt(t, st, Request{Project: "acme/api", SessionID: "s1",
		Goal: "fix RedisClient retry backoff", Paths: []string{"src/cache/redis.go", "cmd/main.go"}})
	out := askAt(t, st, Request{Project: "acme/api", SessionID: "s1",
		Goal: "add invoice pdf export", Paths: []string{"src/billing/invoice.go", "cmd/main.go"}})
	if warnsAbout(out, "F_REDIS") {
		t.Fatalf("topic shift inherited the redis symbols: %v", out.Warnings)
	}
}

// The one-content-token targeted rule is for lookups ("why not jose"),
// not imperatives whose verb happens to be a stop word ("add tests").
func TestTargetedRuleNeedsInterrogative(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "F_CI", Type: "failed", Source: "turn",
		Text:  "Integration tests failed on the CI runner after the cache change.",
		Paths: []string{"ci/run.sh"}, CreatedAt: "2026-08-10T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "D_JOSE", Type: "decision", Source: "turn",
		Text:  "Use jose, not jsonwebtoken, for Edge.",
		Paths: []string{"src/middleware/auth.ts"}, CreatedAt: "2026-08-10T00:00:00Z",
	})
	if out := askAt(t, st, Request{Project: "acme/api", Goal: "add tests"}); warnsAbout(out, "F_CI") {
		t.Fatalf("imperative single-token goal warned: %v", out.Warnings)
	}
	if out := askAt(t, st, Request{Project: "acme/api", Question: "why not jose"}); !warnsAbout(out, "D_JOSE") {
		t.Fatalf("interrogative lookup must still warn: %v", out.Warnings)
	}
}

// Symbol Jaccard on a terse record is one shared word in disguise.
func TestTerseRecordNeedsTwoSharedSymbols(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "F_STAGE", Type: "failed", Source: "turn", Text: "Staging deploy failed.",
		CreatedAt: "2026-08-10T00:00:00Z",
	})
	if out := askAt(t, st, Request{Project: "acme/api", Goal: "check staging"}); warnsAbout(out, "F_STAGE") {
		t.Fatalf("one shared word via Jaccard warned: %v", out.Warnings)
	}
}

// Two-hop infers files from first-pass hits by full path, not basename:
// tools/main.go must not ride in on cmd/main.go.
func TestInferredPathsIgnoreBasenames(t *testing.T) {
	st := tmpStore(t)
	writeRec(t, st, claim.Record{
		ID: "D_INV", Type: "decision", Source: "turn",
		Text:  "Invoice export renders PDFs in src/billing/invoice.go with gotenberg.",
		Paths: []string{"src/billing/invoice.go", "cmd/main.go"}, CreatedAt: "2026-08-11T00:00:00Z",
	})
	writeRec(t, st, claim.Record{
		ID: "F_TOOL", Type: "failed", Source: "turn",
		Text:  "Cron scheduler failed to start after the flag rename.",
		Paths: []string{"tools/main.go"}, CreatedAt: "2026-08-12T00:00:00Z",
	})
	out := askAt(t, st, Request{Project: "acme/api", Goal: "add invoice pdf export"})
	if strings.Contains(textsOf(out), "Cron scheduler") {
		t.Fatalf("basename hop packed an unrelated failed: %s", textsOf(out))
	}
}

// A thin ask compiles from the asking session's own tape, not whichever
// session on this project was modified last.
func TestCompileUsesAskingSession(t *testing.T) {
	st := tmpStore(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) // askAt's pinned clock
	write := func(sid, line string) {
		p := st.LiveRawPath("acme/api", sid, now)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(`{"type":"user","content":"`+line+`"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("sess-a", "rewrite the limiter in src/limiter.go")
	write("sess-b", "rewrite billing invoices in src/billing/invoice.go")
	newer := now.Add(2 * time.Second)
	_ = os.Chtimes(st.LiveRawPath("acme/api", "sess-b", now), newer, newer)

	_ = askAt(t, st, Request{Project: "acme/api", SessionID: "sess-a"})
	acts, err := st.RecentActions("acme/api", "sess-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, a := range acts {
		paths = append(paths, a.Paths...)
	}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "src/limiter.go") || strings.Contains(joined, "invoice") {
		t.Fatalf("compiled from the wrong session: %v", paths)
	}
}
