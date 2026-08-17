package harness

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSetupWritesHooksAndMCP(t *testing.T) {
	user := t.TempDir()
	data := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(user, ".config", "opencode"))
	res, err := Setup(SetupOpts{
		UserHome: user, DataHome: data, Exe: "/bin/am",
		Service: false, Start: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Wrote) < 10 {
		t.Fatalf("wrote %d: %v", len(res.Wrote), res.Wrote)
	}
	for _, rel := range []string{
		".grok/hooks/lossless.json",
		".claude/settings.json",
		".codex/hooks.json",
		".pi/agent/extensions/lossless.ts",
		".grok/config.toml",
		".claude.json",
		".codex/config.toml",
		".pi/agent/mcp.json",
	} {
		if _, err := os.Stat(filepath.Join(user, rel)); err != nil {
			t.Fatal(rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(data, ".grok")); err == nil {
		t.Fatal("hooks must not land in data home")
	}
}

func TestDoctorBeforeAndAfterSetup(t *testing.T) {
	user := t.TempDir()
	data := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(user, ".config", "opencode"))
	before := Doctor(user, data, "/bin/am", "", "")
	if before.Ok() {
		t.Fatal("empty machine should fail doctor")
	}
	if _, err := Setup(SetupOpts{UserHome: user, DataHome: data, Exe: "/bin/am", Service: false, Start: false}); err != nil {
		t.Fatal(err)
	}
	// data home now exists; daemon still down
	after := Doctor(user, data, "/bin/am", "", "")
	var hooks, mcp, daemon Check
	for _, c := range after.Checks {
		switch c.Name {
		case "hooks":
			hooks = c
		case "mcp":
			mcp = c
		case "daemon":
			daemon = c
		}
	}
	if !hooks.OK || !mcp.OK {
		t.Fatalf("hooks=%+v mcp=%+v", hooks, mcp)
	}
	if daemon.OK {
		t.Fatal("daemon should be down")
	}
}

func TestDoctorDaemonAndRemoteURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"records":3,"embedder":"none"}`))
	}))
	t.Cleanup(srv.Close)
	user := t.TempDir()
	data := t.TempDir()
	rep := Doctor(user, data, "/bin/am", srv.URL, "")
	var daemon Check
	for _, c := range rep.Checks {
		if c.Name == "daemon" {
			daemon = c
		}
	}
	if !daemon.OK || !strings.Contains(daemon.Detail, "records=3") {
		t.Fatalf("%+v", daemon)
	}
	bad := Doctor(user, data, "/bin/am", "http://home.example:7432", "")
	var url Check
	for _, c := range bad.Checks {
		if c.Name == "url" {
			url = c
		}
	}
	if url.OK {
		t.Fatal("plain http remote must fail")
	}
}

func TestInstallUserServiceWritesUnit(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip(runtime.GOOS)
	}
	user := t.TempDir()
	data := t.TempDir()
	p, err := InstallUserService("/bin/am", user, data, "https://home.example", "sekrit")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "serve") || !strings.Contains(s, data) {
		t.Fatal(s)
	}
	if !strings.Contains(s, "sekrit") {
		t.Fatal("service unit is 0600 and must carry the token so the daemon starts")
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestSetupRequiresHomes(t *testing.T) {
	if _, err := Setup(SetupOpts{}); err == nil {
		t.Fatal("expected error")
	}
}
