# Harness adapters (write)

[Docs](README.md) · [write](write.md) · [deploy](deploy.md)

Yes: start with writes, and yes: treat Grok, Claude Code, Pi, omp, OpenCode, and Codex as **adapters**, not as six products.

The wrong version of that idea is six memory systems. The right version is one catch-up core ([write.md](write.md)) and a thin adapter per harness that answers only three questions:

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
    harness      = "grok" | "claude" | "pi" | "omp" | "opencode" | "codex",
    source       = turn | compact | session_end
  )
```

Installers differ. Catch-up does not.

Universal fallback, every harness: a **directory watch** on that harness's session root. If a compact hook misses or crashes, the watcher still copies new bytes. Compact **checkout** (`active/<owner__repo>.md`) is separate: PreCompact is the timely path; the watcher writes that file only when the new chunk contains `type=compaction` (Pi and omp, whose compaction is a JSONL entry). Grok compact does not write that line — Grok checkout stays the PreCompact hook.

---

## Capability matrix

| | Grok | Claude Code | Pi | omp | OpenCode | Codex |
|--|------|-------------|----|----|----------|-------|
| Session file we can tail | `~/.grok/sessions/<enc-cwd>/<id>/chat_history.jsonl` | `~/.claude/projects/<slug>/<id>.jsonl` (`transcript_path` on hooks). Watcher peeks `cwd` from the transcript; unknown-cwd files skip. Do not rewrite `cleanupPeriodDays`. | `~/.pi/agent/sessions/--<cwd / as ->--/<ts>_<uuid>.jsonl` | `~/.omp/agent/sessions/<bucket>/<ts>_<uuid>.jsonl`. Bucket: `-<home-relative>`, `-tmp-<temp-relative>`, else `--<absolute>--`. First line is a 256-byte title slot, so the cwd peek reads the `session` header. Subagent transcripts live in a sibling directory named after the session and skip. `PI_CONFIG_DIR` / `PI_CODING_AGENT_DIR` move the root. | SQLite `opencode.db` (`session`/`message`/`part`). Watcher dumps sessions the plugin missed. | CLI: `~/.codex/sessions/**/rollout-*.jsonl`. Desktop: `state_*.sqlite` threads; empty `rollout_path` still catch-up `first_user_message` when cwd is known. `sessions/` may be missing |
| As they go | `Stop` (`end_turn`) + `UserPromptSubmit` (write observe) | `Stop` + `UserPromptSubmit` (write observe, no retrieve) | extension `turn_end` / session events | extension `turn_end` | plugin `session.idle` | plugin **Stop** |
| Before compact | **`PreCompact`** (+ `PostCompact`) | **`PreCompact`** (+ `PostCompact`) | `session_before_compact` (sync wait) | `session_before_compact` (sync wait) | `experimental.session.compacting` (await) / `session.compacted` | **`PreCompact`** (+ `PostCompact`) |
| Session end | `SessionEnd` | `SessionEnd` | `session_end` | `session_shutdown` | `session.deleted` / idle | Stop + process exit |
| Hook shape | JSON stdin, camelCase (Claude files also load) | JSON stdin, snake_case | in-process TS extension | in-process TS extension | in-process TS plugin | Codex plugin hooks (Stop) |
| Cleanup risk | sessions kept | **`cleanupPeriodDays` default 30** | kept under `~/.pi` | kept under `~/.omp` | kept under XDG share | kept under `~/.codex` |
| Adapter risk | Low. We already know this. | Low. Best `transcript_path`. | Medium. Tree JSONL, not linear. Tail the file; do not walk the tree in v1. | Low. Same JSONL as Pi, plus a title slot and a bucket name that is not reversible (an id we were given alone is searched for under every bucket). | Medium-high. May need to serialize from the plugin API if storage is not JSONL. | Medium. Desktop may store threads in sqlite; watcher is still load-bearing. |

Install: `lossless setup`. That is hooks + MCP for all six and a health check. On macOS/Linux it can also write a *user* launchd/systemd unit so `serve` stays up on this login — not a cloud image. `install-hooks` / `install-mcp` are the pieces if you need only one. Any other harness: point MCP at `/mcp` or call REST ([deploy.md](deploy.md)).

| | Grok | Claude Code | Pi | omp | OpenCode | Codex |
|--|------|-------------|----|----|----------|-------|
| MCP config | `~/.grok/config.toml` HTTP `/mcp` | `~/.claude.json` stdio `lossless mcp` | `~/.pi/agent/mcp-adapter.json` stdio (pi-mcp-adapter 3.0+; `mcp.json` for older adapters) | `~/.omp/agent/mcp.json` stdio (omp's native file, which outranks the Claude and Codex configs it also reads) | `~/.config/opencode/opencode.json` local | `~/.codex/config.toml` stdio |
| Token | `headers.Authorization = Bearer ${LOSSLESS_TOKEN}` | inherit env | inherit env | inherit env | inherit env | inherit env |
| Skill | `~/.grok/skills/lossless/SKILL.md` | `~/.claude/skills/lossless/SKILL.md` | `~/.pi/agent/skills/lossless/SKILL.md` (+ `~/.agents/skills`) | `~/.omp/agent/skills/lossless/SKILL.md` (also sees `~/.claude/skills` and `~/.agents/skills`) | `~/.config/opencode/skills/lossless/SKILL.md` (also sees `~/.claude/skills` and `~/.agents/skills`) | `~/.codex/skills/lossless/SKILL.md` (+ `~/.agents/skills`) |
| Ask `session_id` | model sends it if the prompt shows it | MCP child env `CLAUDE_CODE_SESSION_ID`, taken only when the MCP server's parent process is `claude` (a Pi or OpenCode started from a Claude shell inherits the id). Subagents share the parent's MCP server and book to the parent session. | extension `tool_call` stamps `ctx.sessionManager.getSessionId()` on every lossless call | extension `tool_call` stamps `ctx.sessionManager.getSessionId()`. omp mounts MCP tools as `xd://` device paths, so the stamp rewrites the `content` JSON of a `write` to `xd://mcp__lossless_ask` / `_remember` / `_get_record`; a direct `mcp__lossless_*` tool name is stamped too. omp has no session-id env var for children. | plugin `tool.execute.before` stamps `input.sessionID` | model sends it if the prompt shows it |
| Always-on rule | `~/.grok/rules/lossless.md` | `~/.claude/CLAUDE.md` marked section + `~/.claude/rules/lossless.md` | `~/.pi/agent/AGENTS.md` marked section | `~/.omp/agent/RULES.md` marked section (omp's always-apply rule file; its `AGENTS.md` is not used because that is the one user *context* file omp keeps, so claiming it would shadow the user's other global instructions) | `~/.config/opencode/AGENTS.md` marked section | `~/.codex/AGENTS.md` marked section (`AGENTS.override.md` if that file is already the active global) |

