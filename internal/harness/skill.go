package harness

import (
	_ "embed"
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
	dest := path
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		dest = target
	}
	var existing []byte
	if b, err := os.ReadFile(dest); err == nil {
		existing = b
	}
	return writeUserConfig(dest, upsertMarkedSection(existing, ruleBegin, ruleEnd, body), 0o644)
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
