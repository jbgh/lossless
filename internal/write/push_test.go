package write

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHomeIsRemoteAndClientID(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "")
	if HomeIsRemote() {
		t.Fatal("empty")
	}
	t.Setenv("LOSSLESS_URL", "http://127.0.0.1:7432")
	if HomeIsRemote() {
		t.Fatal("loopback")
	}
	t.Setenv("LOSSLESS_URL", "http://home.example:7432")
	if !HomeIsRemote() {
		t.Fatal("remote")
	}
	home := t.TempDir()
	t.Setenv("LOSSLESS_CLIENT", "cid-1")
	if ClientID(home) != "cid-1" {
		t.Fatal(ClientID(home))
	}
	t.Setenv("LOSSLESS_CLIENT", "")
	id := ClientID(home)
	if id == "" || ClientID(home) != id {
		t.Fatal(id)
	}
}

func TestFlushPush(t *testing.T) {
	var gotOff string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/append" {
			http.NotFound(w, r)
			return
		}
		gotOff = r.Header.Get("X-Prev-Offset")
		b, _ := json.Marshal(AppendResult{AcceptedThrough: 10})
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LOSSLESS_URL", srv.URL)
	home := t.TempDir()
	if _, err := WritePush(home, PushJob{
		Project: "acme/api", SessionID: "s", Client: "c", Body: "hello\n",
	}); err != nil {
		t.Fatal(err)
	}
	n, err := FlushPush(home)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if gotOff != "0" || !strings.Contains(gotBody, "hello") {
		t.Fatalf("%q %q", gotOff, gotBody)
	}
	files, _ := ListPush(home)
	if len(files) != 0 {
		t.Fatal(files)
	}
}

func TestMaybeEnqueuePush(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "https://home.example:9")
	home := t.TempDir()
	MaybeEnqueuePush(home, CatchUpRequest{Project: "acme/api", SessionID: "s"}, "line\n", 0)
	files, _ := ListPush(home)
	if len(files) != 1 {
		t.Fatal(files)
	}
	t.Setenv("LOSSLESS_URL", "")
	MaybeEnqueuePush(home, CatchUpRequest{Project: "acme/api", SessionID: "s"}, "line\n", 0)
}

func TestFlushPushConflictAheadKeepsJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(AppendResult{AcceptedThrough: 10, Conflict: true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LOSSLESS_URL", srv.URL)
	home := t.TempDir()
	if _, err := WritePush(home, PushJob{SessionID: "s", PrevOff: 50, Body: "later\n"}); err != nil {
		t.Fatal(err)
	}
	n, err := FlushPush(home)
	if n != 0 || err == nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	files, _ := ListPush(home)
	if len(files) != 1 {
		t.Fatal("should keep job when home is behind")
	}
}

func TestFlushPushConflictBehindDropsJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(AppendResult{AcceptedThrough: 100, Conflict: true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LOSSLESS_URL", srv.URL)
	home := t.TempDir()
	if _, err := WritePush(home, PushJob{SessionID: "s", PrevOff: 0, Body: "old\n"}); err != nil {
		t.Fatal(err)
	}
	n, err := FlushPush(home)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	files, _ := ListPush(home)
	if len(files) != 0 {
		t.Fatal(files)
	}
}

func TestEnqueueHomePushOffsets(t *testing.T) {
	st := tmpStore(t)
	t.Setenv("LOSSLESS_URL", "https://home.example:7432")
	req := CatchUpRequest{Project: "acme/api", SessionID: "s-off", Harness: "grok"}
	enqueueHomePush(st, req, "aaa\n")
	enqueueHomePush(st, req, "bbbb\n")
	files, _ := ListPush(st.Root)
	if len(files) != 2 {
		t.Fatal(files)
	}
	var jobs []PushJob
	for _, p := range files {
		b, _ := os.ReadFile(p)
		var j PushJob
		if err := json.Unmarshal(b, &j); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, j)
	}
	if jobs[0].PrevOff != 0 || jobs[1].PrevOff != 4 {
		t.Fatalf("offsets %+v %+v", jobs[0], jobs[1])
	}
}

func TestCheckHomeURL(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "")
	if err := CheckHomeURL(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOSSLESS_URL", "http://127.0.0.1:7432")
	if err := CheckHomeURL(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOSSLESS_URL", "http://localhost:7432")
	if err := CheckHomeURL(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOSSLESS_URL", "https://home.example:7432")
	if err := CheckHomeURL(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOSSLESS_URL", "http://home.example:7432")
	if err := CheckHomeURL(); err == nil {
		t.Fatal("plain http remote must fail")
	}
	t.Setenv("LOSSLESS_URL", "ftp://home.example")
	if err := CheckHomeURL(); err == nil {
		t.Fatal("ftp must fail")
	}
}

func TestMaybeEnqueuePushRejectsPlainHTTPRemote(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "http://home.example:7432")
	home := t.TempDir()
	MaybeEnqueuePush(home, CatchUpRequest{Project: "acme/api", SessionID: "s"}, "line\n", 0)
	files, _ := ListPush(home)
	if len(files) != 0 {
		t.Fatal("must not enqueue to cleartext remote")
	}
}

func TestFlushPushDoesNotFollowRedirect(t *testing.T) {
	var leaked bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(evil.Close)
	homeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/v1/append", http.StatusFound)
	}))
	t.Cleanup(homeSrv.Close)
	t.Setenv("LOSSLESS_URL", homeSrv.URL)
	t.Setenv("LOSSLESS_TOKEN", "sekrit")
	home := t.TempDir()
	if _, err := WritePush(home, PushJob{SessionID: "s", Body: "hello\n"}); err != nil {
		t.Fatal(err)
	}
	n, err := FlushPush(home)
	if n != 0 || err == nil {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if leaked {
		t.Fatal("bearer followed redirect")
	}
}

func TestProbeHomeUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	if err := ProbeHome(srv.URL, "bad"); err == nil {
		t.Fatal("expected unauthorized")
	}
}

func TestHomeURLStripsMCP(t *testing.T) {
	t.Setenv("LOSSLESS_URL", "https://home.example/mcp")
	if HomeURL() != "https://home.example" {
		t.Fatal(HomeURL())
	}
}
