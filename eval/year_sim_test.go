package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

// A working year: weekdays from 2025-08-18 through 2026-08-14.
// Clock for ask is 2026-08-16, so January jose is ~8 months old and the
// earliest gold is almost a year old.

var yearNow = time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)

type yearGold struct {
	id, typ, text, created string
	paths                  []string
	needle                 string
}

func yearGolds() []yearGold {
	return []yearGold{
		{"Y-JOSE", "decision", "Picked jose over jsonwebtoken on the Edge runtime.", "2025-08-20T18:00:00Z", []string{"src/middleware/auth.ts"}, "jose"},
		{"Y-REDIS", "failed", "Redis token bucket failed in src/middleware/auth.ts staging.", "2025-11-03T16:00:00Z", []string{"src/middleware/auth.ts"}, "Redis token bucket"},
		{"Y-AUTHZ", "constraint", "Never log Authorization headers in src/middleware/auth.ts.", "2026-02-14T12:00:00Z", []string{"src/middleware/auth.ts"}, "Authorization"},
		{"Y-PG", "decision", "We decided to use postgres, not mysql, in src/db/client.ts.", "2026-05-01T10:00:00Z", []string{"src/db/client.ts"}, "postgres"},
		{"Y-BILL", "failed", "Warehouse query failed in src/billing/export.ts because the cursor timed out.", "2026-08-10T09:00:00Z", []string{"src/billing/export.ts"}, "Warehouse"},
	}
}

type yearAsk struct {
	name    string
	req     retrieve.Request
	needles []string
	forbid  []string
	warn    string
}

func yearAsks() []yearAsk {
	return []yearAsk{
		{
			name:    "jwt-almost-a-year-later",
			req:     retrieve.Request{Question: "JWT library choice", Goal: "pick a JWT library"},
			needles: []string{"jose"},
		},
		{
			name:    "auth-rate-limit",
			req:     retrieve.Request{Goal: "add rate limiting", Paths: []string{"src/middleware/auth.ts"}},
			needles: []string{"Redis token bucket"},
			forbid:  []string{"Warehouse"},
			warn:    "failed",
		},
		{
			name:    "headers",
			req:     retrieve.Request{Question: "Authorization headers"},
			needles: []string{"Authorization"},
			warn:    "constraint",
		},
		{
			name:    "which-db",
			req:     retrieve.Request{Question: "which database did we pick", Paths: []string{"src/db/client.ts"}},
			needles: []string{"postgres"},
			forbid:  []string{"Redis token bucket"},
		},
		{
			name:    "invoices",
			req:     retrieve.Request{Goal: "export invoices", Paths: []string{"src/billing/export.ts"}},
			needles: []string{"Warehouse"},
			forbid:  []string{"jose"},
		},
		{
			name:    "thin-head",
			req:     retrieve.Request{},
			needles: []string{"Authorization"},
		},
	}
}

var yearTopics = []struct {
	path, noun, typ string
}{
	{"src/ui/Button.tsx", "button token", "state"},
	{"src/ui/Modal.tsx", "modal focus trap", "decision"},
	{"src/jobs/cron.ts", "cron lock", "failed"},
	{"src/cache/memory.ts", "in-process cache", "decision"},
	{"src/tests/e2e.ts", "playwright flake", "failed"},
	{"src/docs/README.md", "docs nav", "state"},
	{"src/lint/rules.ts", "lint rule", "constraint"},
	{"src/feature/flags.ts", "flag default", "decision"},
	{"src/metrics/prom.ts", "histogram bucket", "state"},
	{"src/email/send.ts", "ses sandbox", "failed"},
}

