package serve

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

func TestCheckAddr(t *testing.T) {
	if err := CheckAddr("127.0.0.1:7432", ""); err != nil {
		t.Fatal(err)
	}
	if err := CheckAddr("0.0.0.0:7432", ""); err == nil {
		t.Fatal("expected refuse public listen without token")
	}
	if err := CheckAddr("0.0.0.0:7432", "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestAskAndGetRecord(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.WriteClaim(claim.Record{
		ID: "01JFAIL", Type: "failed", ProjectKey: "acme/api",
		Text:  "Redis token bucket failed in staging; connection pool exhausted.",
		Paths: []string{"src/middleware/auth.ts"}, Harness: "grok",
		SessionID: "s", CreatedAt: time.Date(2026, 8, 1, 18, 12, 0, 0, time.UTC).Format(time.RFC3339),
		Status: "active", Source: "import",
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)

	body := []byte(`{"project":"acme/api","question":"rate limiting on auth","goal":"add rate limiting","paths":["src/middleware/auth.ts"]}`)
	res, err := http.Post(srv.URL+"/v1/ask", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var out retrieve.Response
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Context[0].Text+joinTexts(out), "Redis") {
		t.Fatalf("ask missed redis: %+v", out)
	}

	got, err := http.Get(srv.URL + "/v1/records/01JFAIL")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	if got.StatusCode != 200 {
		t.Fatalf("get status %d", got.StatusCode)
	}
	var rec claim.Record
	if err := json.NewDecoder(got.Body).Decode(&rec); err != nil {
		t.Fatal(err)
	}
	if rec.ID != "01JFAIL" || !strings.Contains(rec.Text, "Redis") {
		t.Fatalf("record: %+v", rec)
	}
}

