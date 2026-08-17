package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkillsWritesGrokAndClaude(t *testing.T) {
	home := t.TempDir()
	paths, err := InstallSkills(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 5 {
		t.Fatal(paths)
	}
	body, err := os.ReadFile(filepath.Join(home, ".grok", "skills", "lossless", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, need := range []string{"name: lossless", "ask", "workspace_root", "when not to call"} {
		if !strings.Contains(strings.ToLower(s), strings.ToLower(need)) {
			t.Fatalf("missing %q in skill", need)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "lossless", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	rule, err := os.ReadFile(filepath.Join(home, ".grok", "rules", "lossless.md"))
	if err != nil || !strings.Contains(string(rule), "ask") {
		t.Fatalf("rule: %s %v", rule, err)
	}
}

func TestInstallSkillsRequiresHome(t *testing.T) {
	if _, err := InstallSkills(""); err == nil {
		t.Fatal("empty home")
	}
}