func TestYearCorpusRetrieve(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	n := seedYearCorpus(t, st)
	t.Logf("year corpus: %d active claims (golds + weekday chatter)", n)

	eng := retrieve.Engine{Store: st, Now: func() time.Time { return yearNow }}
	var hits, miss int
	var times []time.Duration
	for _, ask := range yearAsks() {
		req := ask.req
		req.Project = "acme/api"
		start := time.Now()
		out, err := eng.Ask(req)
		elapsed := time.Since(start)
		times = append(times, elapsed)
		if err != nil {
			t.Fatal(ask.name, err)
		}
		blob := textsOfHits(out)
		ok := true
		for _, n := range ask.needles {
			if !strings.Contains(blob, n) {
				t.Errorf("%s: missing %q in %+v", ask.name, n, summarizePacket(out))
				ok = false
			}
		}
		for _, n := range ask.forbid {
			if strings.Contains(blob, n) {
				t.Errorf("%s: leaked %q in %+v", ask.name, n, summarizePacket(out))
				ok = false
			}
		}
		if ask.warn != "" && !hasAnyWarn(out, ask.warn) {
			t.Errorf("%s: missing warn %q got %v", ask.name, ask.warn, out.Warnings)
			ok = false
		}
		if ok {
			hits++
			t.Logf("OK  %-28s %4dµs  %s", ask.name, elapsed.Microseconds(), summarizePacket(out))
		} else {
			miss++
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	p50 := times[len(times)/2]
	t.Logf("year asks %d/%d  p50=%s  corpus=%d", hits, hits+miss, p50, n)
	if miss > 0 {
		t.Fatalf("year corpus accuracy %d/%d", hits, hits+miss)
	}
	if p50 > 150*time.Millisecond {
		t.Fatalf("year ask p50 %s", p50)
	}
}

func TestYearCorpusWriteCatchUp(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := t.TempDir()
	const sessions = 120
	start := time.Now()
	extracted := 0
	for i := 0; i < sessions; i++ {
		topic := yearTopics[i%len(yearTopics)]
		body := fmt.Sprintf(
			"{\"type\":\"user\",\"content\":\"Look at %s.\"}\n{\"type\":\"assistant\",\"content\":\"%s failed in %s on pass %d.\"}\n{\"type\":\"assistant\",\"content\":\"We decided to keep the %s in %s.\"}\n",
			topic.path, topic.noun, topic.path, i, topic.noun, topic.path,
		)
		p := filepath.Join(dir, fmt.Sprintf("s%03d.jsonl", i))
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		res, err := write.CatchUp(st, write.CatchUpRequest{
			JSONL: p, Project: "acme/api", Harness: harnessFor(i),
			SessionID: fmt.Sprintf("y%03d", i), Source: "import",
		})
		if err != nil {
			t.Fatal(err)
		}
		extracted += res.Extracted
	}
	elapsed := time.Since(start)
	t.Logf("catch-up %d sessions extracted=%d in %s (%.1f/s)", sessions, extracted, elapsed, float64(sessions)/elapsed.Seconds())
	if extracted < sessions {
		t.Fatalf("extract too thin: %d from %d sessions", extracted, sessions)
	}

	// Gold from catch-up should still retrieve after a pile of other sessions.
	out, err := retrieve.Ask(st, retrieve.Request{
		Project: "acme/api", Goal: "fix cron", Paths: []string{"src/jobs/cron.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsText(out, "cron") {
		t.Fatalf("year write corpus missed cron: %+v", out.Context)
	}
	other, err := retrieve.Ask(st, retrieve.Request{Project: "other/app", Question: "cron"})
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Context) != 0 {
		t.Fatalf("leaked across project: %+v", other.Context)
	}
}

func seedYearCorpus(t *testing.T, st *store.Store) int {
	t.Helper()
	for _, g := range yearGolds() {
		if _, err := st.WriteClaim(claim.Record{
			ID: g.id, Type: g.typ, Text: g.text, Paths: g.paths,
			CreatedAt: g.created, ProjectKey: "acme/api",
			Harness: "grok", SessionID: "gold", Source: "import", Status: "active",
		}); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2025, 8, 18, 12, 0, 0, 0, time.UTC)
	n := len(yearGolds())
	for d := 0; d < 365; d++ {
		day := start.AddDate(0, 0, d)
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			continue
		}
		// Two claims most weekdays, three on Mondays.
		k := 2
		if day.Weekday() == time.Monday {
			k = 3
		}
		for j := 0; j < k; j++ {
			topic := yearTopics[(d+j*7)%len(yearTopics)]
			typ := topic.typ
			if typ == "constraint" && j == 1 {
				typ = "state"
			}
			text := yearDecoyText(typ, topic.noun, topic.path, d, j)
			if _, err := st.WriteClaim(claim.Record{
				Type: typ, Text: text, Paths: []string{topic.path},
				CreatedAt: day.Add(time.Duration(j*3) * time.Hour).Format(time.RFC3339),
				ProjectKey: "acme/api", Harness: harnessFor(d + j),
				SessionID: fmt.Sprintf("d%03d", d), Source: "import", Status: "active",
			}); err != nil {
				t.Fatal(err)
			}
			n++
		}
	}
	// A second project must not pollute acme/api.
	for i := 0; i < 80; i++ {
		if _, err := st.WriteClaim(claim.Record{
			Type: "failed", Text: fmt.Sprintf("Marketing site deploy failed on page %d.", i),
			Paths: []string{"web/pages/home.tsx"}, CreatedAt: "2026-04-01T00:00:00Z",
			ProjectKey: "acme/web", Harness: "claude", SessionID: "web", Source: "import", Status: "active",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return st.CountActive()
}

func yearDecoyText(typ, noun, path string, d, j int) string {
	switch typ {
	case "failed":
		return fmt.Sprintf("The %s failed in %s on weekday %d pass %d.", noun, path, d, j)
	case "decision":
		return fmt.Sprintf("We decided to keep the %s in %s for weekday %d.", noun, path, d)
	case "constraint":
		return fmt.Sprintf("Always run the %s check in %s before merge.", noun, path)
	default:
		return fmt.Sprintf("Working on %s %s now implementing weekday %d.", path, noun, d)
	}
}

func harnessFor(i int) string {
	if i%3 == 0 {
		return "claude"
	}
	return "grok"
}

func summarizePacket(out retrieve.Response) string {
	var b strings.Builder
	for i, h := range out.Context {
		if i > 0 {
			b.WriteByte('|')
		}
		fmt.Fprintf(&b, "%s:%s", h.Type, trim(h.Text, 40))
	}
	return b.String()
}

func trim(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
