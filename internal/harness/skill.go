package harness

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed skill.md
var skillMarkdown []byte

//go:embed rule.md
var ruleMarkdown []byte

func SkillMarkdown() []byte { return skillMarkdown }

func InstallSkills(home string) ([]string, error) {
	if home == "" {
		return nil, os.ErrInvalid
	}
	rels := []string{
		filepath.Join(".grok", "skills", "lossless", "SKILL.md"),
		filepath.Join(".claude", "skills", "lossless", "SKILL.md"),
		filepath.Join(".agents", "skills", "lossless", "SKILL.md"),
	}
	var out []string
	for _, rel := range rels {
		dest := filepath.Join(home, rel)
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
	rels := []string{
		filepath.Join(".grok", "rules", "lossless.md"),
		filepath.Join(".claude", "rules", "lossless.md"),
	}
	var out []string
	for _, rel := range rels {
		dest := filepath.Join(home, rel)
		if err := writeUserConfig(dest, ruleMarkdown, 0o644); err != nil {
			return out, err
		}
		out = append(out, dest)
	}
	return out, nil
}
