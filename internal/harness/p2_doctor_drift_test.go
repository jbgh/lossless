package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doctor said "hooks ok" while the Claude hooks ran a different binary.
// A lossless hook whose command names another exe is drift, reported
// with the harness and the stale path.
func TestDoctorReportsHookTargetDrift(t *testing.T) {
	user := t.TempDir()
	data := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(user, ".config", "opencode"))
	if _, err := Setup(SetupOpts{UserHome: user, DataHome: data, Exe: "/bin/am", Service: false, Start: false}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(user, ".claude", "settings.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(b), `\"/bin/am\" hook-claude`, `\"/Users/jay/developer/lossless/lossless\" hook-claude`, 1)
	if stale == string(b) {
		t.Fatalf("fixture did not change:\n%s", b)
	}
	if err := os.WriteFile(p, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	rep := Doctor(user, data, "/bin/am", "http://127.0.0.1:1", "")
	var hooks Check
	for _, c := range rep.Checks {
		if c.Name == "hooks" {
			hooks = c
		}
	}
	if hooks.OK {
		t.Fatalf("drift not reported: %+v", hooks)
	}
	if !strings.Contains(hooks.Detail, "claude") || !strings.Contains(hooks.Detail, "/Users/jay/developer/lossless/lossless") {
		t.Fatalf("detail should name the harness and stale exe: %q", hooks.Detail)
	}
}
