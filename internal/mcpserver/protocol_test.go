package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/store"
	"lossless/internal/write"
)

func TestInitializeListAndAsk(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := write.Remember(st, claim.Record{
		Type: "failed", Text: "Redis token bucket failed in staging yesterday.",
		ProjectKey: "acme/api", Paths: []string{"src/middleware/auth.ts"},
	}); err != nil {
		t.Fatal(err)
	}
	s := New(Local{Store: st})

	init := s.Handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	var initResp rpcResponse
	if err := json.Unmarshal(init, &initResp); err != nil || initResp.Error != nil {
		t.Fatalf("%s", init)
	}
	raw, _ := json.Marshal(initResp.Result)
	if !strings.Contains(string(raw), "lossless") || !strings.Contains(string(raw), "tools") {
		t.Fatal(string(raw))
	}

	if out := s.Handle([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); out != nil {
		t.Fatalf("notification should be silent: %s", out)
	}

	listed := s.Handle([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if !strings.Contains(string(listed), `"ask"`) || !strings.Contains(string(listed), `"remember"`) || !strings.Contains(string(listed), `"get_record"`) {
		t.Fatal(string(listed))
	}
	if strings.Contains(string(listed), `"name":"catch-up"`) || strings.Contains(string(listed), `"name": "catch-up"`) {
		t.Fatal("catch-up must not be an MCP tool")
	}

	call := s.Handle([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","goal":"add rate limiting","paths":["src/middleware/auth.ts"]}}}`))
	if !strings.Contains(string(call), "Redis") || !strings.Contains(string(call), "warnings") {
		t.Fatal(string(call))
	}
	if !strings.Contains(string(call), `"isError":false`) {
		t.Fatal(string(call))
	}
}

func TestRememberAndGetRecord(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(Local{Store: st})
	got := s.Handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"remember","arguments":{"type":"decision","text":"Use jose, not jsonwebtoken, for Edge.","project":"acme/api"}}}`))
	var wrap rpcResponse
	if err := json.Unmarshal(got, &wrap); err != nil || wrap.Error != nil {
		t.Fatal(string(got))
	}
	m, _ := wrap.Result.(map[string]any)
	sc, _ := m["structuredContent"].(map[string]any)
	ids, _ := sc["ids"].([]any)
	if len(ids) == 0 {
		t.Fatal(string(got))
	}
	id, _ := ids[0].(string)
	fetch := s.Handle([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_record","arguments":{"id":"` + id + `"}}}`))
	if !strings.Contains(string(fetch), "jose") {
		t.Fatal(string(fetch))
	}
	missing := s.Handle([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_record","arguments":{"id":"nope"}}}`))
	if !strings.Contains(string(missing), `"isError":true`) {
		t.Fatal(string(missing))
	}
}

func TestToolErrors(t *testing.T) {
	s := New(nil)
	got := s.Handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{}}}`))
	if !strings.Contains(string(got), "no backend") {
		t.Fatal(string(got))
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s = New(Local{Store: st})
	got = s.Handle([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask","arguments":"nope"}}`))
	if !strings.Contains(string(got), "isError") {
		t.Fatal(string(got))
	}
	got = s.Handle([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ask","arguments":{}}}`))
	if !strings.Contains(string(got), "project") && !strings.Contains(string(got), "isError") {
		t.Fatal(string(got))
	}
	got = s.Handle([]byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"remember","arguments":{"type":"decision"}}}`))
	if !strings.Contains(string(got), "isError") {
		t.Fatal(string(got))
	}
	got = s.Handle([]byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_record","arguments":{}}}`))
	if !strings.Contains(string(got), "id required") {
		t.Fatal(string(got))
	}
	got = s.Handle([]byte(`{"jsonrpc":"2.0","id":6,"method":"initialize"}`))
	if !strings.Contains(string(got), "lossless") {
		t.Fatal(string(got))
	}
	got = s.Handle([]byte(`{"jsonrpc":"2.0","id":7}`))
	if !strings.Contains(string(got), "-32600") {
		t.Fatal(string(got))
	}
}

func TestUnknownMethodAndTool(t *testing.T) {
	s := New(Local{})
	got := s.Handle([]byte(`{"jsonrpc":"2.0","id":1,"method":"nope"}`))
	if !strings.Contains(string(got), "-32601") {
		t.Fatal(string(got))
	}
	got = s.Handle([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search"}}`))
	if !strings.Contains(string(got), "unknown tool") {
		t.Fatal(string(got))
	}
	got = s.Handle([]byte(`not-json`))
	if !strings.Contains(string(got), "-32700") {
		t.Fatal(string(got))
	}
	if s.Handle([]byte(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)) == nil {
		t.Fatal("ping")
	}
}

func TestHTTPHandler(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(Local{Store: st})
	srv := httptest.NewServer(s.HTTPHandler())
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL, "application/json", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	if !strings.Contains(buf.String(), "ask") {
		t.Fatal(buf.String())
	}
	res, err = http.Post(srv.URL, "application/json", bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatal(res.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)))
	req.Header.Set("Accept", "text/event-stream")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if !strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(res.Header.Get("Content-Type"))
	}
	res, err = http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(res.StatusCode)
	}
}

func TestStdioAndContentLength(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(Local{Store: st})
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var out bytes.Buffer
	if err := s.ServeStdio(in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"result"`) {
		t.Fatal(out.String())
	}

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	framed := "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	in = bytes.NewBufferString(framed)
	out.Reset()
	if err := s.ServeStdio(in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ask") {
		t.Fatal(out.String())
	}
}

func TestContentLengthTooLarge(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(Local{Store: st})
	framed := "Content-Length: 999999999\r\n\r\n{}"
	var out bytes.Buffer
	if err := s.ServeStdio(strings.NewReader(framed), &out); err == nil {
		t.Fatal("expected Content-Length reject")
	}
}
