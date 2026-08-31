package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lossless/internal/store"
)

func localStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// The tokenless listener is loopback-bound, but a browser can still reach
// it: DNS rebinding carries a foreign Host, cross-origin XHR a foreign
// Origin. Both must be refused or a web page can read the index and
// poison memory via /v1/remember.
func TestTokenlessRejectsForeignHost(t *testing.T) {
	h := Handler(localStore(t), "")
	req := httptest.NewRequest(http.MethodPost, "/v1/ask", strings.NewReader(`{"project":"acme/api"}`))
	req.Host = "rebind.attacker.example:7432"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign Host served: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTokenlessRejectsForeignOrigin(t *testing.T) {
	h := Handler(localStore(t), "")
	req := httptest.NewRequest(http.MethodPost, "/v1/remember", strings.NewReader(`{"project":"acme/api","type":"decision","text":"poison"}`))
	req.Host = "127.0.0.1:7432"
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin served: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTokenlessAllowsLoopbackClients(t *testing.T) {
	h := Handler(localStore(t), "")
	for _, host := range []string{"127.0.0.1:7432", "localhost:7432", "[::1]:7432"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/ask", strings.NewReader(`{"project":"acme/api"}`))
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("host %s refused: %d %s", host, rec.Code, rec.Body.String())
		}
	}
}

// Remote mode binds a real hostname behind TLS with a token; the bearer
// is the auth there and a foreign Host must keep working.
func TestTokenModeAllowsForeignHost(t *testing.T) {
	h := Handler(localStore(t), "sekrit")
	req := httptest.NewRequest(http.MethodPost, "/v1/ask", strings.NewReader(`{"project":"acme/api"}`))
	req.Host = "home.example:7432"
	req.Header.Set("Authorization", "Bearer sekrit")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token-mode foreign host refused: %d %s", rec.Code, rec.Body.String())
	}
}
