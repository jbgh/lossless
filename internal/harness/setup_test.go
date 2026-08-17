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
		".grok/skills/lossless/SKILL.md",
		".claude/skills/lossless/SKILL.md",
		".agents/skills/lossless/SKILL.md",
		".codex/skills/lossless/SKILL.md",
		".pi/agent/skills/lossless/SKILL.md",
		".config/opencode/skills/lossless/SKILL.md",
		".grok/rules/lossless.md",
		".claude/rules/lossless.md",
		".claude/CLAUDE.md",
		".codex/AGENTS.md",
		".pi/agent/AGENTS.md",
		".config/opencode/AGENTS.md",
	} {
		if _, err := os.Stat(filepath.Join(user, rel)); err != nil {
			t.Fatal(rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(data, ".grok")); err == nil {
		t.Fatal("hooks must not land in data home")
	}
	g, err := os.ReadFile(filepath.Join(user, ".grok", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(g), "127.0.0.1") || strings.Contains(string(g), "Authorization") {
		t.Fatalf("setup must be local: %s", g)
	}
}

func TestDoctorBeforeAndAfterSetup(t *testing.T) {
	user := t.TempDir()
	data := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(user, ".config", "opencode"))
	dead := "http://127.0.0.1:1"
	before := Doctor(user, data, "/bin/am", dead, "")
	if before.Ok() {
		t.Fatal("empty machine should fail doctor")
	}
	if _, err := Setup(SetupOpts{UserHome: user, DataHome: data, Exe: "/bin/am", Service: false, Start: false}); err != nil {
		t.Fatal(err)
	}
	after := Doctor(user, data, "/bin/am", dead, "")
	var hooks, mcp, skills, rules, daemon Check
	for _, c := range after.Checks {
		switch c.Name {
		case "hooks":
			hooks = c
		case "mcp":
			mcp = c
		case "skills":
			skills = c
		case "rules":
			rules = c
		case "daemon":
			daemon = c
		}
	}
	if !hooks.OK || !mcp.OK || !skills.OK || !rules.OK {
		t.Fatalf("hooks=%+v mcp=%+v skills=%+v rules=%+v", hooks, mcp, skills, rules)
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
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	envb, err := os.ReadFile(serviceEnvPath(data))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(envb), "sekrit") {
		t.Fatal("token must live in 0600 service.env")
	}
	if runtime.GOOS == "linux" && strings.Contains(s, "sekrit") {
		t.Fatal("systemd unit must not inline the token")
	}
	if runtime.GOOS == "linux" && !strings.Contains(s, "EnvironmentFile") {
		t.Fatal(s)
	}
}

func TestInstallUserServiceRejectsControlChars(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip(runtime.GOOS)
	}
	_, err := InstallUserService("/bin/am", t.TempDir(), t.TempDir(), "", "sekrit\nExecStart=/evil")
	if err == nil {
		t.Fatal("newline token")
	}
}

func TestSystemdQuoteAndNoInlineToken(t *testing.T) {
	unit := string(systemdUnit(`/opt/my tools/lossless`, `/home/u/My Home/.lossless`, "https://home.example", "sekrit"))
	if strings.Contains(unit, "sekrit") {
		t.Fatal(unit)
	}
	if !strings.Contains(unit, `"/opt/my tools/lossless"`) || !strings.Contains(unit, `"/home/u/My Home/.lossless"`) {
		t.Fatal(unit)
	}
}

func TestSetupRejectsCleartextRemote(t *testing.T) {
	_, err := Setup(SetupOpts{
		UserHome: t.TempDir(), DataHome: t.TempDir(), Exe: "/bin/am",
		URL: "http://home.example:7432", Service: false, Start: false,
	})
	if err == nil {
		t.Fatal("expected https requirement")
	}
}

func TestDoctorServiceOkWhenDaemonUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"records":0,"embedder":"none"}`))
	}))
	t.Cleanup(srv.Close)
	rep := Doctor(t.TempDir(), t.TempDir(), "/bin/am", srv.URL, "")
	for _, c := range rep.Checks {
		if c.Name == "service" && !c.OK {
			t.Fatalf("service should pass when daemon is up: %+v", c)
		}
	}
}

func TestProbeHealthNoRedirect(t *testing.T) {
	var hit bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(evil.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/health", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	if _, err := ProbeHealth(srv.URL); err == nil {
		t.Fatal("expected redirect reject")
	}
	if hit {
		t.Fatal("followed redirect")
	}
}

func TestWriteUserConfigPreservesMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(p, []byte(`{"model":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeUserConfig(p, []byte(`{"model":"y"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestSetupRequiresHomes(t *testing.T) {
	if _, err := Setup(SetupOpts{}); err == nil {
		t.Fatal("expected error")
	}
}
