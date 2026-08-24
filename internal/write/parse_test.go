package write

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseJSONLEmptyAndPartial(t *testing.T) {
	if msgs, n := ParseJSONL("", 0); msgs != nil || n != 0 {
		t.Fatal(msgs, n)
	}
	if msgs, n := ParseJSONL("no-newline", 0); msgs != nil || n != 0 {
		t.Fatal(msgs, n)
	}
	msgs, n := ParseJSONL("{\"type\":\"user\",\"content\":\"hi there friend\"}\nincomplete", 10)
	if n == 0 || len(msgs) != 1 {
		t.Fatal(msgs, n)
	}
}

func TestParseJSONLSkipsAndRoles(t *testing.T) {
	chunk := strings.Join([]string{
		``,
		`not-json`,
		`{"_redacted":true}`,
		`{"type":"compaction","content":"compacted"}`,
		`{"type":"system","content":"sys"}`,
		`{"type":"reasoning","content":"think"}`,
		`{"type":"backend_tool_call","content":"x"}`,
		`{"synthetic_reason":"dump","content":"skills"}`,
		`{"role":"human","content":"Always use jose please."}`,
		`{"role":"model","text":"We decided to use jose."}`,
		`{"message":{"role":"assistant","content":"going with jose"}}`,
		`{"type":"user","content":[{"text":"part-a"}," part-b"]}`,
		`{"type":"tool_result","content":"file body here"}`,
		`{"type":"assistant","content":"failed to compile","error":true}`,
		`{"type":"user","content":"<system-reminder>ignore"}`,
		`{"type":"user","content":"<user_info>os"}`,
		`{"role":"user","content":""}`,
	}, "\n") + "\n"
	msgs, _ := ParseJSONL(chunk, 0)
	var roles []string
	var skipped, errors int
	for _, m := range msgs {
		if m.Skip {
			skipped++
			continue
		}
		roles = append(roles, m.Role)
		if m.Error {
			errors++
		}
	}
	if skipped < 5 {
		t.Fatalf("skipped=%d msgs=%+v", skipped, msgs)
	}
	if errors != 1 {
		t.Fatalf("errors=%d", errors)
	}
	joined := strings.Join(roles, ",")
	if !strings.Contains(joined, "user") || !strings.Contains(joined, "assistant") || !strings.Contains(joined, "tool") {
		t.Fatalf("roles %v", roles)
	}
	bom := "\ufeff" + `{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}` + "\n"
	msgs, _ = ParseJSONL(bom, 0)
	if len(msgs) != 1 || msgs[0].Skip || !strings.Contains(msgs[0].Text, "jose") {
		t.Fatalf("BOM: %+v", msgs)
	}
}

func TestParseJSONLCompactionFlag(t *testing.T) {
	msgs, _ := ParseJSONL(`{"type":"compaction","content":"x"}`+"\n"+`{"type":"assistant","content":"ok go"}`+"\n", 0)
	n := 0
	for _, m := range msgs {
		if m.Compact {
			n++
			if !m.Skip {
				t.Fatal("compaction must skip extract")
			}
		}
	}
	if n != 1 {
		t.Fatalf("compact=%d %+v", n, msgs)
	}
}

func TestParseJSONLClipsLongAndHugeSkip(t *testing.T) {
	long := strings.Repeat("a", 2500)
	msgs, _ := ParseJSONL(`{"role":"user","content":"`+long+`"}`+"\n", 0)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "…") {
		t.Fatalf("clip: %+v", msgs)
	}
	huge := strings.Repeat("b", 8001)
	msgs, _ = ParseJSONL(`{"role":"user","content":"`+huge+`"}`+"\n", 0)
	if len(msgs) != 1 || msgs[0].Skip || !strings.Contains(msgs[0].Text, "…") {
		t.Fatalf("huge should clip not skip: %+v", msgs)
	}
}

func TestParseCodexRollout(t *testing.T) {
	chunk := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"s1","cwd":"/ws"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","input":1}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"Always use jose for Edge please."}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"We decided to use jose, not jsonwebtoken, for Edge."}}`,
		`{"type":"response_item","payload":{"type":"reasoning","summary":"think"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"ask","call_id":"c1","arguments":"{}"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"{\"context\":[],\"warnings\":[\"failed\"],\"tokens\":1,\"project\":\"acme/api\"}"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Rate limiter lives in src/middleware/auth.ts as an in-process token bucket."}]}}`,
		`{"type":"turn_context","payload":{}}`,
	}, "\n") + "\n"
	msgs, _ := ParseJSONL(chunk, 0)
	var texts []string
	for _, m := range msgs {
		if m.Skip || strings.TrimSpace(m.Text) == "" {
			continue
		}
		texts = append(texts, m.Text)
	}
	joined := strings.Join(texts, " | ")
	if strings.Contains(joined, "warnings") || strings.Contains(joined, "token_count") {
		t.Fatalf("noise leaked: %q", joined)
	}
	if !strings.Contains(joined, "Always use jose") || !strings.Contains(joined, "not jsonwebtoken") || !strings.Contains(joined, "Rate limiter") {
		t.Fatalf("lost content: %q", joined)
	}
}

