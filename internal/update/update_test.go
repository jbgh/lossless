package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeAndCompare(t *testing.T) {
	if NormalizeTag("v0.1.0") != "0.1.0" || NormalizeTag("0.1.0") != "0.1.0" {
		t.Fatal(NormalizeTag("v0.1.0"))
	}
	if Compare("0.1.0", "v0.1.0") != 0 {
		t.Fatal("equal")
	}
	if Compare("0.1.0", "0.1.1") >= 0 {
		t.Fatal("older")
	}
	if Compare("0.2.0", "0.1.9") <= 0 {
		t.Fatal("newer")
	}
}

func TestAssetNameAndDest(t *testing.T) {
	if AssetName("darwin", "arm64") != "lossless-darwin-arm64" {
		t.Fatal(AssetName("darwin", "arm64"))
	}
	got := DefaultDest("/Users/x")
	if got != "/Users/x/.local/bin/lossless" {
		t.Fatal(got)
	}
}

func TestLooksLikeSourceTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "lossless"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module lossless\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "lossless")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !LooksLikeSourceTree(exe) {
		t.Fatal("source tree")
	}
	if LooksLikeSourceTree(filepath.Join(t.TempDir(), "lossless")) {
		t.Fatal("plain dest")
	}
}

func TestChecksumFor(t *testing.T) {
	sums := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  lossless-darwin-arm64\n")
	got, err := checksumFor(sums, "lossless-darwin-arm64")
	if err != nil || got != strings.Repeat("a", 64) {
		t.Fatal(got, err)
	}
	if _, err := checksumFor(sums, "missing"); err == nil {
		t.Fatal("expected missing")
	}
	if _, err := checksumFor([]byte("zzzz  lossless-darwin-arm64\n"), "lossless-darwin-arm64"); err == nil {
		t.Fatal("expected bad checksum")
	}
}

func TestInstallBinaryReplacesSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "old")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "bin", "lossless")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dest); err != nil {
		t.Fatal(err)
	}
	body := []byte{0xcf, 0xfa, 0xed, 0xfe, 0x01, 0x02, 0x03, 0x04}
	if err := installBinary(dest, body); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("must replace symlink")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(body) {
		t.Fatal(got, err)
	}
	keep, err := os.ReadFile(outside)
	if err != nil || string(keep) != "keep" {
		t.Fatal("wrote through symlink")
	}
}

func TestDestOffChannel(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	if !DestOffChannel(missing) {
		t.Fatal("missing")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(missing, link); err != nil {
		t.Fatal(err)
	}
	if !DestOffChannel(link) {
		t.Fatal("symlink")
	}
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if DestOffChannel(real) {
		t.Fatal("regular file is on channel")
	}
}

func TestApplyCheckAndInstall(t *testing.T) {
	machO := []byte{0xcf, 0xfa, 0xed, 0xfe, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03}
	sum := sha256.Sum256(machO)
	hexSum := hex.EncodeToString(sum[:])
	sums := hexSum + "  lossless-darwin-arm64\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/jbgh/lossless/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t0k" {
			http.Error(w, "no token", 401)
			return
		}
		fmt.Fprintf(w, `{
  "tag_name": "v0.1.0",
  "assets": [
    {"name":"lossless-darwin-arm64","browser_download_url":"http://%s/download/lossless-darwin-arm64","size":%d},
    {"name":"SHA256SUMS","browser_download_url":"http://%s/download/SHA256SUMS","size":%d}
  ]
}`, r.Host, len(machO), r.Host, len(sums))
	})
	mux.HandleFunc("/download/lossless-darwin-arm64", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(machO)
	})
	mux.HandleFunc("/download/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), ".local", "bin", "lossless")
	opts := Options{
		Repo:     DefaultRepo,
		API:      srv.URL,
		Dest:     dest,
		Current:  "0.0.1",
		GOOS:     "darwin",
		GOARCH:   "arm64",
		HTTP:     Client(srv.URL),
		Token:    "t0k",
		UserHome: t.TempDir(),
	}

	check, err := Apply(Options{
		Repo: opts.Repo, API: opts.API, Dest: dest, Current: "0.0.1",
		GOOS: "darwin", GOARCH: "arm64", HTTP: Client(srv.URL), Token: "t0k",
		CheckOnly: true,
	})
	if err != nil || !check.UpdateAvailable || check.Replaced {
		t.Fatalf("%+v %v", check, err)
	}

	res, err := Apply(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced || res.Version != "0.1.0" {
		t.Fatalf("%+v", res)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(machO) {
		t.Fatal(got, err)
	}
	fi, err := os.Lstat(dest)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal(fi, err)
	}

	opts.Current = "0.1.0"
	again, err := Apply(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !again.AlreadyLatest || again.Replaced {
		t.Fatalf("second apply should be current: %+v", again)
	}

	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
	}))
	t.Cleanup(redir.Close)
	_, err = Apply(Options{
		Repo: DefaultRepo, API: redir.URL, Dest: dest, Current: "0.0.1",
		GOOS: "darwin", GOARCH: "arm64", HTTP: Client(redir.URL), Token: "t0k",
	})
	if err == nil || !strings.Contains(err.Error(), "refusing host") {
		t.Fatal(err)
	}
}

func TestApplyRefusesChecksumMismatch(t *testing.T) {
	machO := []byte{0xcf, 0xfa, 0xed, 0xfe, 0x00}
	sums := strings.Repeat("0", 64) + "  lossless-linux-amd64\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			fmt.Fprintf(w, `{"tag_name":"v0.1.0","assets":[
				{"name":"lossless-linux-amd64","browser_download_url":"http://%s/a","size":5},
				{"name":"SHA256SUMS","browser_download_url":"http://%s/s","size":80}]}`, r.Host, r.Host)
		case r.URL.Path == "/a":
			_, _ = w.Write(machO)
		case r.URL.Path == "/s":
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	_, err := Apply(Options{
		Repo: DefaultRepo, API: srv.URL, Dest: filepath.Join(t.TempDir(), "lossless"),
		Current: "0.0.1", GOOS: "linux", GOARCH: "amd64", HTTP: Client(srv.URL),
	})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal(err)
	}
}

func TestCheckURL(t *testing.T) {
	must := func(s string) *http.Request {
		t.Helper()
		r, err := http.NewRequest(http.MethodGet, s, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if err := checkURL(must("https://evil.example/x").URL, DefaultAPI); err == nil {
		t.Fatal("evil host")
	}
	if err := checkURL(must("http://api.github.com/x").URL, DefaultAPI); err == nil {
		t.Fatal("http github")
	}
	if err := checkURL(must("https://api.github.com/repos/jbgh/lossless/releases/latest").URL, DefaultAPI); err != nil {
		t.Fatal(err)
	}
	if err := checkURL(must("https://github.com/jbgh/lossless/releases/download/v0.1.0/x").URL, DefaultAPI); err != nil {
		t.Fatal(err)
	}
	if err := checkURL(must("https://release-assets.githubusercontent.com/x").URL, DefaultAPI); err != nil {
		t.Fatal(err)
	}
	if err := checkURL(must("https://objects.githubusercontent.com/foo").URL, DefaultAPI); err != nil {
		t.Fatal(err)
	}
}
