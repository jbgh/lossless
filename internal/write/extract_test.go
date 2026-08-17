package write

import (
	"fmt"
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

func TestExtractKeepsDurableFromEarlyInLongSession(t *testing.T) {
	var msgs []Message
	msgs = append(msgs, Message{
		Role: "assistant", Offset: 1,
		Text: "Redis token bucket failed in src/middleware/auth.ts staging.",
	})
	for i := 0; i < 50; i++ {
		msgs = append(msgs, Message{
			Role: "assistant", Offset: int64(i + 2),
			Text: "Working on src/ui/Button.tsx hover pass " + strings.Repeat("x", i%3) + " now implementing next.",
		})
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", SessionID: "long"})
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("early failed dropped from long session: %+v", got)
	}
}

func TestExtractFailureInPathIsNotFailed(t *testing.T) {
	got := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "We decided to keep src/failure/handler.ts instead of Redis.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "failed" {
			t.Fatalf("path word failure: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "decision" && strings.Contains(r.Text, "handler.ts") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("wanted decision: %+v", got)
	}
}

func TestExtractKeepsDecisionAmongManyFaileds(t *testing.T) {
	var msgs []Message
	msgs = append(msgs, Message{
		Role: "assistant", Offset: 0,
		Text: "We decided to use jose, not jsonwebtoken, for Edge in src/middleware/auth.ts.",
	})
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{
			Role: "assistant", Offset: int64(i + 1),
			Text: fmt.Sprintf("Helper decoy %d failed in src/other/file%d.ts during compile.", i, i),
		})
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api", SessionID: "s"})
	ok := false
	nFail := 0
	for _, r := range got {
		if r.Type == "failed" {
			nFail++
		}
		if r.Type == "decision" && strings.Contains(r.Text, "jose") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("decision starved: %+v", got)
	}
	if nFail > 5 {
		t.Fatalf("extract failed flood %d: %+v", nFail, got)
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

func TestExtractErrorHandlingIsNotFailed(t *testing.T) {
	got := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "We decided to add error handling in src/api/errors.ts.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "failed" {
			t.Fatalf("error handling classified failed: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "decision" && strings.Contains(r.Text, "error handling") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("wanted decision: %+v", got)
	}
}

func TestExtractSkipsStatusFaileds(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "That background notification is just the local Android MediaUrlsTest run finishing (exit 0) from earlier while we fixed the CI unit-test failure."},
		{Role: "assistant", Offset: 2, Text: "Checking #3081 failure and re-pushing both."},
		{Role: "assistant", Offset: 3, Text: "If anything still looks off on device after pull-to-refresh / reinstall, say which of those four failed."},
		{Role: "assistant", Offset: 4, Text: "So the Retry button is live: it re-queues failed items and they land on the server/grid."},
		{Role: "assistant", Offset: 5, Text: "Who-reacted failed in preview."},
		{Role: "assistant", Offset: 6, Text: "**Upload Complete** sheet: **7 of 10 uploaded, 3 failed**, each with **Could not load this photo**, and a Retry that does nothing."},
		{Role: "assistant", Offset: 7, Text: "Redis token bucket failed in src/middleware/auth.ts staging."},
	}, ExtractOpts{ProjectKey: "memora/memora", SessionID: "s"})
	for _, r := range got {
		if strings.Contains(r.Text, "background notification") || strings.Contains(r.Text, "Checking #") || strings.Contains(r.Text, "which of those") || strings.Contains(r.Text, "re-queues failed") {
			t.Fatalf("status failed extracted: %+v", r)
		}
	}
	var who, upload, redis bool
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Who-reacted") {
			who = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "Upload Complete") {
			upload = true
		}
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			redis = true
		}
	}
	if !who || !upload || !redis {
		t.Fatalf("real faileds missed who=%v upload=%v redis=%v %+v", who, upload, redis, got)
	}
}

