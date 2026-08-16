package write

import (
	"strings"
	"testing"
)

func TestChunkExcerptsWindowsAndClips(t *testing.T) {
	msgs := []Message{
		{Role: "user", Text: "Always use jose for Edge please now.", Offset: 0},
		{Role: "assistant", Text: "We decided to use jose, not jsonwebtoken, for Edge.", Offset: 40},
	}
	xs := ChunkExcerpts("s1", "acme/api", msgs)
	if len(xs) == 0 {
		t.Fatal("empty")
	}
	joined := ""
	for _, x := range xs {
		if x.SessionID != "s1" || x.ID == "" {
			t.Fatalf("%+v", x)
		}
		joined += x.Text
	}
	if !strings.Contains(joined, "jose") || !strings.Contains(joined, "user:") {
		t.Fatal(joined)
	}

	huge := strings.Repeat("a", 2500)
	clipped := ChunkExcerpts("s1", "acme/api", []Message{{Role: "tool", Text: huge, Offset: 10}})
	if len(clipped) != 1 {
		t.Fatalf("len=%d", len(clipped))
	}
	if strings.Contains(clipped[0].Text, strings.Repeat("a", 2000)) {
		t.Fatal("tool body not clipped")
	}
	if !strings.Contains(clipped[0].Text, "…") {
		t.Fatal(clipped[0].Text)
	}
}

func TestChunkSkipsEmpty(t *testing.T) {
	if xs := ChunkExcerpts("s", "p", []Message{{Role: "user", Text: "   ", Offset: 0}}); xs != nil {
		t.Fatal(xs)
	}
}

func TestCatchUpWritesExcerptAndRef(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "sess-ex"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extracted == 0 {
		t.Fatal("no claim")
	}
	got, ok := st.Get(res.IDs[0])
	if !ok || got.TranscriptRef == nil || got.TranscriptRef.SessionID != "sess-ex" {
		t.Fatalf("ref %+v", got.TranscriptRef)
	}
	view, ok := st.View(res.IDs[0])
	if !ok || !strings.Contains(view.Excerpt, "jose") {
		t.Fatalf("excerpt %q", view.Excerpt)
	}
}
