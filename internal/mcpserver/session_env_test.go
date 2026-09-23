package mcpserver

import (
	"testing"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

type captureBackend struct {
	ask      retrieve.Request
	remember claim.Record
	getSess  string
}

func (c *captureBackend) Ask(req retrieve.Request) (retrieve.Response, error) {
	c.ask = req
	return retrieve.Response{}, nil
}

func (c *captureBackend) Remember(rec claim.Record) (write.CatchUpResult, error) {
	c.remember = rec
	return write.CatchUpResult{}, nil
}

func (c *captureBackend) Get(id, project, session string) (store.RecordView, bool, error) {
	c.getSess = session
	return store.RecordView{Record: claim.Record{Type: "state", Text: "t", ProjectKey: project}}, true, nil
}

func callTool(t *testing.T, s *Server, payload string) {
	t.Helper()
	if out := s.Handle([]byte(payload)); out == nil {
		t.Fatal("expected a tool response")
	}
}

// A caller-omitted session_id fills from the harness environment so the
// ask books the real session, not the project default. A sentinel like
// "default" counts as omitted and is replaced, never kept.
func TestToolSessionFillsFromEnv(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("PI_SESSION_ID", "01a0c725-37e2-7120-9e95-a0dbb87761fd")
	c := &captureBackend{}
	s := New(c)
	callTool(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","goal":"smoke"}}}`)
	if c.ask.SessionID != "01a0c725-37e2-7120-9e95-a0dbb87761fd" {
		t.Fatalf("ask session = %q, want env id", c.ask.SessionID)
	}
	callTool(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"remember","arguments":{"type":"decision","text":"We chose jose.","project":"acme/api"}}}`)
	if c.remember.SessionID != "01a0c725-37e2-7120-9e95-a0dbb87761fd" {
		t.Fatalf("remember session = %q, want env id", c.remember.SessionID)
	}
	callTool(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_record","arguments":{"id":"20260101000000000000000000000a","session_id":"default"}}}`)
	if c.getSess != "01a0c725-37e2-7120-9e95-a0dbb87761fd" {
		t.Fatalf("get_record session = %q, want env id", c.getSess)
	}
}

// An explicit session_id wins over the environment.
func TestToolSessionExplicitWins(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("PI_SESSION_ID", "01a0c725-37e2-7120-9e95-a0dbb87761fd")
	c := &captureBackend{}
	s := New(c)
	callTool(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","goal":"smoke","session_id":"ses_f3a5f4829ffeLHyxg9XP3Z7N8i"}}}`)
	if c.ask.SessionID != "ses_f3a5f4829ffeLHyxg9XP3Z7N8i" {
		t.Fatalf("ask session = %q, want caller id", c.ask.SessionID)
	}
}

// Without any harness env the session stays omitted (the project
// default), and env sentinels are scrubbed like caller sentinels.
func TestToolSessionAbsentStaysEmpty(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("PI_SESSION_ID", "default")
	c := &captureBackend{}
	s := New(c)
	callTool(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","goal":"smoke"}}}`)
	if c.ask.SessionID != "" {
		t.Fatalf("ask session = %q, want omitted", c.ask.SessionID)
	}
	t.Setenv("LOSSLESS_SESSION_ID", "explicit-wrapper")
	callTool(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","goal":"smoke"}}}`)
	if c.ask.SessionID != "explicit-wrapper" {
		t.Fatalf("ask session = %q, want LOSSLESS_SESSION_ID", c.ask.SessionID)
	}
}

func clearSessionEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"LOSSLESS_SESSION_ID", "PI_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION"} {
		t.Setenv(k, "")
	}
}

// Claude Code exports CLAUDE_CODE_SESSION_ID to the MCP servers it
// spawns, and every process under a Claude shell inherits it. Only a
// server whose parent is Claude Code itself takes it: a Pi or OpenCode
// started from a Claude shell must not book to the Claude session, and a
// nested `claude -p` (which inherits CLAUDE_CODE_CHILD_SESSION=1) still
// books its own.
func TestToolSessionFillsFromClaudeEnv(t *testing.T) {
	clearSessionEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "1c75c44b-8632-4581-b6c9-d87bf6c3670e")
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	c := &captureBackend{}
	s := New(c)
	for _, tc := range []struct{ parent, want string }{
		{"claude", "1c75c44b-8632-4581-b6c9-d87bf6c3670e"},
		{"/Users/x/.local/bin/claude", "1c75c44b-8632-4581-b6c9-d87bf6c3670e"},
		{"node", ""},
		{"pi", ""},
		{"opencode", ""},
	} {
		setParent(t, tc.parent)
		callTool(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","goal":"smoke"}}}`)
		if c.ask.SessionID != tc.want {
			t.Errorf("parent %q: ask session = %q, want %q", tc.parent, c.ask.SessionID, tc.want)
		}
	}
}

func setParent(t *testing.T, comm string) {
	t.Helper()
	old := parentComm
	parentComm = func() string { return comm }
	t.Cleanup(func() { parentComm = old })
}

// Caller ids the harness never issued (names a model made up, or a real
// id with a task suffix glued on) do not book a phantom session: a
// suffixed id trims to its UUID, anything else falls back to the env.
func TestToolSessionRejectsInventedIDs(t *testing.T) {
	cases := []struct{ sent, env, want string }{
		{"w22-ios", "", ""},
		{"adversarial-verify-fix-delivery", "1c75c44b-8632-4581-b6c9-d87bf6c3670e", "1c75c44b-8632-4581-b6c9-d87bf6c3670e"},
		{"session_01Ee6hNqTyELvcBJTfHhuioL", "", ""},
		{"01a0c72c-e2ef-7076-8f5b-be1aa6f6c190-68779", "", "01a0c72c-e2ef-7076-8f5b-be1aa6f6c190"},
		{"019ffbda-a0f1-7ea2-b21a-23f65798ecec.part2", "", "019ffbda-a0f1-7ea2-b21a-23f65798ecec"},
		{"01A0C72C-E2EF-7076-8F5B-BE1AA6F6C190", "", "01A0C72C-E2EF-7076-8F5B-BE1AA6F6C190"},
		{"ses_f3a5f4829ffeLHyxg9XP3Z7N8i", "", "ses_f3a5f4829ffeLHyxg9XP3Z7N8i"},
		{"agent-a003ed486c0a3cdb3", "", "agent-a003ed486c0a3cdb3"},
	}
	setParent(t, "claude")
	for _, tc := range cases {
		clearSessionEnv(t)
		t.Setenv("CLAUDE_CODE_SESSION_ID", tc.env)
		if got := fillSession(tc.sent); got != tc.want {
			t.Errorf("fillSession(%q) env=%q = %q, want %q", tc.sent, tc.env, got, tc.want)
		}
	}
}
