package store

import (
	"sync"
	"testing"

	"lossless/internal/claim"
)

func TestWriteClaimConcurrentSameHash(t *testing.T) {
	st := tmp(t)
	var wg sync.WaitGroup
	errc := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := st.WriteClaim(claim.Record{
				Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.",
				ProjectKey: "acme/api", Harness: "grok", SessionID: "s",
			})
			if err != nil {
				errc <- err
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	active, err := st.ListActive("acme/api")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range active {
		if r.Type == "decision" && r.Status == "active" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want 1 active jose decision, got %d (%+v)", n, active)
	}
}

func TestConcurrentCursorSessionExcerpt(t *testing.T) {
	st := tmp(t)
	var wg sync.WaitGroup
	errc := make(chan error, 24)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := "sess-" + string(rune('A'+i))
			jsonl := "/tmp/" + sid + ".jsonl"
			if err := st.SetCursor(jsonl, int64(i+1)); err != nil {
				errc <- err
				return
			}
			if err := st.UpsertSession(Session{
				JSONL: jsonl, SessionID: sid, Harness: "grok", Project: "acme/api",
			}); err != nil {
				errc <- err
				return
			}
			if err := st.WriteExcerpts("2026-08", []Excerpt{{
				SessionID: sid, ProjectKey: "acme/api",
				StartOffset: 0, EndOffset: 10, Text: "hello",
			}}); err != nil {
				errc <- err
				return
			}
			if _, err := st.WriteClaim(claim.Record{
				Type: "failed", Text: "Redis token bucket failed in src/middleware/auth.ts staging.",
				Paths:      []string{"src/middleware/auth.ts"},
				ProjectKey: "acme/api", Harness: "grok", SessionID: sid,
			}); err != nil {
				errc <- err
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
}

func TestWriteClaimConcurrentDifferentSessions(t *testing.T) {
	st := tmp(t)
	var wg sync.WaitGroup
	errc := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := st.WriteClaim(claim.Record{
				Type:       "decision",
				Text:       "Session " + string(rune('A'+i)) + " decided to keep hooks fail-open.",
				ProjectKey: "acme/api", Harness: "grok", SessionID: "sess-" + string(rune('A'+i)),
			})
			if err != nil {
				errc <- err
			}
		}(i)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
	active, err := st.ListActive("acme/api")
	if err != nil || len(active) != 8 {
		t.Fatalf("want 8, got %d err=%v", len(active), err)
	}
}
