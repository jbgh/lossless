package write

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lossless/internal/claim"
)

func TestExtractClassifiesAndDrops(t *testing.T) {
	ws := t.TempDir()
	rel := "src/middleware/auth.ts"
	if err := os.MkdirAll(filepath.Join(ws, "src/middleware"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, rel), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs := []Message{
		{Skip: true, Text: "We decided to use nothing here."},
		{Role: "assistant", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Offset: 1},
		{Role: "assistant", Text: "We decided to use jose, not jsonwebtoken, for Edge.", Offset: 2},
		{Role: "user", Text: "Always never log Authorization headers in src/middleware/auth.ts.", Offset: 3},
		{Role: "assistant", Text: "Working on billing invoices export next.", Offset: 4},
		{Role: "assistant", Text: "short", Offset: 5},
		{Role: "assistant", Text: "AKIAIOSFODNN7EXAMPLE failed to compile in staging.", Offset: 6},
		{Role: "assistant", Offset: 99, Text: "unrelated no classify words here at all really."},
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", WorkspaceRoot: ws, Harness: "grok", SessionID: "s", Source: "import"})
	types := map[string]bool{}
	for _, r := range got {
		types[r.Type] = true
		if strings.Contains(r.Text, "AKIA") {
			t.Fatal("secret claim")
		}
		if r.Type == "failed" && r.PathMtime[rel] == 0 {
			t.Fatalf("expected mtime on %s: %+v", rel, r)
		}
	}
	for _, need := range []string{"failed", "decision", "constraint", "state"} {
		if !types[need] {
			t.Fatalf("missing type %s in %+v", need, got)
		}
	}
}

func TestExtractCapsAtTwelve(t *testing.T) {
	var msgs []Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{
			Role:   "assistant",
			Text:   "This attempt failed because of unique reason number " + strings.Repeat("x", i) + " end.",
			Offset: int64(i),
		})
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "p", SessionID: "s"})
	if len(got) > 12 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestExtractDedupPrefersFailed(t *testing.T) {
	// same normalized text, different classify — failed should win
	text := "We decided this failed to compile for Edge runtime."
	got := Extract([]Message{
		{Role: "assistant", Text: text, Offset: 1},
	}, ExtractOpts{ProjectKey: "p"})
	if len(got) != 1 || got[0].Type != "failed" {
		t.Fatalf("%+v", got)
	}
}

func TestClassifyAndHelpers(t *testing.T) {
	if classify("it didn't work today at all", Message{}) != "failed" {
		t.Fatal("failed")
	}
	if classify("ok", Message{Error: true}) != "failed" {
		t.Fatal("error flag")
	}
	if classify("we will use jose instead of x", Message{}) != "decision" {
		t.Fatal("decision")
	}
	if classify("Always use jose for this", Message{Role: "user"}) != "constraint" {
		t.Fatal("constraint")
	}
	if classify("Always use jose for this", Message{Role: "assistant"}) != "" {
		t.Fatal("constraint is user-only")
	}
	if classify("Now implementing the limiter next", Message{}) != "state" {
		t.Fatal("state")
	}
	if classify("hello there friend", Message{}) != "" {
		t.Fatal("none")
	}

	if nearby(Message{Offset: 99}, []Message{{Offset: 1}}) != nil {
		t.Fatal("nearby miss")
	}
	paths := nearby(Message{Offset: 1}, []Message{
		{Offset: 1, Text: "see src/a.ts please"},
	})
	if len(paths) == 0 {
		t.Fatal("nearby hit")
	}

	long := make([]Message, 3)
	for i := range long {
		long[i] = Message{Text: strings.Repeat("z", 100)}
	}
	if n := tail(long, 40, 50); len(n) < 1 {
		t.Fatal("tail")
	}

	if len(uniq([]string{"", "a", "a", "b"})) != 2 {
		t.Fatal("uniq")
	}
	recs := []claim.Record{{Type: "state"}, {Type: "failed"}, {Type: "decision"}}
	sortByPri(recs)
	if recs[0].Type != "failed" || recs[1].Type != "decision" {
		t.Fatalf("%+v", recs)
	}

	r := makeRec("decision", "Use jose, not jsonwebtoken, for Edge.", []string{
		"a.ts", "b.ts", "c.ts", "d.ts", "e.ts", "f.ts", "g.ts", "h.ts", "i.ts",
	}, 0, ExtractOpts{})
	if len(r.Paths) != 8 {
		t.Fatal(len(r.Paths))
	}
}

func TestExtractManyPathsAndDropSensitive(t *testing.T) {
	var paths []string
	for i := 0; i < 10; i++ {
		paths = append(paths, "src/pkg/file"+string(rune('a'+i))+".ts")
	}
	text := "We decided to keep the limiter. See " + strings.Join(paths, " ") + "."
	got := Extract([]Message{{Role: "assistant", Text: text, Offset: 1}}, ExtractOpts{ProjectKey: "p"})
	if len(got) == 0 {
		t.Fatal("expected decision")
	}
	drop := Extract([]Message{{
		Role: "user", Text: "Always put SECRET in .env for local dev now.", Offset: 1,
	}}, ExtractOpts{ProjectKey: "p"})
	for _, r := range drop {
		if strings.Contains(r.Text, "SECRET") && len(r.Paths) > 0 {
			t.Fatalf("should drop: %+v", r)
		}
	}
}

func TestSplitSentences(t *testing.T) {
	got := splitSentences("One. Two!\nThree?")
	if len(got) < 3 {
		t.Fatal(got)
	}
	if len(splitSentences("no terminator")) != 1 {
		t.Fatal(splitSentences("no terminator"))
	}
}
