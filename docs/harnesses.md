# Harness adapters (write)

[Docs](README.md) · [write](write.md) · [deploy](deploy.md)

Yes: start with writes, and yes: treat Grok, Claude Code, Pi, OpenCode, and Codex as **adapters**, not as five products.

The wrong version of that idea is five memory systems. The right version is one catch-up core ([write.md](write.md)) and a thin adapter per harness that answers only three questions:

1. **Where** is the session file?
2. **When** do we catch up? (map native events → `turn` | `compact` | `session_end`)
3. **What** does a line look like? (normalize to `{role, text, error, offset}`)

If a harness cannot answer (1), the adapter may POST already-normalized messages. It still must not rank, store, or redact differently.

Do not read harness memory products (`MEMORY.md`, `/flush`, `/dream`, `memory_search`, `parent_session_id`, Claude `additionalContext`). Compact checkout uses owned `raw/`. If the live session file is rewritten or deleted, the tape is still there.

---

## Shared contract

```
adapter.on(native_event):
  loc = locate_session(event)          # path + session_id + cwd
  catch_up(
    harness_path = loc.path,           # or messages[] if no file
    project_key  = from(loc.cwd),
    session_id   = loc.session_id,
    workspace    = loc.cwd,
    harness      = "grok" | "claude" | "pi" | "opencode" | "codex",
    source       = turn | compact | session_end
  )
```

Installers differ. Catch-up does not.

Universal fallback, every harness: a **directory watch** on that harness's session root. If a compact hook misses or crashes, the watcher still copies new bytes. Compact **checkout** (`active/<owner__repo>.md`) is separate: PreCompact is the timely path; the watcher writes that file only when the new chunk contains `type=compaction` (Pi). Grok compact does not write that line — Grok checkout stays the PreCompact hook.

---

## Capability matrix

| | Grok | Claude Code | Pi | OpenCode | Codex |
|--|------|-------------|----|----------|-------|
| Session file we can tail | `~/.grok/sessions/<enc-cwd>/<id>/chat_history.jsonl` | `~/.claude/projects/<slug>/<id>.jsonl` (`transcript_path` on hooks). Watcher peeks `cwd` from the transcript; unknown-cwd files skip. Do not rewrite `cleanupPeriodDays`. | `~/.pi/agent/sessions/--<cwd / as ->--/<ts>_<uuid>.jsonl` | SQLite `opencode.db` (`session`/`message`/`part`). Watcher dumps sessions the plugin missed. | CLI: `~/.codex/sessions/**/rollout-*.jsonl`. Desktop: `state_*.sqlite` threads; empty `rollout_path` still catch-up `first_user_message` when cwd is known. `sessions/` may be missing |
| As they go | `Stop` (`end_turn`) + `UserPromptSubmit` (write observe) | `Stop` + `UserPromptSubmit` (write observe, no retrieve) | extension `turn_end` / session events | plugin `session.idle` | plugin **Stop** |
| Before compact | **`PreCompact`** (+ `PostCompact`) | **`PreCompact`** (+ `PostCompact`) | `session_before_compact` (sync wait) | `experimental.session.compacting` (await) / `session.compacted` | **`PreCompact`** (+ `PostCompact`) |
| Session end | `SessionEnd` | `SessionEnd` | `session_end` | `session.deleted` / idle | Stop + process exit |
| Hook shape | JSON stdin, camelCase (Claude files also load) | JSON stdin, snake_case | in-process TS extension | in-process TS plugin | Codex plugin hooks (Stop) |
| Cleanup risk | sessions kept | **`cleanupPeriodDays` default 30** | kept under `~/.pi` | kept under XDG share | kept under `~/.codex` |
| Adapter risk | Low. We already know this. | Low. Best `transcript_path`. | Medium. Tree JSONL, not linear. Tail the file; do not walk the tree in v1. | Medium-high. May need to serialize from the plugin API if storage is not JSONL. | Medium. Desktop may store threads in sqlite; watcher is still load-bearing. |

Install: `lossless setup`. That is hooks + MCP for all five and a health check. On macOS/Linux it can also write a *user* launchd/systemd unit so `serve` stays up on this login — not a cloud image. `install-hooks` / `install-mcp` are the pieces if you need only one. Any other harness: point MCP at `/mcp` or call REST ([deploy.md](deploy.md)).

| | Grok | Claude Code | Pi | OpenCode | Codex |
|--|------|-------------|----|----------|-------|
| MCP config | `~/.grok/config.toml` HTTP `/mcp` | `~/.claude.json` stdio `lossless mcp` | `~/.pi/agent/mcp.json` stdio | `~/.config/opencode/opencode.json` local | `~/.codex/config.toml` stdio |
| Token | `headers.Authorization = Bearer ${LOSSLESS_TOKEN}` | inherit env | inherit env | inherit env | inherit env |
| Skill | `~/.grok/skills/lossless/SKILL.md` | `~/.claude/skills/lossless/SKILL.md` | `~/.pi/agent/skills/lossless/SKILL.md` (+ `~/.agents/skills`) | `~/.config/opencode/skills/lossless/SKILL.md` (also sees `~/.claude/skills` and `~/.agents/skills`) | `~/.codex/skills/lossless/SKILL.md` (+ `~/.agents/skills`) |
| Always-on rule | `~/.grok/rules/lossless.md` | `~/.claude/CLAUDE.md` marked section + `~/.claude/rules/lossless.md` | `~/.pi/agent/AGENTS.md` marked section | `~/.config/opencode/AGENTS.md` marked section | `~/.codex/AGENTS.md` marked section (`AGENTS.override.md` if that file is already the active global) |

