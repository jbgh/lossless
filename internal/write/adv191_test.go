package write

import (
	"encoding/json"
	"strings"
	"testing"

	"lossless/internal/claim"
)

func recBlob(got []claim.Record) string {
	var blob string
	for _, r := range got {
		blob += r.Type + ":" + r.Text
		if len(r.Paths) > 0 {
			blob += " paths=" + strings.Join(r.Paths, ",")
		}
		blob += "\n"
	}
	return blob
}

func liftedFailed(got []claim.Record, sub string) bool {
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, sub) {
			return true
		}
	}
	return false
}

func TestAdv191LeadingFenceKeepsLeftover(t *testing.T) {
	body := "```json\n" +
		`{"asked":true,"findings":[{"issue":"Settings opens Family instead of the child list.","severity":"high"}],"ok":true}` +
		"\n```\nI'll stick with JWT next."
	got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
	blob := recBlob(got)
	if !strings.Contains(blob, "JWT") {
		t.Fatalf("lost leftover after leading fence: %s", blob)
	}
	if !liftedFailed(got, "Settings opens Family") {
		t.Fatalf("issue not lifted as failed: %s", blob)
	}
}

func TestAdv191LeftoverBeforeAndAfterFence(t *testing.T) {
	body := "I'll stick with JWT next.\n```json\n" +
		`{"asked":true,"findings":[{"issue":"Settings opens Family instead of the child list.","severity":"high"}],"ok":true}` +
		"\n```\nRedis token bucket failed in src/tmp/limiter.ts staging."
	got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
	blob := recBlob(got)
	if !strings.Contains(blob, "JWT") {
		t.Fatalf("lost leftover before fence: %s", blob)
	}
	if !strings.Contains(blob, "Redis") {
		t.Fatalf("lost leftover after fence: %s", blob)
	}
	if !liftedFailed(got, "Settings opens Family") {
		t.Fatalf("issue not lifted as failed: %s", blob)
	}
}

func TestAdv191AskedFalseDoesNotLift(t *testing.T) {
	body := `{"asked":false,"findings":[{"issue":"Consider extracting a helper.","severity":"high"}]}` +
		"\nWe decided to use jose, not jsonwebtoken, for Edge."
	got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
	blob := recBlob(got)
	for _, r := range got {
		if strings.Contains(r.Text, "extracting a helper") {
			t.Fatalf("asked=false lifted: %+v", r)
		}
	}
	if !strings.Contains(blob, "jose") {
		t.Fatalf("asked=false dropped leftover: %s", blob)
	}
}

func TestAdv191MissingAskedDoesNotLift(t *testing.T) {
	body := `{"findings":[{"issue":"Consider extracting a helper.","severity":"high"}]}` +
		"\nWe decided to use jose, not jsonwebtoken, for Edge."
	got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
	blob := recBlob(got)
	for _, r := range got {
		if strings.Contains(r.Text, "extracting a helper") {
			t.Fatalf("missing asked lifted: %+v", r)
		}
	}
	if !strings.Contains(blob, "jose") {
		t.Fatalf("missing asked dropped leftover: %s", blob)
	}
}

func TestAdv191AskedCoercionDocumentsActual(t *testing.T) {
	issue := "Settings opens Family instead of the child list."
	lift := func(askedJSON string) bool {
		body := `{"asked":` + askedJSON + `,"findings":[{"issue":"` + issue + `","severity":"high"}]}`
		got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
		return liftedFailed(got, "Settings opens Family")
	}
	if !lift("true") {
		t.Fatal(`asked:true bool did not lift`)
	}
	// askedTrue: string "true"/"yes"/"1" and nonzero JSON numbers count as asked.
	if !lift(`"true"`) {
		t.Fatal(`asked:"true" string did not lift (askedTrue string branch)`)
	}
	if !lift("1") {
		t.Fatal("asked:1 did not lift (askedTrue treats nonzero float64 as true)")
	}
	if lift("false") {
		t.Fatal("asked:false lifted")
	}
	if lift(`"false"`) {
		t.Fatal(`asked:"false" string lifted`)
	}
	if lift("0") {
		t.Fatal("asked:0 lifted")
	}
}

