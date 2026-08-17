package write

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCatchUpConcurrentDifferentSessions(t *testing.T) {
	st := tmpStore(t)
	dir := t.TempDir()
	var wg sync.WaitGroup
	errc := make(chan error, 6)
	for i := 0; i < 6; i++ {
		sid := "sess-" + string(rune('A'+i))
		src := writeJSONL(t, dir, sid+".jsonl",
			`{"type":"assistant","content":"We decided to use `+sid+` for this harness."}`+"\n")
		wg.Add(1)
		go func(src, sid string) {
			defer wg.Done()
			_, err := CatchUp(st, CatchUpRequest{
				JSONL: src, Project: "acme/api", SessionID: sid, Harness: "grok", Source: "turn",
			})
			if err != nil {
				errc <- err
			}
		}(src, sid)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	active, err := st.ListActive("acme/api")
	if err != nil || len(active) < 6 {
		t.Fatalf("want >=6 claims, got %d err=%v", len(active), err)
	}
}

func TestCatchUpConcurrentSameSessionIdempotent(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	var wg sync.WaitGroup
	var copied int64
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := CatchUp(st, CatchUpRequest{
				JSONL: src, Project: "acme/api", SessionID: "same", Harness: "claude", Source: "turn",
			})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			copied += res.Copied
			mu.Unlock()
		}()
	}
	wg.Wait()
	raws, _ := filepath.Glob(filepath.Join(st.Root, "raw", "*", "*", "same.jsonl"))
	if len(raws) != 1 {
		t.Fatalf("raw files %v", raws)
	}
	body, err := os.ReadFile(raws[0])
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "jose"); n != 1 {
		t.Fatalf("duplicated raw %d times: %s", n, body)
	}
	if copied != int64(len(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")) {
		t.Fatalf("copied=%d", copied)
	}
}
