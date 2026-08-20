package write

import (
	"strings"
	"testing"
)

func TestAppendIncrementalAndConflict(t *testing.T) {
	st := tmpStore(t)
	chunk1 := `{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}` + "\n"
	a, err := Append(st, AppendRequest{
		Project: "acme/api", Harness: "grok", SessionID: "s-app", Client: "c1",
		PrevOff: 0, Body: []byte(chunk1),
	})
	if err != nil || a.Conflict || a.AcceptedThrough != int64(len(chunk1)) {
		t.Fatalf("%+v %v", a, err)
	}
	if a.Extracted == 0 {
		t.Fatal("expected extract")
	}
	// retry same prev → conflict, home is ahead
	b, err := Append(st, AppendRequest{
		Project: "acme/api", SessionID: "s-app", Client: "c1",
		PrevOff: 0, Body: []byte(chunk1),
	})
	if err != nil || !b.Conflict || b.AcceptedThrough != a.AcceptedThrough {
		t.Fatalf("conflict: %+v %v", b, err)
	}
	chunk2 := `{"type":"assistant","content":"Redis token bucket failed in staging."}` + "\n"
	c, err := Append(st, AppendRequest{
		Project: "acme/api", SessionID: "s-app", Client: "c1",
		PrevOff: a.AcceptedThrough, Body: []byte(chunk2),
	})
	if err != nil || c.Conflict || c.AcceptedThrough != a.AcceptedThrough+int64(len(chunk2)) {
		t.Fatalf("second: %+v %v", c, err)
	}
	claims, _ := st.ListActive("acme/api")
	var redisStart int64 = -1
	for _, rec := range claims {
		if strings.Contains(rec.Text, "Redis") {
			if rec.TranscriptRef == nil || rec.TranscriptRef.StartOffset == 0 {
				t.Fatalf("append second still offset 0: %+v", rec.TranscriptRef)
			}
			redisStart = rec.TranscriptRef.StartOffset
			view, ok := st.View(rec.ID)
			if !ok || !strings.Contains(view.Excerpt, "Redis") {
				t.Fatalf("append excerpt %q", view.Excerpt)
			}
		}
	}
	if redisStart < 0 {
		t.Fatal("redis claim missing")
	}
	empty, err := Append(st, AppendRequest{
		Project: "acme/api", SessionID: "s-app", Client: "c1",
		PrevOff: c.AcceptedThrough, Body: nil,
	})
	if err != nil || !empty.Noop {
		t.Fatalf("empty: %+v %v", empty, err)
	}
}

func TestAppendValidationAndRedact(t *testing.T) {
	st := tmpStore(t)
	if _, err := Append(st, AppendRequest{}); err == nil {
		t.Fatal("session")
	}
	if _, err := Append(st, AppendRequest{SessionID: "s"}); err == nil {
		t.Fatal("project")
	}
	body := `{"type":"assistant","content":"key AKIAIOSFODNN7EXAMPLE leaked"}` + "\n"
	res, err := Append(st, AppendRequest{
		Project: "Acme/API", SessionID: "s", Body: []byte(body),
	})
	if err != nil || res.Conflict {
		t.Fatal(res, err)
	}
	claims, _ := st.ListActive("acme/api")
	for _, c := range claims {
		if strings.Contains(c.Text, "AKIA") {
			t.Fatal("secret")
		}
	}
	partial, err := Append(st, AppendRequest{
		Project: "acme/api", SessionID: "s", Client: "default",
		PrevOff: res.AcceptedThrough, Body: []byte(`{"type":"user","content":"no nl`),
	})
	if err != nil || !partial.Noop {
		t.Fatalf("partial %+v %v", partial, err)
	}
}

func TestCatchUpMessagesAndTwoHarnesses(t *testing.T) {
	st := tmpStore(t)
	msgs := []map[string]any{
		{"role": "assistant", "content": "We decided to use jose, not jsonwebtoken, for Edge."},
	}
	res, err := CatchUp(st, CatchUpRequest{
		Project: "acme/api", Harness: "opencode", SessionID: "ses_1", Messages: msgs,
	})
	if err != nil || res.Copied == 0 {
		t.Fatal(res, err)
	}
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	res2, err := CatchUp(st, CatchUpRequest{
		JSONL: src, Project: "acme/api", Harness: "grok", SessionID: "grok-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawPath == res2.RawPath {
		t.Fatal("two harnesses should be two raw files")
	}
	claims, _ := st.ListActive("acme/api")
	// same claim_hash → one active
	active := 0
	for _, c := range claims {
		if strings.Contains(c.Text, "jose") {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("shared claim should supersede, active=%d %+v", active, claims)
	}
}

func TestCatchUpMessagesStableCursor(t *testing.T) {
	st := tmpStore(t)
	msgs := []map[string]any{
		{"role": "user", "content": "Always use jose please.", "zzz": 1, "aaa": 2},
		{"role": "assistant", "content": "We decided to use jose, not jsonwebtoken, for Edge."},
	}
	req := CatchUpRequest{Project: "acme/api", Harness: "opencode", SessionID: "ses_stable", Messages: msgs}
	a, err := CatchUp(st, req)
	if err != nil || a.Copied == 0 {
		t.Fatal(a, err)
	}
	b, err := CatchUp(st, req)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Noop {
		t.Fatalf("unstable dump broke cursor: %+v", b)
	}
}

func TestParsePiAndClaudeNoise(t *testing.T) {
	chunk := strings.Join([]string{
		`{"type":"session","version":3,"id":"u1","cwd":"/ws"}`,
		`{"type":"message","id":"a1","parentId":null,"message":{"role":"user","content":"Always use jose please."}}`,
		`{"type":"message","id":"a2","parentId":"a1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"We decided to use jose, not jsonwebtoken, for Edge."}]}}`,
		`{"type":"message","id":"a3","parentId":"a2","message":{"role":"toolResult","toolCallId":"t1","toolName":"bash","content":[{"type":"text","text":"ok"}],"isError":false}}`,
		`{"type":"compaction","summary":"old turns","tokensBefore":9}`,
		`{"type":"custom","customType":"x","data":{}}`,
		`{"type":"last-prompt","sessionId":"x"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Rate limiter lives in auth.ts"}]}}`,
	}, "\n") + "\n"
	msgs, _ := ParseJSONL(chunk, 0)
	var texts []string
	var skipped int
	for _, m := range msgs {
		if m.Skip {
			skipped++
			continue
		}
		texts = append(texts, m.Role+":"+m.Text)
	}
	if skipped < 3 {
		t.Fatalf("skipped=%d", skipped)
	}
	joined := strings.Join(texts, " | ")
	if !strings.Contains(joined, "jose") || !strings.Contains(joined, "tool:") {
		t.Fatalf("%q", joined)
	}
	if strings.Contains(joined, "old turns") || strings.Contains(joined, "hmm") {
		t.Fatalf("noise: %q", joined)
	}
}