func TestExtractSkipsLiveSessionNoise(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "## Investigation: why those uploads failed"},
		{Role: "user", Offset: 2, Text: "why don't you pull up the simulator on an iphone 12 mini and see what it looks like"},
		{Role: "user", Offset: 3, Text: "i'll be stepping away so don't ask any questions just do what you think is correct."},
		{Role: "user", Offset: 4, Text: "- Don't change source"},
		{Role: "user", Offset: 5, Text: "- Don't delete data"},
		{Role: "assistant", Offset: 6, Text: "I'll check what we already decided, then install a real Grok/Claude skill as part of the product."},
		{Role: "assistant", Offset: 7, Text: "Who-reacted failed in preview."},
		{Role: "assistant", Offset: 8, Text: "Android Photos-like lightbox open uses a same-window hero overlay above NavHost instead of a hard cut."},
	}, ExtractOpts{ProjectKey: "memora/memora", SessionID: "s"})
	for _, r := range got {
		if strings.HasPrefix(strings.TrimSpace(r.Text), "#") {
			t.Fatalf("heading: %+v", r)
		}
		if strings.Contains(strings.ToLower(r.Text), "why don't you") {
			t.Fatalf("agent prompt: %+v", r)
		}
		if strings.Contains(strings.ToLower(r.Text), "don't ask") || strings.Contains(r.Text, "Don't change source") {
			t.Fatalf("session op: %+v", r)
		}
		if r.Type == "decision" && strings.HasPrefix(r.Text, "I'll check") {
			t.Fatalf("planning: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "decision" && strings.Contains(r.Text, "lightbox") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real lightbox decision missed: %+v", got)
	}
}

func TestExtractSkipsTablesAndMetaFailedTalk(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "| **Claims** | `owner/repo` | A Grok `failed` on `acme/api` is what Claude’s ask is supposed to see."},
		{Role: "assistant", Offset: 2, Text: "Force in the best failed-overlap (don't repeat burned work)."},
		{Role: "assistant", Offset: 3, Text: "Raising to 8 or 10 would make recall look better without making ranking better — and this live project already fills 5 with extract noise (markdown table rows classified as `failed`)."},
		{Role: "assistant", Offset: 4, Text: "Next I'll check whether this session is actually on tape."},
		{Role: "assistant", Offset: 5, Text: "Redis token bucket failed in src/middleware/auth.ts staging."},
	}, ExtractOpts{ProjectKey: "jbgh/lossless", SessionID: "s"})
	for _, r := range got {
		if strings.Contains(r.Text, "Claims") || strings.Contains(r.Text, "failed-overlap") || strings.Contains(r.Text, "extract noise") || strings.Contains(r.Text, "Next I'll") {
			t.Fatalf("meta extracted: %+v", r)
		}
	}
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real failed missed: %+v", got)
	}
}

func TestExtractSkipsHeadingsAndQuotedFixtures(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Offset: 1, Text: "**What you do next**"},
		{Role: "assistant", Offset: 2, Text: "- Redis limiter **failed** (twice) + warning: do not repeat"},
		{Role: "assistant", Offset: 3, Text: "> Redis token bucket failed in staging."},
		{Role: "assistant", Offset: 4, Text: "A markdown heading became a state, and quoting the Redis fixture became a failed."},
		{Role: "assistant", Offset: 5, Text: "Next I will check the store after compact."},
	}, ExtractOpts{ProjectKey: "jbgh/lossless", SessionID: "s"})
	for _, r := range got {
		if strings.Contains(r.Text, "What you do next") || strings.Contains(r.Text, "Redis limiter") || strings.Contains(r.Text, "quoting the") {
			t.Fatalf("noise extracted: %+v", r)
		}
		if r.Type == "state" && strings.Contains(r.Text, "Next I will") {
			t.Fatalf("bare next became state: %+v", r)
		}
	}
}

