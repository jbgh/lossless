package update

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// "latest" older than the running build (a yanked release, a local
// build) must not be installed. --version pinning still may.
func TestApplyRefusesDowngrade(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/jbgh/lossless/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.1.10","assets":[{"name":"lossless-darwin-arm64","browser_download_url":"http://%s/download/x","size":1}]}`, r.Host)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	dest := filepath.Join(t.TempDir(), "lossless")
	res, err := Apply(Options{
		Repo: DefaultRepo, API: srv.URL, Dest: dest, Current: "0.1.22",
		GOOS: "darwin", GOARCH: "arm64", HTTP: Client(srv.URL), UserHome: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Replaced || !res.Ahead || res.UpdateAvailable {
		t.Fatalf("downgrade not refused: %+v", res)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("binary written on downgrade (stat err=%v)", err)
	}
}
