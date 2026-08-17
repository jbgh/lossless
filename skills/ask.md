# lossless skill (source)

Canonical copy used by `lossless setup` lives in `internal/harness/skill.md`
and is written to each harness's native skill dir:

- `~/.grok/skills/lossless/SKILL.md`
- `~/.claude/skills/lossless/SKILL.md`
- `~/.codex/skills/lossless/SKILL.md`
- `~/.pi/agent/skills/lossless/SKILL.md`
- `~/.config/opencode/skills/lossless/SKILL.md`
- `~/.agents/skills/lossless/SKILL.md` (Codex / Pi / OpenCode also look here)

Always-on one-liner (`internal/harness/rule.md`) is upserted into
`~/.grok/rules/lossless.md`, `~/.claude/rules/lossless.md`,
`~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, `~/.pi/agent/AGENTS.md`,
and `~/.config/opencode/AGENTS.md`.

The model should call MCP `ask` on its own. Users should not have to type
`/lossless`. See that SKILL.md for when and how.