func TestCatchUpCodexExtractsClaims(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "rollout.jsonl",
		`{"type":"session_meta","payload":{"id":"s","cwd":"/ws"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"agent_message","message":"We decided to use jose, not jsonwebtoken, for Edge."}}`+"\n")
	res, err := CatchUp(st, CatchUpRequest{JSONL: src, Project: "acme/api", Harness: "codex", SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extracted == 0 {
		t.Fatal("expected claim")
	}
	claims, _ := st.ListActive("acme/api")
	found := false
	for _, c := range claims {
		if strings.Contains(c.Text, "jose") {
			found = true
		}
	}
	if !found {
		t.Fatalf("%+v", claims)
	}
}

func TestParseJSONLSkipsOwnAskIO(t *testing.T) {
	chunk := strings.Join([]string{
		`{"type":"assistant","content":"","tool_calls":[{"id":"c1","name":"lossless__ask","arguments":"{}"}]}`,
		`{"type":"tool_result","tool_call_id":"c1","content":"{\"context\":[],\"warnings\":[\"A prior attempt at this goal failed (see X).\"],\"tokens\":1,\"project\":\"acme/api\"}"}`,
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"u1","name":"remember","input":{}},{"type":"text","text":"We decided to keep the limiter."}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"u1","content":"{\"extracted\":1,\"ids\":[\"01\"]}"}]}}`,
		`{"type":"assistant","content":"","tool_calls":[{"id":"g1","name":"get_record","arguments":"{\"id\":\"01JFAIL\"}"}]}`,
		`{"type":"tool_result","tool_call_id":"g1","content":"{\"id\":\"01JFAIL\",\"type\":\"failed\",\"text\":\"Redis token bucket failed in staging.\"}"}`,
		`{"type":"tool_result","tool_call_id":"orphan","content":"{\"copied\":12,\"extracted\":1,\"ids\":[\"01\"]}"}`,
	}, "\n") + "\n"
	msgs, _ := ParseJSONL(chunk, 0)
	var texts []string
	for _, m := range msgs {
		if m.Skip {
			continue
		}
		texts = append(texts, m.Text)
	}
	joined := strings.Join(texts, " | ")
	if strings.Contains(joined, "prior attempt") || strings.Contains(joined, "extracted") || strings.Contains(joined, "01JFAIL") {
		t.Fatalf("own packet leaked: %q", joined)
	}
	if !strings.Contains(joined, "jose") || !strings.Contains(joined, "limiter") {
		t.Fatalf("lost real claims: %q", joined)
	}
}

func TestParseJSONLTypeAsRoleAndTextFallback(t *testing.T) {
	chunk := `{"type":"user","text":"Always use jose please now."}` + "\n" +
		`{"type":"assistant","content":"","text":"We decided to use jose instead."}` + "\n"
	msgs, _ := ParseJSONL(chunk, 0)
	if len(msgs) != 2 {
		t.Fatalf("%+v", msgs)
	}
}

func TestMapRoleAndFlatten(t *testing.T) {
	if mapRole("human") != "user" || mapRole("model") != "assistant" || mapRole("tool_result") != "tool" {
		t.Fatal("aliases")
	}
	if mapRole("") != "other" || mapRole("system") != "system" {
		t.Fatal("default")
	}
	if flatten("x") != "x" || flatten(1) != "" {
		t.Fatal("flatten scalar")
	}
	got := flatten([]any{"a", map[string]any{"text": "b"}, map[string]any{"no": 1}, 3})
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Fatal(got)
	}
}

func TestParseCodexFunctionOutputAndItemWrap(t *testing.T) {
	chunk := strings.Join([]string{
		`{"item":{"type":"response_item","payload":{"type":"function_call_output","call_id":"x","output":"compiled ok with jose"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","id":"y","output":""}}`,
		`{"type":"response_item","payload":{"type":"web_search_call"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","content":[{"type":"output_text","text":"We decided to keep the in-process limiter."}]}}`,
	}, "\n") + "\n"
	msgs, _ := ParseJSONL(chunk, 0)
	var texts []string
	for _, m := range msgs {
		if m.Skip || strings.TrimSpace(m.Text) == "" {
			continue
		}
		texts = append(texts, m.Text)
	}
	joined := strings.Join(texts, " | ")
	if !strings.Contains(joined, "jose") || !strings.Contains(joined, "limiter") {
		t.Fatalf("%q", joined)
	}
}

func TestClip(t *testing.T) {
	if clip("short") != "short" {
		t.Fatal("short")
	}
	s := strings.Repeat("z", 2001)
	c := clip(s)
	if len(c) >= len(s) || !strings.Contains(c, "…") {
		t.Fatal(len(c))
	}
}

func TestParseKeepsWorkflowFindingsJSON(t *testing.T) {
	issue := "Settings opens Family instead of the child list."
	inner := `{"asked":true,"pad":"` + strings.Repeat("x", 2500) + `","findings":[{"issue":"` + issue + `","severity":"high"}],"ok":true}`
	if len(inner) <= 2000 {
		t.Fatalf("need longer than clip: %d", len(inner))
	}
	raw, err := json.Marshal(map[string]any{"type": "assistant", "content": inner})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := ParseJSONL(string(raw)+"\n", 0)
	if len(msgs) != 1 || msgs[0].Skip {
		t.Fatalf("%+v", msgs)
	}
	if !strings.Contains(msgs[0].Text, issue) || strings.Contains(msgs[0].Text, "…") {
		t.Fatalf("clipped workflow json: %d %q", len(msgs[0].Text), msgs[0].Text[:min(80, len(msgs[0].Text))])
	}
	got := Extract(msgs, ExtractOpts{ProjectKey: "acme/api"})
	ok := false
	for _, r := range got {
		if r.Type == "failed" && strings.Contains(r.Text, "Settings opens Family") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("issue not lifted: %+v", got)
	}
}
