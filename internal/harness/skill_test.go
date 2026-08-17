package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkillsWritesEveryHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(home, ".config", "opencode"))
	paths, err := InstallSkills(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 12 {
		t.Fatalf("wrote %d: %v", len(paths), paths)
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
	for _, rel := range []string{
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
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Fatal(rel, err)
		}
	}
	rule, err := os.ReadFile(filepath.Join(home, ".grok", "rules", "lossless.md"))
	if err != nil || !strings.Contains(string(rule), "ask") {
		t.Fatalf("rule: %s %v", rule, err)
	}
	claude, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claude), ruleBegin) || !strings.Contains(string(claude), "ask") {
		t.Fatalf("CLAUDE.md missing marked rule: %s", claude)
	}
}

func TestInstallSkillsRequiresHome(t *testing.T) {
	if _, err := InstallSkills(""); err == nil {
		t.Fatal("empty home")
	}
}

func TestInstallRulesPreservesUserAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(home, ".config", "opencode"))
	agents := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(agents), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "# mine\n\n- use pnpm\n"
	if err := os.WriteFile(agents, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallRules(home); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallRules(home); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "use pnpm") {
		t.Fatalf("lost user text: %s", s)
	}
	if strings.Count(s, ruleBegin) != 1 {
		t.Fatalf("duplicated block: %s", s)
	}
	if !strings.Contains(s, "ask") {
		t.Fatalf("missing rule: %s", s)
	}
}

func TestInstallRulesUsesCodexOverrideWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCODE_CONFIG", filepath.Join(home, ".config", "opencode"))
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte("# override\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallRules(home); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(override)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), ruleBegin) {
		t.Fatalf("override not upserted: %s", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "AGENTS.md")); err == nil {
		t.Fatal("must not create AGENTS.md when override is active")
	}
}

func TestUpsertMarkedSection(t *testing.T) {
	body := []byte("# lossless\ncall ask\n")
	if got := string(upsertMarkedSection(nil, ruleBegin, ruleEnd, body)); !strings.Contains(got, "call ask") {
		t.Fatal(got)
	}
	existing := []byte("# prefs\n\n- tabs\n")
	once := upsertMarkedSection(existing, ruleBegin, ruleEnd, body)
	twice := upsertMarkedSection(once, ruleBegin, ruleEnd, []byte("# lossless\ncall ask again\n"))
	s := string(twice)
	if strings.Count(s, ruleBegin) != 1 || !strings.Contains(s, "tabs") || !strings.Contains(s, "call ask again") {
		t.Fatal(s)
	}
	if strings.Contains(s, "call ask\n") && !strings.Contains(s, "call ask again") {
		t.Fatal(s)
	}
	orphan := []byte("keep\n" + ruleBegin + "\nold\n")
	appended := string(upsertMarkedSection(orphan, ruleBegin, ruleEnd, body))
	if !strings.Contains(appended, "keep") || !strings.Contains(appended, "old") || strings.Count(appended, ruleBegin) != 2 {
		t.Fatal(appended)
	}
}

func TestUpsertMarkedFileFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	target := filepath.Join(dir, "AGENTS.md")
	link := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(target, []byte("# shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := upsertMarkedFile(link, []byte("# lossless\nask\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("replaced symlink")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "shared") || !strings.Contains(string(got), "ask") {
		t.Fatal(string(got))
	}
}

func TestUpsertMarkedFileRefusesSymlinkOutsideHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "CLAUDE.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := upsertMarkedFile(link, []byte("# lossless\nask\n")); err == nil {
		t.Fatal("expected refuse")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "ask") {
		t.Fatal("wrote through symlink")
	}
}

func TestUpsertMarkedFileRefusesSensitiveTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(ssh, "authorized_keys")
	if err := os.WriteFile(target, []byte("ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "CLAUDE.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := upsertMarkedFile(link, []byte("# lossless\nask\n")); err == nil {
		t.Fatal("expected refuse")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "ask") {
		t.Fatal("wrote through sensitive symlink")
	}
}
