package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

func TestStressAsk10kDecoys(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	gold := claim.Record{
		ID: "GOLDFAIL", Type: "failed", ProjectKey: "acme/api",
		Text:      "Redis token bucket failed in src/middleware/auth.ts staging.",
		Paths:     []string{"src/middleware/auth.ts"},
		CreatedAt: "2026-01-01T00:00:00Z",
		Harness:   "grok", SessionID: "gold", Source: "import", Status: "active",
	}
	if _, err := st.WriteClaim(gold); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		rec := claim.Record{
			Type: "state", ProjectKey: "acme/api",
			Text:      fmt.Sprintf("Working on src/decoy/file%d.ts helper number %d for invoices.", i, i),
			Paths:     []string{fmt.Sprintf("src/decoy/file%d.ts", i)},
			CreatedAt: fmt.Sprintf("2026-07-%02dT00:00:00Z", 1+i%28),
			Harness:   "grok", SessionID: "decoy", Source: "import", Status: "active",
		}
		if _, err := st.WriteClaim(rec); err != nil {
			t.Fatal(err)
		}
	}

	eng := retrieve.Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	}}
	req := retrieve.Request{
		Project: "acme/api", Goal: "add rate limiting",
		Paths: []string{"src/middleware/auth.ts"},
	}
	const n = 30
	times := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		out, err := eng.Ask(req)
		times[i] = time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if !containsText(out, "Redis") {
				t.Fatalf("gold missed among 10k decoys: %+v", out.Context)
			}
			if !hasAnyWarn(out, "failed") {
				t.Fatalf("no failed warning: %v", out.Warnings)
			}
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	p50 := times[n/2]
	p95 := times[(n*95)/100]
	t.Logf("ask 10k decoys n=%d p50=%s p95=%s max=%s", n, p50, p95, times[n-1])
	if p50 > 500*time.Millisecond {
		t.Fatalf("p50 %s exceeds 500ms ask budget", p50)
	}
	if p95 > 2*time.Second {
		t.Fatalf("p95 %s exceeds 2s ask budget", p95)
	}
}

func TestStressConcurrentAsk(t *testing.T) {
	st := seed(t, false)
	eng := retrieve.Engine{Store: st, Now: func() time.Time {
		return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	}}
	var wg sync.WaitGroup
	errc := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := eng.Ask(retrieve.Request{
				Project: "acme/api", Goal: "add rate limiting",
				Paths: []string{"src/middleware/auth.ts"},
			})
			if err != nil {
				errc <- err
				return
			}
			if !containsText(out, "Redis") {
				errc <- fmt.Errorf("miss: %+v", out.Context)
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
}

func TestStressManySessionCatchUp(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := t.TempDir()
	const n = 80
	start := time.Now()
	for i := 0; i < n; i++ {
		p := testdataWrite(t, dir, fmt.Sprintf("s%d.jsonl", i),
			fmt.Sprintf("{\"type\":\"assistant\",\"content\":\"Helper %d failed in src/mod/file%d.ts during compile.\"}\n", i, i))
		if _, err := write.CatchUp(st, write.CatchUpRequest{
			JSONL: p, Project: "acme/api", Harness: "grok",
			SessionID: fmt.Sprintf("s%d", i), Source: "import",
		}); err != nil {
			t.Fatal(err)
		}
	}
	elapsed := time.Since(start)
	t.Logf("catch-up %d sessions in %s (%.1f/s)", n, elapsed, float64(n)/elapsed.Seconds())
	if st.CountActive() < n/2 {
		t.Fatalf("extracted too few: %d", st.CountActive())
	}
	if elapsed > 15*time.Second {
		t.Fatalf("catch-up too slow: %s", elapsed)
	}

	out, err := retrieve.Ask(st, retrieve.Request{
		Project: "acme/api", Question: "file17 compile",
		Paths: []string{"src/mod/file17.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsText(out, "17") {
		t.Fatalf("did not retrieve session 17: %+v", out.Context)
	}
}

func testdataWrite(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func containsText(out retrieve.Response, needle string) bool {
	for _, h := range out.Context {
		if strings.Contains(h.Text, needle) {
			return true
		}
	}
	return false
}
