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

func TestWriteClaimConcurrentDifferentSessions(t *testing.T) {
	st := tmp(t)
	var wg sync.WaitGroup
	errc := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := st.WriteClaim(claim.Record{
				Type: "decision",
				Text: "Session " + string(rune('A'+i)) + " decided to keep hooks fail-open.",
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
