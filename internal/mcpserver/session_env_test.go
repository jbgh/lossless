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
	t.Setenv("LOSSLESS_SESSION_ID", "")
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
	t.Setenv("LOSSLESS_SESSION_ID", "")
	t.Setenv("PI_SESSION_ID", "from-env")
	c := &captureBackend{}
	s := New(c)
	callTool(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","goal":"smoke","session_id":"from-caller"}}}`)
	if c.ask.SessionID != "from-caller" {
		t.Fatalf("ask session = %q, want caller id", c.ask.SessionID)
	}
}

// Without any harness env the session stays omitted (the project
// default), and env sentinels are scrubbed like caller sentinels.
func TestToolSessionAbsentStaysEmpty(t *testing.T) {
	t.Setenv("LOSSLESS_SESSION_ID", "")
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
