package write

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpoolAndEnsure(t *testing.T) {
	st := tmpStore(t)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	dest, err := WriteSpool(st.Root, SpoolJob{
		JSONL: src, Project: "acme/api", Harness: "grok", SessionID: "s-spool", Source: "turn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	res, err := Ensure(st, st.Root)
	if err != nil {
		t.Fatal(err)
	}
	if res.Replayed != 1 {
		t.Fatalf("replayed=%d errors=%v", res.Replayed, res.Errors)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("spool should be consumed")
	}
	claims, _ := st.ListActive("acme/api")
	found := false
	for _, c := range claims {
		if strings.Contains(c.Text, "jose") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected jose after ensure: %+v", claims)
	}
	again, err := Ensure(st, st.Root)
	if err != nil || again.Replayed != 0 {
		t.Fatalf("empty ensure: %+v %v", again, err)
	}
}

func TestEnsureBadSpool(t *testing.T) {
	st := tmpStore(t)
	dir := SpoolDir(st.Root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.json"), []byte(`{}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "push-x.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Ensure(st, st.Root)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed < 1 || res.Skipped < 1 {
		t.Fatalf("%+v", res)
	}
}

func TestSubmitCatchUpSpoolsWhenCatchUpFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LOSSLESS_HOME", home)
	t.Setenv("LOSSLESS_SIDECAR", "http://127.0.0.1:1")
	t.Setenv("LOSSLESS_URL", "")
	SubmitCatchUp(CatchUpRequest{
		JSONL: filepath.Join(t.TempDir(), "missing.jsonl"),
		Project: "acme/api", SessionID: "s", Harness: "grok",
	})
	files, err := ListSpool(home)
	if err != nil || len(files) != 1 {
		t.Fatalf("spool files=%v err=%v", files, err)
	}
}

func TestSubmitCatchUpLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LOSSLESS_HOME", home)
	t.Setenv("LOSSLESS_SIDECAR", "http://127.0.0.1:1")
	t.Setenv("LOSSLESS_URL", "")
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	SubmitCatchUp(CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "s1", Harness: "grok"})
	files, _ := ListSpool(home)
	if len(files) != 0 {
		t.Fatal(files)
	}
	raws, _ := filepath.Glob(filepath.Join(home, "raw", "*", "*", "*.jsonl"))
	if len(raws) == 0 {
		t.Fatal("expected raw")
	}
}

func TestSubmitCatchUpTimeoutSpoolsNotLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LOSSLESS_HOME", home)
	t.Setenv("LOSSLESS_URL", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(600 * time.Millisecond)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("LOSSLESS_SIDECAR", srv.URL)
	src := writeJSONL(t, t.TempDir(), "chat.jsonl",
		`{"type":"assistant","content":"We decided to use jose, not jsonwebtoken, for Edge."}`+"\n")
	SubmitCatchUp(CatchUpRequest{JSONL: src, Project: "acme/api", SessionID: "slow", Harness: "grok"})
	files, _ := ListSpool(home)
	if len(files) != 1 {
		t.Fatalf("expected spool after timeout, got %v", files)
	}
	raws, _ := filepath.Glob(filepath.Join(home, "raw", "*", "*", "*.jsonl"))
	if len(raws) != 0 {
		t.Fatal("timeout must not also write locally")
	}
}

func TestUncertainSidecar(t *testing.T) {
	if uncertainSidecar(nil) {
		t.Fatal("nil")
	}
	if !uncertainSidecar(fmt.Errorf("catch-up status 500")) {
		t.Fatal("5xx")
	}
	if uncertainSidecar(fmt.Errorf("connection refused")) {
		t.Fatal("refused is certain-down")
	}
}
