package harness

import (
	"encoding/json"
	"strings"
	"testing"
)

// Four of five live Claude hooks pointed at a dev-tree binary and
// install-hooks left them alone because any "hook-claude" counted as
// installed. A stale lossless entry is rewritten to the current exe,
// never duplicated; foreign hooks stay.
func TestMergeClaudeSettingsRetargetsStaleLosslessHook(t *testing.T) {
	existing := []byte(`{"hooks":{
	  "Stop":[{"hooks":[{"type":"command","command":"\"/Users/jay/developer/lossless/lossless\" hook-claude","timeout":2}]},
	          {"hooks":[{"type":"command","command":"echo hi"}]}],
	  "UserPromptSubmit":[{"hooks":[{"type":"command","command":"\"/Users/jay/.local/bin/lossless\" hook-claude","timeout":2}]}]
	}}`)
	got, err := MergeClaudeSettings(existing, "/Users/jay/.local/bin/lossless")
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "developer/lossless/lossless") {
		t.Fatalf("stale target kept:\n%s", s)
	}
	if strings.Count(s, "hook-claude") != 5 {
		t.Fatalf("want one lossless hook per event, got %d:\n%s", strings.Count(s, "hook-claude"), s)
	}
	if !strings.Contains(s, "echo hi") {
		t.Fatal("foreign hook dropped")
	}
	var root map[string]any
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatal(err)
	}
}

func TestMergeCodexHooksRetargetsStaleLosslessHook(t *testing.T) {
	existing := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"\"/old/lossless\" hook-codex","timeout":2}]}]}}`)
	got, err := MergeCodexHooks(existing, "/new/lossless")
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "/old/lossless") || strings.Count(s, "hook-codex") != 4 {
		t.Fatalf("%s", s)
	}
}
