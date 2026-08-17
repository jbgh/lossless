package harness

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed skill.md
var skillMarkdown []byte

//go:embed rule.md
var ruleMarkdown []byte

const (
	ruleBegin = "<!-- lossless:start -->"
	ruleEnd   = "<!-- lossless:end -->"
)

func SkillMarkdown() []byte { return skillMarkdown }

func openCodeUserDir(home string) string {
	if cfg := os.Getenv("OPENCODE_CONFIG"); cfg != "" {
		return cfg
	}
	return filepath.Join(home, ".config", "opencode")
}

// CodexAgentsPath is the global file Codex actually loads.
// AGENTS.override.md wins when it exists and is non-empty.
func CodexAgentsPath(home string) string {
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	if st, err := os.Stat(override); err == nil && st.Size() > 0 {
		return override
	}
	return filepath.Join(home, ".codex", "AGENTS.md")
}

func SkillDests(home string) map[string]string {
	return map[string]string{
		"grok":     filepath.Join(home, ".grok", "skills", "lossless", "SKILL.md"),
		"claude":   filepath.Join(home, ".claude", "skills", "lossless", "SKILL.md"),
		"agents":   filepath.Join(home, ".agents", "skills", "lossless", "SKILL.md"),
		"codex":    filepath.Join(home, ".codex", "skills", "lossless", "SKILL.md"),
		"pi":       filepath.Join(home, ".pi", "agent", "skills", "lossless", "SKILL.md"),
		"opencode": filepath.Join(openCodeUserDir(home), "skills", "lossless", "SKILL.md"),
	}
}

func RuleDests(home string) map[string]string {
	return map[string]string{
		"grok":     filepath.Join(home, ".grok", "rules", "lossless.md"),
		"claude":   filepath.Join(home, ".claude", "CLAUDE.md"),
		"codex":    CodexAgentsPath(home),
		"pi":       filepath.Join(home, ".pi", "agent", "AGENTS.md"),
		"opencode": filepath.Join(openCodeUserDir(home), "AGENTS.md"),
	}
}

func InstallSkills(home string) ([]string, error) {
	if home == "" {
		return nil, os.ErrInvalid
	}
	order := []string{"grok", "claude", "agents", "codex", "pi", "opencode"}
	dests := SkillDests(home)
	var out []string
	for _, name := range order {
		dest := dests[name]
		if err := writeUserConfig(dest, skillMarkdown, 0o644); err != nil {
			return out, err
		}
		out = append(out, dest)
	}
	rules, err := InstallRules(home)
	if err != nil {
		return out, err
	}
	return append(out, rules...), nil
}

func InstallRules(home string) ([]string, error) {
	if home == "" {
		return nil, os.ErrInvalid
	}
	var out []string
	dedicated := []string{
		filepath.Join(home, ".grok", "rules", "lossless.md"),
		filepath.Join(home, ".claude", "rules", "lossless.md"),
	}
	for _, dest := range dedicated {
		if err := writeUserConfig(dest, ruleMarkdown, 0o644); err != nil {
			return out, err
		}
		out = append(out, dest)
	}
	// Always-on files the harness loads every session. Marked upsert so we
	// do not clobber the user's own CLAUDE.md / AGENTS.md.
	for _, dest := range []string{
		filepath.Join(home, ".claude", "CLAUDE.md"),
		CodexAgentsPath(home),
		filepath.Join(home, ".pi", "agent", "AGENTS.md"),
		filepath.Join(openCodeUserDir(home), "AGENTS.md"),
	} {
		if err := upsertMarkedFile(dest, ruleMarkdown); err != nil {
			return out, err
		}
		out = append(out, dest)
	}
	return out, nil
}

func upsertMarkedFile(path string, body []byte) error {
	dest, err := resolveMarkedTarget(path)
	if err != nil {
		return err
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	return writeUserConfig(dest, upsertMarkedSection(existing, ruleBegin, ruleEnd, body), 0o644)
}

// resolveMarkedTarget follows a symlink only when the target is a regular
// file under $HOME and not a secret path. Dedicated skill/hook files use
// writeUserConfig, which replaces a symlink instead of writing through it.
func resolveMarkedTarget(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("not a regular file: %s", path)
		}
		return path, nil
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	target = filepath.Clean(target)
	if !underHome(target) {
		return "", fmt.Errorf("refusing to write through symlink outside home")
	}
	if sensitiveConfigTarget(target) {
		return "", fmt.Errorf("refusing to write through symlink to sensitive path")
	}
	tfi, err := os.Lstat(target)
	if err != nil {
		return "", fmt.Errorf("refusing to write through dangling symlink")
	}
	if tfi.Mode()&os.ModeSymlink != 0 || !tfi.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to write through symlink to non-file")
	}
	return target, nil
}

func underHome(path string) bool {
	home := os.Getenv("HOME")
	if home == "" {
		return false
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	home = filepath.Clean(home)
	path = filepath.Clean(path)
	sep := string(os.PathSeparator)
	return path == home || strings.HasPrefix(path, home+sep)
}

func sensitiveConfigTarget(path string) bool {
	n := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	for _, s := range []string{"/.ssh/", "/.gnupg/", "/.aws/", "/.netrc"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	base := filepath.Base(n)
	switch base {
	case "id_rsa", "id_ed25519", "authorized_keys", "known_hosts", "credentials", ".env":
		return true
	}
	return strings.HasSuffix(base, ".pem")
}

func upsertMarkedSection(existing []byte, begin, end string, body []byte) []byte {
	inner := strings.TrimSpace(string(body))
	block := begin + "\n" + inner + "\n" + end + "\n"
	s := string(existing)
	start := strings.Index(s, begin)
	if start >= 0 {
		afterBegin := start + len(begin)
		stopRel := strings.Index(s[afterBegin:], end)
		if stopRel >= 0 {
			stop := afterBegin + stopRel + len(end)
			prefix := strings.TrimRight(s[:start], "\n")
			suffix := strings.TrimLeft(s[stop:], "\n")
			var b strings.Builder
			if prefix != "" {
				b.WriteString(prefix)
				b.WriteString("\n\n")
			}
			b.WriteString(block)
			if suffix != "" {
				b.WriteString(suffix)
				if !strings.HasSuffix(suffix, "\n") {
					b.WriteByte('\n')
				}
			}
			return []byte(b.String())
		}
	}
	if strings.TrimSpace(s) == "" {
		return []byte(block)
	}
	return []byte(strings.TrimRight(s, "\n") + "\n\n" + block)
}
