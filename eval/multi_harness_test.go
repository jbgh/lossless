package eval

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

func TestMultiHarnessConcurrentReconcile(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	dir := t.TempDir()
	type job struct {
		name, harness, project, sid, body string
	}
	jobs := []job{
		{"grok.jsonl", "grok", "acme/api", "sess-grok",
			`{"type":"user","content":"Add rate limiting to src/middleware/auth.ts."}` + "\n" +
				`{"type":"assistant","content":"Redis token bucket failed in src/middleware/auth.ts staging."}` + "\n" +
				`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}` + "\n"},
		{"claude.jsonl", "claude", "acme/api", "sess-claude",
			`{"type":"assistant","content":"We decided to keep the limiter in-process in src/middleware/auth.ts."}` + "\n"},
		{"codex.jsonl", "codex", "acme/api", "sess-codex",
			`{"type":"assistant","content":"Never log Authorization headers in src/middleware/auth.ts."}` + "\n"},
		{"pi.jsonl", "pi", "other/shop", "sess-pi",
			`{"type":"assistant","content":"Stripe invoice webhook failed in src/billing/export.ts."}` + "\n"},
	}

	var wg sync.WaitGroup
	errc := make(chan error, len(jobs))
	start := time.Now()
	for _, j := range jobs {
		j := j
		p := filepath.Join(dir, j.name)
		if err := os.WriteFile(p, []byte(j.body), 0o644); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := write.CatchUp(st, write.CatchUpRequest{
				JSONL: p, Project: j.project, Harness: j.harness,
				SessionID: j.sid, Source: "turn",
			})
			if err != nil {
				errc <- err
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	writeMs := time.Since(start)

	eng := retrieve.Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	}}

	type askCase struct {
		name, project, sid, goal, path string
		want, never                    []string
	}
	cases := []askCase{
		{
			name: "claude sees grok failed+jose",
			project: "acme/api", sid: "sess-claude",
			goal: "add rate limiting", path: "src/middleware/auth.ts",
			want: []string{"Redis", "jose"},
		},
		{
			name: "grok does not see shop stripe",
			project: "acme/api", sid: "sess-grok",
			goal: "pick a jwt library", path: "src/middleware/auth.ts",
			want: []string{"jose"}, never: []string{"Stripe"},
		},
		{
			name: "shop isolated from acme",
			project: "other/shop", sid: "sess-pi",
			goal: "fix invoice webhook", path: "src/billing/export.ts",
			want: []string{"Stripe"}, never: []string{"jose", "Redis"},
		},
	}

	askStart := time.Now()
	var askWg sync.WaitGroup
	var mu sync.Mutex
	var fails []string
	for _, c := range cases {
		c := c
		askWg.Add(1)
		go func() {
			defer askWg.Done()
			out, err := eng.Ask(retrieve.Request{
				Project: c.project, SessionID: c.sid, Goal: c.goal,
				Paths: []string{c.path},
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fails = append(fails, c.name+": "+err.Error())
				return
			}
			blob := textsOfHits(out)
			for _, w := range c.want {
				if !strings.Contains(blob, w) {
					fails = append(fails, c.name+": missing "+w+" in "+blob)
				}
			}
			for _, n := range c.never {
				if strings.Contains(blob, n) {
					fails = append(fails, c.name+": leaked "+n)
				}
			}
		}()
	}
	askWg.Wait()
	askMs := time.Since(askStart)
	if len(fails) > 0 {
		t.Fatal(strings.Join(fails, "\n"))
	}
	if writeMs > 2*time.Second {
		t.Fatalf("concurrent catch-up too slow: %s", writeMs)
	}
	if askMs > time.Second {
		t.Fatalf("concurrent ask too slow: %s", askMs)
	}
}
