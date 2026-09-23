package harness

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Pi scrubs PI_SESSION_ID from MCP servers and OpenCode never shows the
// model its session id, so the models there omit session_id or invent
// one (w22-ios). The extension and plugin know the real id and stamp it
// on every lossless tool call, whatever the model sent.
func TestPluginSourcesStampSession(t *testing.T) {
	pi := PiExtensionSource("/x")
	if !strings.Contains(pi, `pi.on("tool_call"`) || !strings.Contains(pi, "stampLossless(") || !strings.Contains(pi, "getSessionId()") {
		t.Fatal("pi extension must stamp the session id on lossless tool calls:\n" + pi)
	}
	oc := OpenCodePluginSource()
	if !strings.Contains(oc, `"tool.execute.before"`) || !strings.Contains(oc, "stampLossless(") || !strings.Contains(oc, "input?.sessionID") {
		t.Fatal("opencode plugin must stamp the session id on lossless tool calls:\n" + oc)
	}
}

func TestStampLosslessBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	type tc struct {
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
		Sid   string         `json:"sid"`
	}
	cases := []tc{
		{"mcp__lossless", map[string]any{"tool": "ask", "args": map[string]any{"goal": "g"}}, "S"},
		{"mcp", map[string]any{"tool": "lossless_ask", "args": `{"goal":"g"}`}, "S"},
		{"mcp", map[string]any{"server": "lossless", "tool": "remember", "args": map[string]any{}}, "S"},
		{"lossless_get_record", map[string]any{"id": "x", "session_id": "default"}, "S"},
		{"lossless_ask", map[string]any{"session_id": "w22-ios"}, "S"},
		{"mcp__lossless__ask", map[string]any{"goal": "g"}, "S"},
		{"mcp__lossless", map[string]any{"tool": "ask"}, "S"},
		{"bash", map[string]any{"command": "lossless ask"}, "S"},
		{"mcp", map[string]any{"tool": "github_search_ask", "args": map[string]any{}}, "S"},
		{"lossless_ask", map[string]any{"goal": "g"}, ""},
	}
	in, _ := json.Marshal(cases)
	script := stampLosslessJS + `
const cases = JSON.parse(process.argv[1]);
console.log(JSON.stringify(cases.map((c) => { stampLossless(c.name, c.input, c.sid); return c.input; })));`
	out, err := exec.Command(node, "-e", script, string(in)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	sidOf := func(i int) any {
		m := got[i]
		switch a := m["args"].(type) {
		case map[string]any:
			return a["session_id"]
		case string:
			var p map[string]any
			_ = json.Unmarshal([]byte(a), &p)
			return p["session_id"]
		}
		return m["session_id"]
	}
	for i, want := range []any{"S", "S", "S", "S", "S", "S", "S", nil, nil, nil} {
		if g := sidOf(i); g != want {
			t.Errorf("case %d (%s): session_id = %v, want %v; input now %v", i, cases[i].Name, g, want, got[i])
		}
	}
	if got[0]["args"].(map[string]any)["goal"] != "g" {
		t.Errorf("stamp must keep the other args: %v", got[0])
	}
}