The skill is when/how to call `ask`. The always-on rule is the one-liner that stays in session context so the model does not wait for `/lossless`. Setup upserts a marked `<!-- lossless:start -->` … `<!-- lossless:end -->` block and leaves the rest of the user's CLAUDE.md / AGENTS.md alone.

Install order: **Grok → Claude → Codex → Pi → omp → OpenCode**. That is popularity plus "do we have a file we can copy before the window dies."

---

The MCP server keeps a caller-sent `session_id` only when it has a harness shape (UUID, `ses_…`, `agent-<hex>`; a UUID with a `-<pid>` or `.partN` suffix trims to the UUID). A made-up name (`w22-ios`) is dropped, then the harness fill above applies (`LOSSLESS_SESSION_ID` first, for wrappers). Nothing is guessed from other sessions.

## Per harness

### Grok

- Locate: `GROK_HOME` or `~/.grok` + URL-encoded cwd + `sessionId` + **`chat_history.jsonl`**. Do not ingest `updates.jsonl` (ACP UI stream, ~2.5× larger). Grok 1.0.13 also runs the Claude-scope hooks in `~/.claude/settings.json` and passes `updates.jsonl` as `transcript_path`; `hook-claude` maps that to the sibling `chat_history.jsonl` with harness `grok`, and catch-up refuses `updates.jsonl` from any caller (0.1.25). Measured: this repo session was 1.4 MB history vs 3.5 MB updates; an 8-compact session was 17.6 MB history with **zero** compact summaries in the file. Compact shrinks the window, not the log.
- Fire: `Stop` (filter `reason == end_turn`), `PreCompact`, `SessionEnd`.
- Normalize: `{role, content|text}` and Claude-shaped `message.role`.
- Install: `~/.grok/hooks/lossless.json`.

### Claude Code