func TestAdv191RedisLeftoverNextToFindings(t *testing.T) {
	findings := `{"asked":true,"findings":[{"issue":"Settings opens Family instead of the child list.","severity":"high"}]}`
	redis := "Redis token bucket failed in src/tmp/limiter.ts staging."
	bodies := []string{
		findings + "\n" + redis,
		redis + "\n" + findings,
		"```json\n" + findings + "\n```\n" + redis,
	}
	for _, body := range bodies {
		got := Extract([]Message{{Role: "assistant", Text: body, Offset: 1}}, ExtractOpts{ProjectKey: "acme/api"})
		blob := recBlob(got)
		if !liftedFailed(got, "Settings opens Family") {
			t.Fatalf("issue not lifted: %s", blob)
		}
		ok := false
		for _, r := range got {
			if r.Type == "failed" && strings.Contains(r.Text, "Redis") && len(r.Paths) > 0 {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("redis leftover lost next to findings: %s", blob)
		}
	}
}

func TestAdv191ParseCompactsFindingsPast32k(t *testing.T) {
	issue := "Settings opens Family instead of the child list."
	inner := `{"asked":true,"pad":"` + strings.Repeat("x", 40*1024) + `","findings":[{"issue":"` + issue + `","severity":"high"}],"ok":true}`
	raw, err := json.Marshal(map[string]any{"type": "assistant", "content": inner})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := ParseJSONL(string(raw)+"\n", 0)
	if len(msgs) != 1 || msgs[0].Skip {
		t.Fatalf("%+v", msgs)
	}
	if len(msgs[0].Text) > 32<<10+64 {
		t.Fatalf("did not compact: %d", len(msgs[0].Text))
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api"})
	if !liftedFailed(got, "Settings opens Family") {
		t.Fatalf("40k pad ate findings: %+v text=%q", got, msgs[0].Text)
	}
}

func TestAdv191ParseFenced40kKeepsLeftoverAndIssue(t *testing.T) {
	issue := "Settings opens Family instead of the child list."
	inner := "```json\n" +
		`{"asked":true,"pad":"` + strings.Repeat("x", 40*1024) + `","findings":[{"issue":"` + issue + `","severity":"high"}],"ok":true}` +
		"\n```\nI'll stick with JWT next."
	raw, err := json.Marshal(map[string]any{"type": "assistant", "content": inner})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := ParseJSONL(string(raw)+"\n", 0)
	if len(msgs) != 1 || msgs[0].Skip {
		t.Fatalf("%+v", msgs)
	}
	if len(msgs[0].Text) > 32<<10+64 {
		t.Fatalf("did not compact: %d", len(msgs[0].Text))
	}
	if !strings.Contains(msgs[0].Text, issue) || !strings.Contains(msgs[0].Text, "JWT") {
		t.Fatalf("compact dropped issue or leftover: %q", msgs[0].Text)
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api"})
	blob := recBlob(got)
	if !liftedFailed(got, "Settings opens Family") {
		t.Fatalf("fenced 40k pad ate findings: %s text=%q", blob, msgs[0].Text)
	}
	if !strings.Contains(blob, "JWT") {
		t.Fatalf("fenced 40k pad ate leftover: %s", blob)
	}
}

func TestAdv191SrcTmpFailedStillStores(t *testing.T) {
	got := Extract([]Message{{
		Role: "assistant", Offset: 1,
		Text: "Redis token bucket failed in src/tmp/limiter.ts staging.",
	}}, ExtractOpts{ProjectKey: "acme/api"})
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Redis") {
			for _, p := range r.Paths {
				if p == "src/tmp/limiter.ts" || strings.HasSuffix(p, "limiter.ts") {
					ok = true
				}
			}
		}
	}
	if !ok {
		t.Fatalf("src/tmp/limiter.ts did not ground failed: %+v", got)
	}
}

func TestAdv191InstructionChromeDoesNotStore(t *testing.T) {
	got := Extract([]Message{
		{Role: "assistant", Text: "READ-ONLY: do not push, edit, or merge.", Offset: 1},
		{Role: "assistant", Text: "Now I understand the failure in src/middleware/auth.ts.", Offset: 2},
		{Role: "assistant", Text: "Return APPROVE or REQUEST_CHANGES with findings ranked by severity.", Offset: 3},
		{Role: "assistant", Text: "Lossless will not abort a child if ask is missing.", Offset: 4},
		{Role: "assistant", Text: "Lossless ask returned the USB-only / cream-hole.", Offset: 5},
		{Role: "assistant", Text: "Redis token bucket failed in src/middleware/auth.ts staging.", Offset: 6},
	}, ExtractOpts{ProjectKey: "acme/api"})
	if len(got) != 1 || got[0].Type != "failed" || !strings.Contains(got[0].Text, "Redis") {
		t.Fatalf("%+v", got)
	}
}
