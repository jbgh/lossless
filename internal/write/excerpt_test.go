package write

import (
	"os"
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
	ref := got.TranscriptRef
	if ref.StartOffset != 0 || ref.EndOffset <= ref.StartOffset {
		t.Fatalf("span %+v", ref)
	}
}

func TestCatchUpSecondTurnCiteIsNotFirstTurn(t *testing.T) {
	st := tmpStore(t)
	p := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	first, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "sess-two"})
	if err != nil || first.Extracted == 0 {
		t.Fatalf("first %+v %v", first, err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"assistant","content":"Redis token bucket failed in src/middleware/auth.ts staging."}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	second, err := CatchUp(st, CatchUpRequest{JSONL: p, Project: "acme/api", SessionID: "sess-two", Source: "turn"})
	if err != nil || second.Extracted == 0 {
		t.Fatalf("second %+v %v", second, err)
	}
	got, ok := st.Get(second.IDs[0])
	if !ok || got.TranscriptRef == nil || got.TranscriptRef.StartOffset == 0 {
		t.Fatalf("second still offset 0: %+v", got.TranscriptRef)
	}
	view, ok := st.View(second.IDs[0])
	if !ok || !strings.Contains(view.Excerpt, "Redis") {
		t.Fatalf("second excerpt %q", view.Excerpt)
	}
	firstView, ok := st.View(first.IDs[0])
	if !ok || !strings.Contains(firstView.Excerpt, "jose") {
		t.Fatalf("first excerpt %q", firstView.Excerpt)
	}
}