- Locate: hook field `transcript_path`. Do not guess if it is present. The watcher peeks `cwd` from user/attachment lines so Claude JSONL catch-up without a prior hook, including nested JSONL when cwd is in the transcript. `subagents/` and `agent-*.jsonl` dumps skip. The project-dir slug is not reversible. Unknown-cwd files still skip. Do not rewrite `cleanupPeriodDays`.
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

### omp (oh-my-pi)

- Locate: `~/.omp/agent/sessions/<bucket>/<timestamp>_<uuid>.jsonl`. The bucket comes from the canonicalized cwd: `-<home-relative>` under home, `-tmp-<temp-relative>` under the temp root, else `--<encoded-absolute>--` (so `/tmp` and `/private/tmp` share `--private-tmp--`). `PI_CONFIG_DIR` moves the config root, `PI_CODING_AGENT_DIR` the agent dir, and a profile uses `~/.omp/profiles/<name>/agent`. Written files open with a fixed-width 256-byte `type:"title"` slot, so the cwd peek reads the `session` header line, not line one. The bucket name is not reversible: a session id we were handed without a cwd is searched for under every bucket. Subagent transcripts sit in a sibling directory named after the session (`<ts>_<id>/<AgentName>.jsonl`) and are skipped, like Claude's `agent-*.jsonl` dumps.
- Fire: extension `turn_end` (as they go), `session_before_compact` (before compact, awaited), `session_compact`, `session_shutdown` (end). Compaction is also a JSONL `type:compaction` entry, and omp may rewrite the file body while pruning tool output, so the watcher and the reset-on-shrink cursor are load-bearing.
- Normalize: `type:message` + `message.role` (`user` / `assistant` / `toolResult` / `bashExecution` / `pythonExecution` / `developer` / `fileMention`). `toolResult`, `bashExecution`, and `pythonExecution` are tool output; a `fileMention` is the inlined text of an `@file` mention, which is not the user's typing. Skip the bookkeeping entries (`title`, `session`, `model_change`, `model_usage`, `thinking_level_change`, `service_tier_change`, `custom`, `custom_message`, `branch_summary`, `label`, `title_change`, `reset_boundary`, `ttsr_injection`, `credential_pin`, `session_init`, `mode_change`) and mark `compaction` as a compact.
- Install: `~/.omp/agent/extensions/lossless.ts` (spawns `hook-omp`, fail-open), MCP in `~/.omp/agent/mcp.json`, skill in `~/.omp/agent/skills/lossless/SKILL.md`, rule block in `~/.omp/agent/RULES.md`.

### OpenCode

- Locate: **no tail-able JSONL**. Live install is `~/.local/share/opencode/opencode.db` (Drizzle): `session.directory`, `message.data` (role), `part.data` (text / tool / reasoning). `storage/` is not a session log.
- Fire: plugin `session.idle`, `experimental.session.compacting`, `session.compacted`, `session.deleted`. The watcher lists `session` rows and dumps those whose `time_updated` is ahead of the cursor (16 per tick) so a missed plugin still copies the tape.
- Normalize: dump message+parts to `{role, content}` and catch-up. HTTP `POST /v1/catch-up` with `harness=opencode` + `session_id` reads the DB; `messages[]` is the no-file fallback. The dump is rendered whole each time and copied by byte cursor, so it only holds messages that can no longer change: it stops at the first assistant step without `time.completed` or `error` (a dead step counts once a later step exists). Text parts are prose; a tool part's `state.output` / `state.error` is a clipped `tool_result`; reasoning and empty steps are left out.
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

Do not implement six adapters before catch-up is real.

1. **Core catch-up** with owned `raw/`, cursors, redact, spool (write eval W1–W10).
2. **Grok** hooks + watcher on `~/.grok/sessions`.
3. **Claude** hooks (`transcript_path`).
4. **Watcher-only Codex** (then add Stop).
5. **Pi** extension.
6. **OpenCode** plugin.

Grok first because we can dogfood in this TUI the same day the core lands.

---

## Watcher (all six)

Poll or fs-events on each known session root, plus OpenCode `opencode.db` and Codex desktop `state_*.sqlite`. On file grow or sqlite `time_updated`: `catch_up`. Debounce 200ms. Ignore files we already sealed (cursor at EOF and mtime stale). Claude files with no peeked cwd skip (do not guess the slug).

This is how "everything is remembered" survives missing compact hooks and crashed hook processes. Hooks make it timely. The watcher makes it complete.