The skill is when/how to call `ask`. The always-on rule is the one-liner that stays in session context so the model does not wait for `/lossless`. Setup upserts a marked `<!-- lossless:start -->` … `<!-- lossless:end -->` block and leaves the rest of the user's CLAUDE.md / AGENTS.md alone.

Install order: **Grok → Claude → Codex → Pi → OpenCode**. That is popularity plus "do we have a file we can copy before the window dies."

---

## Per harness

### Grok

- Locate: `GROK_HOME` or `~/.grok` + URL-encoded cwd + `sessionId` + **`chat_history.jsonl`**. Do not ingest `updates.jsonl` (ACP UI stream, ~2.5× larger). Measured: this repo session was 1.4 MB history vs 3.5 MB updates; an 8-compact session was 17.6 MB history with **zero** compact summaries in the file. Compact shrinks the window, not the log.
- Fire: `Stop` (filter `reason == end_turn`), `PreCompact`, `SessionEnd`.
- Normalize: `{role, content|text}` and Claude-shaped `message.role`.
- Install: `~/.grok/hooks/lossless.json`.

### Claude Code

- Locate: hook field `transcript_path`. Do not guess if it is present. The watcher peeks `cwd` from user/attachment lines so Claude JSONL catch-up without a prior hook. The project-dir slug is not reversible. Unknown-cwd files still skip. Do not rewrite `cleanupPeriodDays`.
- Fire: `Stop`, `UserPromptSubmit` (write observe only), `PreCompact`, `SessionEnd`. No `additionalContext`.
- Normalize: `message.role` / `message.content` (string or parts).
- Install: `~/.claude/settings.json` hooks (user scope) so it is not per-repo.
- Why this is second: same hook names as Grok, best path field, and the 30-day delete makes owned raw mandatory.

### Codex

- Locate: hook `transcript_path` when present. Else `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl`. Desktop app stores threads in `~/.codex/state_*.sqlite`. `sessions/` can be missing. Watcher catch-up a thread with cwd and `first_user_message` when `rollout_path` is empty or the file is gone. Peek `session_meta` / `SessionMeta` for cwd when a rollout exists.
- Fire: **Stop**, **SessionEnd**, and **PreCompact** (Codex hooks now include PreCompact / PostCompact; confirmed in current hook docs). Watcher is still load-bearing.
- Normalize: rollout `event_msg` / `response_item` payload types.
- Install: `~/.codex/hooks.json` (Stop / PreCompact / SessionEnd) plus `[features] hooks = true` if no `[features]` block exists. Hooks are on by default.

### Pi

- Locate: `~/.pi/agent/sessions/--<cwd with / as ->--/<timestamp>_<uuid>.jsonl` (earendil-works/pi `session-format.md`). Header line is `type:session` with `cwd`. Extensions: `ctx.sessionManager.getSessionFile()` / `getSessionId()`.
- Fire: extension `turn_end` / `agent_settled` (as they go), `session_before_compact` (before compact), `session_shutdown` (end). Compaction is also a JSONL `type:compaction` entry; tailing is enough if the hook misses.
- Normalize: `type:message` + `message.role` (`user` / `assistant` / `toolResult` / `bashExecution`). Skip `session`, `compaction`, `custom`, `branch_summary`, tree metadata. Do not walk the tree; ingest new lines.
- Install: `~/.pi/agent/extensions/lossless.ts` (spawns `hook-pi`, fail-open).

### OpenCode

- Locate: **no tail-able JSONL**. Live install is `~/.local/share/opencode/opencode.db` (Drizzle): `session.directory`, `message.data` (role), `part.data` (text / tool / reasoning). `storage/` is not a session log.
- Fire: plugin `session.idle`, `experimental.session.compacting`, `session.compacted`, `session.deleted`. The watcher lists `session` rows and dumps those whose `time_updated` is ahead of the cursor (16 per tick) so a missed plugin still copies the tape.
- Normalize: dump message+parts to `{role, content}` and catch-up. HTTP `POST /v1/catch-up` with `harness=opencode` + `session_id` reads the DB; `messages[]` is the no-file fallback.
- Install: `~/.config/opencode/plugins/lossless.ts` (auto-loaded).

OpenCode is last because the on-disk format is SQLite, not a file we can tail. The core still does not change.

---

## What is *not* a harness adapter

- Retrieval / `ask` — one API, all models.
- Redaction, raw layout, partitions, zstd — core write path.
- `project_key` — core, from `cwd`.
- "How Grok remembers vs how Claude remembers" — there is no such split.

A new harness is: locate + event map + line parser + installer. If it takes more than that, the core is leaking.

---

## v1 slice for writes

Do not implement five adapters before catch-up is real.

1. **Core catch-up** with owned `raw/`, cursors, redact, spool (write eval W1–W10).
2. **Grok** hooks + watcher on `~/.grok/sessions`.
3. **Claude** hooks (`transcript_path`).
4. **Watcher-only Codex** (then add Stop).
5. **Pi** extension.
6. **OpenCode** plugin.

Grok first because we can dogfood in this TUI the same day the core lands.

---

## Watcher (all five)

Poll or fs-events on each known session root, plus OpenCode `opencode.db` and Codex desktop `state_*.sqlite`. On file grow or sqlite `time_updated`: `catch_up`. Debounce 200ms. Ignore files we already sealed (cursor at EOF and mtime stale). Claude files with no peeked cwd skip (do not guess the slug).

This is how "everything is remembered" survives missing compact hooks and crashed hook processes. Hooks make it timely. The watcher makes it complete.
