package store

import (
	"fmt"
	"testing"

	"lossless/internal/claim"
)

// Posting channels cap at 40 per key. Without ORDER BY the covering
// index returned the 40 oldest ids, hiding the newest faileds on a busy
// file from the job-1 channel.
func TestPostingChannelsReturnNewestFirst(t *testing.T) {
	st := orphanStore(t)
	for i := 0; i < 45; i++ {
		if _, err := st.WriteClaim(claim.Record{
			ID: fmt.Sprintf("REC%03d", i), Type: "failed", ProjectKey: "acme/api",
			Harness: "grok", SessionID: "s", Source: "turn", Status: "active",
			CreatedAt: fmt.Sprintf("2026-08-%02dT00:00:%02dZ", 1+i/60, i%60),
			Text:      fmt.Sprintf("lightbox swipe attempt %d failed in ui/Viewer.kt", i),
			Paths:     []string{"ui/Viewer.kt"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	has := func(ids []string, want string) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}
	byPath, err := st.IDsByPath("acme/api", []string{"ui/Viewer.kt"}, 40, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !has(byPath, "REC044") || has(byPath, "REC000") {
		t.Fatalf("path channel not newest-first: first=%v last=%v", byPath[0], byPath[len(byPath)-1])
	}
	bySym, err := st.TypeIDsOverlapping("acme/api", "failed", nil, []string{"lightbox"}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !has(bySym, "REC044") || has(bySym, "REC000") {
		t.Fatalf("typed symbol channel not newest-first: n=%d", len(bySym))
	}
}
