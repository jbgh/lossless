package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"lossless/internal/harness"
	"lossless/internal/store"
)

// A Grok session fires the Claude-scope hooks with transcript_path set
// to its updates.jsonl. hook-claude must store the Grok tape, not an
// event stream under harness=claude.
func TestRunHookClaudeRoutesGrokPayload(t *testing.T) {
	old := os.Stdin
	t.Cleanup(func() { os.Stdin = old })
	t.Setenv("LOSSLESS_SIDECAR", "off")
	home := t.TempDir()
	t.Setenv("LOSSLESS_HOME", home)
	root := t.TempDir()
	t.Setenv("GROK_HOME", filepath.Join(root, ".grok"))
	ws := t.TempDir()
	sid := "01a0557f-15b5-70a0-971c-efb01960423d"
	dir := filepath.Join(root, ".grok", "sessions", harness.EncodeCWD(ws), sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(dir, "chat_history.jsonl")
	upd := filepath.Join(dir, "updates.jsonl")
	if err := os.WriteFile(hist, []byte(`{"role":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(upd, []byte(`{"timestamp":1,"method":"session/update"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, `{"session_id":"`+sid+`","transcript_path":"`+upd+`","cwd":"`+ws+`","hook_event_name":"Stop"}`)
	_ = w.Close()
	os.Stdin = r
	if runHookClaude() != 0 {
		t.Fatal("hook must fail open")
	}
	st, err := store.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sess, _ := st.ListSessions()
	if len(sess) != 1 || sess[0].Harness != "grok" || sess[0].JSONL != hist {
		t.Fatalf("sessions %+v", sess)
	}
}
