package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lossless/internal/claim"
	"lossless/internal/retrieve"
	"lossless/internal/store"
	"lossless/internal/write"
)

func TestHTTPBackendAgainstDaemon(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := write.Remember(st, claim.Record{
		Type: "decision", Text: "Use jose, not jsonwebtoken, for Edge.", ProjectKey: "acme/api",
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ask", func(w http.ResponseWriter, r *http.Request) {
		var req retrieve.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		out, err := retrieve.Ask(st, req)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/v1/remember", func(w http.ResponseWriter, r *http.Request) {
		var rec claim.Record
		_ = json.NewDecoder(r.Body).Decode(&rec)
		out, err := write.Remember(st, rec)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, 400)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/v1/records/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/records/")
		rec, ok := st.Get(id)
		if !ok {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(rec)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	b := HTTP{BaseURL: srv.URL, Token: "tok"}
	out, err := b.Ask(retrieve.Request{Project: "acme/api", Question: "jose"})
	if err != nil || len(out.Context) == 0 {
		t.Fatalf("%+v %v", out, err)
	}
	res, err := b.Remember(claim.Record{
		Type: "failed", Text: "Redis token bucket failed in staging yesterday.", ProjectKey: "acme/api",
	})
	if err != nil || res.Extracted != 1 {
		t.Fatal(res, err)
	}
	// The stdio MCP path is an HTTP client, so the record's session and
	// harness have to survive the round trip. Dropping them made every
	// remember from a harness-stamped call land as session "manual".
	stamped, err := b.Remember(claim.Record{
		Type: "failed", Text: "The stamped remember kept its session.", ProjectKey: "acme/api",
		Harness: "omp", SessionID: "01a130f8-96e7-749d-bc95-9abfa64d6dea",
	})
	if err != nil || len(stamped.IDs) != 1 {
		t.Fatal(stamped, err)
	}
	got, ok := st.Get(stamped.IDs[0])
	if !ok {
		t.Fatal("record missing")
	}
	if got.SessionID != "01a130f8-96e7-749d-bc95-9abfa64d6dea" || got.Harness != "omp" {
		t.Fatalf("session/harness dropped over HTTP: %+v", got)
	}
	rec, ok, err := b.Get(res.IDs[0], "", "")
	if err != nil || !ok || rec.Type != "failed" {
		t.Fatal(rec, ok, err)
	}
	if _, ok, err := b.Get("missing", "", ""); err != nil || ok {
		t.Fatal("missing", ok, err)
	}
}

func TestHTTPBackendUnreachable(t *testing.T) {
	b := HTTP{BaseURL: "http://127.0.0.1:1"}
	_, err := b.Ask(retrieve.Request{Project: "acme/api"})
	if err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("err=%v", err)
	}
}