func TestExtractHedgingIsNotConstraint(t *testing.T) {
	got := Extract([]Message{{
		Role: "user", Offset: 1,
		Text: "I don't think we should use Mongo in src/db/client.ts.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "constraint" {
			t.Fatalf("hedge classified constraint: %+v", r)
		}
	}
}

func TestExtractSkipsToolDumps(t *testing.T) {
	got := Extract([]Message{{
		Role: "tool", Offset: 1,
		Text: "FAIL src/pkg/foo.test.ts: assertion failed at line 12\n" + strings.Repeat("stack ", 20),
	}}, ExtractOpts{ProjectKey: "acme/api"})
	if len(got) != 0 {
		t.Fatalf("tool dump became claims: %+v", got)
	}
	keep := Extract([]Message{{
		Role: "assistant", Offset: 2,
		Text: "The foo unit test failed in src/pkg/foo.test.ts.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	if len(keep) == 0 || keep[0].Type != "failed" {
		t.Fatalf("assistant failure missed: %+v", keep)
	}
}

func TestExtractQuestionIsNotDecisionOrConstraint(t *testing.T) {
	got := Extract([]Message{{
		Role: "user", Offset: 1,
		Text: "Should we use postgres in src/db/client.ts?",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "decision" || r.Type == "constraint" {
			t.Fatalf("question classified %s: %+v", r.Type, r)
		}
	}
}

func TestExtractDoesNotMarkHypotheticalAsFailed(t *testing.T) {
	got := Extract([]Message{{
		Role: "assistant",
		Text: "I was going to try jsonwebtoken unless we already rejected that.",
		Offset: 1,
	}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range got {
		if r.Type == "failed" {
			t.Fatalf("hypothetical classified failed: %+v", r)
		}
	}
	real := Extract([]Message{{
		Role: "assistant",
		Text: "We rejected Redis for rate limiting in src/middleware/auth.ts.",
		Offset: 2,
	}}, ExtractOpts{ProjectKey: "acme/api"})
	ok := false
	for _, r := range real {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("real rejection missed: %+v", real)
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
	if classify("We decided to revert the Redis limiter and keep jose.", Message{}) != "decision" {
		t.Fatal("decided-to-revert")
	}
	if classify("That's an exception to the rule we always use jose.", Message{}) != "" {
		t.Fatal("exception-to")
	}
	if classify("We decided to use jose, not jsonwebtoken, for Edge.", Message{Error: true}) != "decision" {
		t.Fatal("error flag must not stomp decision")
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
	paths := nearby(Message{Role: "assistant", Offset: 2}, []Message{
		{Role: "user", Offset: 1, Text: "see src/a.ts please"},
		{Role: "assistant", Offset: 2, Text: "Redis token bucket failed in staging."},
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
	list := splitSentences("1. Redis limiter **failed** (twice) + warning: do not repeat")
	if len(list) != 1 || !strings.HasPrefix(list[0], "1. ") {
		t.Fatalf("numbered list split: %#v", list)
	}
	still := splitSentences("Stopped at 12. Then we kept going on src/a.ts.")
	if len(still) < 2 {
		t.Fatalf("mid-sentence number should still split: %#v", still)
	}
}

func TestSplitDoesNotBreakOnFileExtension(t *testing.T) {
	text := "The limiter stays in-process in src/middleware/auth.ts instead of Redis."
	got := splitSentences(text)
	if len(got) != 1 {
		t.Fatalf("split on .ts: %#v", got)
	}
	recs := Extract([]Message{{Role: "assistant", Text: text, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
	for _, r := range recs {
		if strings.HasPrefix(strings.TrimSpace(r.Text), "ts ") {
			t.Fatalf("chopped claim: %q", r.Text)
		}
	}
	joined := ""
	for _, r := range recs {
		joined += r.Text
	}
	if !strings.Contains(joined, "instead of Redis") || !strings.Contains(joined, "auth.ts") {
		t.Fatalf("lost sentence: %+v", recs)
	}
}