func TestGetRecordIncludesExcerpt(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	src := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(src, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := write.CatchUp(st, write.CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "sx"})
	if err != nil || len(res.IDs) == 0 {
		t.Fatal(res, err)
	}
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)
	got, err := http.Get(srv.URL + "/v1/records/" + res.IDs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(got.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	ex, _ := body["excerpt"].(string)
	if !strings.Contains(ex, "jose") {
		t.Fatalf("%v", body)
	}
}

func TestAskMissingProject400(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/v1/ask", "application/json", bytes.NewReader([]byte(`{"question":"hi"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestAskEmptyContext200(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/v1/ask", "application/json", bytes.NewReader([]byte(`{"project":"acme/api","question":"nothing"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestBearerRequired(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(Handler(st, "s3cret"))
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/v1/ask", "application/json", bytes.NewReader([]byte(`{"project":"acme/api"}`)))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("status %d", res.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/ask", bytes.NewReader([]byte(`{"project":"acme/api"}`)))
	req.Header.Set("Authorization", "Bearer s3cret")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("authed status %d", res.StatusCode)
	}
}

func TestCheckAddrAndLoopback(t *testing.T) {
	if err := CheckAddr("", ""); err != nil {
		t.Fatal(err)
	}
	if err := CheckAddr("localhost:9", ""); err != nil {
		t.Fatal(err)
	}
	if loopback(":7432") || loopback("0.0.0.0:1") || !loopback("127.0.0.1") || !loopback("[::1]:1") {
		t.Fatal("loopback")
	}
	if loopback(":") {
		t.Fatal("colon")
	}
	if loopback("not-an-addr") {
		t.Fatal("hostname")
	}
}

func TestHandlerMethodsAndErrors(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)

	post := func(path string) *http.Response {
		t.Helper()
		res, err := http.Post(srv.URL+path, "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	get := func(path string) *http.Response {
		t.Helper()
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	r := post("/health")
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()

	r = get("/v1/ask")
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()

	r = post("/v1/records/x")
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()

	r, err = http.Post(srv.URL+"/v1/ask", "application/json", bytes.NewReader([]byte(`{`)))
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 400 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()

	r = get("/v1/records/")
	if r.StatusCode != 400 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()

	r = get("/v1/records/missing")
	if r.StatusCode != 404 {
		t.Fatal(r.StatusCode)
	}
	r.Body.Close()

	h := get("/health")
	if h.StatusCode != 200 {
		t.Fatal(h.StatusCode)
	}
	h.Body.Close()
}

func TestMCPHTTP(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.WriteClaim(claim.Record{
		ID: "01JJOSE", Type: "decision", ProjectKey: "acme/api",
		Text: "Use jose, not jsonwebtoken, for Edge.", Harness: "grok",
		SessionID: "s", Status: "active", Source: "import",
		CreatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/mcp", "application/json", bytes.NewReader([]byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask","arguments":{"project":"acme/api","question":"jose"}}}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(res.Body)
	if !strings.Contains(buf.String(), "jose") {
		t.Fatal(buf.String())
	}
}

func TestCatchUpAndRememberHTTP(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	src := filepath.Join(t.TempDir(), "chat.jsonl")
	if err := os.WriteFile(src, []byte(`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]any{"path_to_jsonl": src, "project": "acme/api", "harness": "grok", "session_id": "s1"})
	res, err := http.Post(srv.URL+"/v1/catch-up", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	res, err = http.Post(srv.URL+"/v1/remember", "application/json", bytes.NewReader([]byte(
		`{"project":"acme/api","type":"failed","text":"Redis token bucket failed in staging yesterday."}`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	res, err = http.Post(srv.URL+"/v1/catch-up", "application/json", bytes.NewReader([]byte(`{`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()
	res, err = http.Post(srv.URL+"/v1/remember", "application/json", bytes.NewReader([]byte(`{`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()
	res, err = http.Post(srv.URL+"/v1/remember", "application/json", bytes.NewReader([]byte(`{"type":"decision"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	res, err = http.Post(srv.URL+"/v1/remember", "application/json", bytes.NewReader([]byte(
		`{"project":"acme/api","type":"decision","text":"Use jose, not jsonwebtoken, for Edge.","id":"../etc/passwd"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	res, err = http.Get(srv.URL + "/v1/records/has/slash")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	res, err = http.Get(srv.URL + "/v1/catch-up")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()
	res, err = http.Get(srv.URL + "/v1/remember")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()
}

func TestAskInternalError(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)
	res, err := http.Post(srv.URL+"/v1/ask", "application/json", bytes.NewReader([]byte(`{"project":"acme/api","paths":["a.ts"],"question":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 500 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestListenStartsWatch(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	errCh := make(chan error, 1)
	go func() { errCh <- Listen(Options{Addr: addr, Watch: true}, st) }()
	ok := false
	for i := 0; i < 40; i++ {
		res, err := http.Get("http://" + addr + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				ok = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ok {
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
			t.Fatal("not healthy")
		}
	}
}

func TestListenAndServe(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := ListenAndServe("0.0.0.0:9", "", st); err == nil {
		t.Fatal("refuse")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	errCh := make(chan error, 1)
	go func() { errCh <- ListenAndServe(addr, "", st) }()
	var last error
	for i := 0; i < 40; i++ {
		res, err := http.Get("http://" + addr + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		last = err
		time.Sleep(25 * time.Millisecond)
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	}
	t.Fatalf("never healthy: %v", last)
}

func TestAlreadyServingRequiresLosslessHealth(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(plain.Close)
	if alreadyServing(plain.Listener.Addr().String()) {
		t.Fatal("plain 200 must not count as lossless")
	}
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/health", http.StatusFound)
	}))
	t.Cleanup(redir.Close)
	if alreadyServing(redir.Listener.Addr().String()) {
		t.Fatal("redirect must not count")
	}
	okOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(okOnly.Close)
	if alreadyServing(okOnly.Listener.Addr().String()) {
		t.Fatal("ok without records must not count")
	}
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"records":0,"embedder":"none"}`))
	}))
	t.Cleanup(good.Close)
	if !alreadyServing(good.Listener.Addr().String()) {
		t.Fatal("lossless health must count")
	}
}

func TestListenAlreadyServingIsOK(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	errCh := make(chan error, 1)
	go func() { errCh <- Listen(Options{Addr: addr}, st) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get("http://" + addr + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := Listen(Options{Addr: addr}, st); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestAppendHTTP(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(Handler(st, "tok"))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/append", strings.NewReader(
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n"))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("X-Project", "acme/api")
	req.Header.Set("X-Harness", "grok")
	req.Header.Set("X-Session", "s-home")
	req.Header.Set("X-Client", "c1")
	req.Header.Set("X-Prev-Offset", "0")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	var first write.AppendResult
	_ = json.NewDecoder(res.Body).Decode(&first)
	res.Body.Close()
	if first.AcceptedThrough == 0 {
		t.Fatal(first)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/append", strings.NewReader("x\n"))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("X-Project", "acme/api")
	req.Header.Set("X-Session", "s-home")
	req.Header.Set("X-Client", "c1")
	req.Header.Set("X-Prev-Offset", "0")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 409 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/append", nil)
	req.Header.Set("Authorization", "Bearer tok")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	body, _ := json.Marshal(map[string]any{
		"project": "acme/api", "harness": "opencode", "session_id": "ses_http",
		"messages": []map[string]any{{"role": "assistant", "content": "We decided to use jose, not jsonwebtoken, for Edge."}},
	})
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/catch-up", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	res.Body.Close()

	h, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	if h.StatusCode != 200 {
		t.Fatalf("health behind token: %d", h.StatusCode)
	}
	h.Body.Close()

	bad, err := http.Post(srv.URL+"/v1/ask", "application/json", strings.NewReader(`{"project":"acme/api"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bad.StatusCode != 401 {
		t.Fatalf("ask without token: %d", bad.StatusCode)
	}
	bad.Body.Close()
}

func TestCatchUpHTTPRejectsNonJSONL(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	p := filepath.Join(t.TempDir(), "secrets.txt")
	if err := os.WriteFile(p, []byte("AKIAIOSFODNN7EXAMPLE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)
	body, _ := json.Marshal(map[string]any{"path_to_jsonl": p, "project": "acme/api", "session_id": "x"})
	res, err := http.Post(srv.URL+"/v1/catch-up", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func joinTexts(out retrieve.Response) string {
	var b strings.Builder
	for _, h := range out.Context {
		b.WriteString(h.Text)
	}
	return b.String()
}
