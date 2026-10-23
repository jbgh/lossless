package serve

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lossless/internal/retrieve"
	"lossless/internal/store"
)

func captureSlowAsks(t *testing.T, after time.Duration) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevAfter, prevOut := slowAskAfter, slowAskOut
	slowAskAfter, slowAskOut = after, &buf
	t.Cleanup(func() { slowAskAfter, slowAskOut = prevAfter, prevOut })
	return &buf
}

func postAsk(t *testing.T, srv *httptest.Server, body string) {
	t.Helper()
	res, err := http.Post(srv.URL+"/v1/ask", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("ask status %d", res.StatusCode)
	}
}

// An ask past the threshold leaves one line in serve.log saying where its
// time went, so the next 2-second ask can be told apart from CPU
// starvation. It names no goal, path or session id.
func TestSlowAskWritesATimingLine(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(Handler(st, ""))
	t.Cleanup(srv.Close)

	buf := captureSlowAsks(t, 0)
	postAsk(t, srv, `{"project":"acme/api","goal":"secret-goal-text","session_id":"sid-123"}`)
	line := buf.String()
	for _, want := range []string{"slow ask", "catch-up", "project", "rest", "cpu", "sid=yes", "in flight 1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("slow-ask line missing %q: %q", want, line)
		}
	}
	for _, leak := range []string{"secret-goal-text", "sid-123", "acme/api"} {
		if strings.Contains(line, leak) {
			t.Fatalf("slow-ask line leaks %q: %q", leak, line)
		}
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("want one line, got %q", line)
	}

	buf.Reset()
	slowAskAfter = time.Hour
	postAsk(t, srv, `{"project":"acme/api","goal":"fast"}`)
	if buf.Len() != 0 {
		t.Fatalf("a fast ask logged: %q", buf.String())
	}
}

func TestSlowAskLineFormat(t *testing.T) {
	got := slowAskLine(2600*time.Millisecond, 310*time.Millisecond,
		retrieve.AskTiming{CatchUp: 100 * time.Millisecond, CatchUps: 1, Project: 20 * time.Millisecond}, false, 2)
	want := "slow ask 2.60s: catch-up 0.10s (1 session), project 0.02s, rest 2.48s; daemon cpu 0.31s during it, sid=no, in flight 2"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
