package write

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListRawTapesSkipsSymlinkAndGroupsParts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "raw", "acme__api", "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s1.part2.jsonl"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret.jsonl")
	if err := os.WriteFile(secret, []byte("nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak.jsonl")); err != nil {
		t.Fatal(err)
	}
	tapes, err := ListRawTapes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tapes) != 1 || tapes[0].SessionID != "s1" || tapes[0].Project != "acme/api" {
		t.Fatalf("%+v", tapes)
	}
	if len(tapes[0].Paths) != 2 {
		t.Fatal(tapes[0].Paths)
	}
}

func TestMigratePushAndResume(t *testing.T) {
	var got []string
	var prevs []string
	accepted := int64(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ask" {
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"context":[],"project":"lossless/migrate"}`))
			return
		}
		if r.URL.Path != "/v1/append" {
			w.WriteHeader(404)
			return
		}
		prevs = append(prevs, r.Header.Get("X-Prev-Offset"))
		b := make([]byte, 4096)
		n, _ := r.Body.Read(b)
		got = append(got, string(b[:n]))
		accepted += int64(n)
		_ = json.NewEncoder(w).Encode(AppendResult{AcceptedThrough: accepted})
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	dir := filepath.Join(root, "raw", "acme__api", "2026-08")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl"), []byte("hello\nworld\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Migrate(MigrateOpts{DataHome: root, URL: srv.URL, Token: "tok", Push: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pushed != 1 || res.Bytes < 10 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(strings.Join(got, ""), "hello") {
		t.Fatal(got)
	}

	// Second run: home already has the prefix.
	accepted = 12
	got = nil
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ask" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(AppendResult{AcceptedThrough: 12, Conflict: true})
	}))
	t.Cleanup(srv2.Close)
	res, err = Migrate(MigrateOpts{DataHome: root, URL: srv2.URL, Token: "tok", Push: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 {
		t.Fatalf("want skip, got %+v", res)
	}
}

func TestMigrateRejectsCleartextAndMissingToken(t *testing.T) {
	if _, err := Migrate(MigrateOpts{DataHome: t.TempDir(), URL: "http://home.example", Token: "t", Push: false}); err == nil {
		t.Fatal("http remote")
	}
	if _, err := Migrate(MigrateOpts{DataHome: t.TempDir(), URL: "https://home.example", Token: "", Push: false}); err == nil {
		t.Fatal("token required")
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

func TestNextChunkBreaksOnLines(t *testing.T) {
	b := []byte("aaa\nbbbb\ncccc\n")
	got := nextChunk(b, 6)
	if string(got) != "aaa\n" {
		t.Fatalf("%q", got)
	}
}
