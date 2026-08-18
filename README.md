<p align="center">
  <strong>lossless</strong><br>
  Agent memory for coding sessions
</p>

<p align="center">
  <a href="docs/README.md">Docs</a>
  ·
  <a href="https://github.com/jbgh/lossless/releases/latest">Releases</a>
  ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="Apache 2.0"></a>
  <a href="https://github.com/jbgh/lossless/releases/latest"><img src="https://img.shields.io/github/v/release/jbgh/lossless" alt="Release"></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/go-1.24-00ADD8?logo=go&logoColor=white" alt="Go"></a>
</p>

The session log is the memory. Compact is lossy. lossless is not.

# Introduction

lossless is a work-context index for coding agents. It copies the harness session file, builds a small index, and on `ask` returns at most five records for the current goal and files. Failed work first, then what already shipped.

Memory is not tied to a model or a harness. Claims are keyed by project (`owner/repo`). Switch tools on the same repo and `ask` still returns the same faileds and decisions.

```
ask({ question, project, paths, goal })
  → context + warnings
```

## Key features

- **Tape.** Hooks copy the session file into `~/.lossless/raw`. Full JSONL. Kept.
- **Claims.** A rebuildable index: failed, decision, constraint, state, thread. Not a substitute for the tape.
- **ask.** At most five records plus warnings. The agent writes the next reply. lossless does not.
- **Across models and harnesses.** One store per project. Grok, Claude, Codex, Pi, and OpenCode share it.
- **Local by default.** `127.0.0.1`, no token, nothing uploaded. `doctor` does not phone home.

# Compaction is lossy

A coding agent does not keep the whole session in mind. Each turn the *window* is rebuilt: system prompt, recent turns, whatever still fits. When that fills up, the harness **compacts**. Early work is replaced by a summary so the next call fits the context limit.

A summary is not the work. Failed approaches become a clause. A library choice becomes "picked something." A constraint disappears. Over a long project that happens again and again. Each compact is thinner than the last. A new session starts from the latest summary. A new model or a new harness has none of it. Burned work gets retried. A shipped decision gets undone. The model and the harness no longer have the record.

The session *file* on disk is still append-only. The window is not. lossless copies that file, builds an index, and lets the next turn check out what must not be forgotten.

Write is push (hooks on compact, stop, session end). Read is pull (`ask`). Compact hooks copy the tape. They do not inject a pack.

### Compact only

The harness still writes a session file. Compact keeps a summary in the window. After enough turns, or a new session, or a new model, the early record is gone.

```
turns --> compact --> thinner window --+
              ^                       |
              +-----------------------+
                                      |
                                      v
                            new session or model
                                      |
                                      v
                              early record gone
```

### Compact plus lossless

Compact still thins the window. Hooks copy the session file onto the tape. `ask` checks out at most five records and warnings for this goal and these files.

```
turns --+--> compact --> thinner window
        |
        v
      tape -----------------------+
                                  v
              thinner window --> ask --> five records
                                            |
                                            v
                                        agent acts
```

### Another harness, same repo

Same project store. A pack from one harness is visible to the next.

```
Grok   --+
Claude --+--> project store --> ask
Codex  --+
```

# Quickstart

### Tell the agent

Open any supported coding agent and paste this. The agent does the rest.

```
Install lossless from https://github.com/jbgh/lossless. lossless is agent memory
for coding sessions. Default install is this machine only (127.0.0.1:7432, no
token, nothing uploaded). Do not provision a cloud. Do not auto-inject packs.
Do not retune retrieve weights. Do not clobber the user's CLAUDE.md / AGENTS.md
outside the marked lossless section.

Supported OS: macOS and Linux, amd64 and arm64. Not Windows.

Supported harnesses (setup writes all of them, even if this session is only one):
  Grok, Claude Code, Codex, Pi, OpenCode.

Detect the current harness from the environment (GROK_SESSION_ID, CLAUDE_*,
CODEX_*, this process name, or how the user launched you). Still run full setup
so the other harnesses work later.

1. Binary
   If `lossless version` already works, skip to step 2 (or run `lossless update`
   if the user asked for latest).
   Else try the release installer:
     curl -fsSL https://github.com/jbgh/lossless/releases/latest/download/install.sh | sh
   That writes ~/.local/bin/lossless (replaces a dest symlink, does not write
   through it).
   If curl is blocked or the OS/arch is missing, clone and build (needs Go 1.24+):
     git clone --depth 1 https://github.com/jbgh/lossless.git /tmp/lossless
     cd /tmp/lossless && go build -o lossless ./cmd/lossless
     mkdir -p "$HOME/.local/bin" && mv lossless "$HOME/.local/bin/lossless"
   Put "$HOME/.local/bin" on PATH for this shell if `command -v lossless` fails.
   Confirm: lossless version

2. Setup
   Run: lossless setup && lossless doctor
   setup installs hooks, MCP, the lossless skill, an always-on rule, and a user
   service (launchd on macOS, systemd --user on Linux) and starts
   `lossless serve --watch`.
   Only pass --no-service if the user asked not to install a user unit.
   Only pass --no-start if the user asked not to start the daemon.
   Do not pass a remote LOSSLESS_URL unless the user already has a remote home
   and asked to point at it. Remote home is manual: https + LOSSLESS_TOKEN.
   A private release fork needs GITHUB_TOKEN or GH_TOKEN. The public repo does not.

3. Doctor
   doctor must show ok for binary, home, daemon, hooks, mcp, skills, rules.
   service may say "no unit; daemon is running" if --no-service was used.
   If daemon is down: lossless serve --watch  (or start the user service).
   If hooks/mcp/skills/rules are missing: run lossless setup again.
   Do not finish while daemon is FAIL.

4. Reload this harness so MCP and the skill attach
   The current session will not see new MCP tools until reload or a new session.
   Tell the user the line that matches how they are talking to you:
     Grok:       /hooks then r. /skills then r if this session is already open.
     Claude:     start a new Claude Code session (or restart Claude).
     Codex:      start a new Codex session (or restart the Codex app / CLI).
     Pi:         start a new Pi session.
     OpenCode:   start a new OpenCode session.
   If you cannot tell, say: start a new agent session so lossless MCP tools appear.

   What setup wrote (do not hand-edit unless doctor is FAIL):
     Grok     hooks ~/.grok/hooks/lossless.json
              MCP   ~/.grok/config.toml
              skill ~/.grok/skills/lossless/SKILL.md
              rule  ~/.grok/rules/lossless.md
     Claude   hooks ~/.claude/settings.json
              MCP   ~/.claude.json
              skill ~/.claude/skills/lossless/SKILL.md
              rule  ~/.claude/rules/lossless.md and a marked block in ~/.claude/CLAUDE.md
     Codex    hooks ~/.codex/hooks.json
              MCP   ~/.codex/config.toml
              skill ~/.codex/skills/lossless/SKILL.md
              rule  marked block in ~/.codex/AGENTS.md (or AGENTS.override.md if that
                    is already the active global)
     Pi       ext   ~/.pi/agent/extensions/lossless.ts
              MCP   ~/.pi/agent/mcp.json
              skill ~/.pi/agent/skills/lossless/SKILL.md
              rule  marked block in ~/.pi/agent/AGENTS.md
     OpenCode plugin ~/.config/opencode/plugins/lossless.ts
              MCP    ~/.config/opencode/opencode.json
              skill  ~/.config/opencode/skills/lossless/SKILL.md
              rule   marked block in ~/.config/opencode/AGENTS.md
     Also     ~/.agents/skills/lossless/SKILL.md
   Marked rule blocks are <!-- lossless:start --> ... <!-- lossless:end -->.
   Leave everything outside those markers alone.

5. How to use lossless after install
   MCP tools: ask, remember, get_record. Same JSON as POST http://127.0.0.1:7432/v1/ask.
   Before implementing, changing behavior, or continuing after compact, call ask:
     workspace_root: absolute repo path
     goal: one sentence, what you are about to do
     paths: repo-relative files in play
     question: the user's ask, or "what must I not forget"
     session_id: if the harness gave you one
   Treat warnings as blocking unless the user overrides.
   remember is only for a durable sentence the tape will miss. It does not
   replace catch-up.
   Do not wait for the user to type /lossless.
   Later upgrades: lossless update && lossless doctor

6. Confirm
   Print lossless version, lossless doctor, and which harness reload the user
   needs. Then stop. The current session may not see MCP until they reload.
```

### Manual

```bash
curl -fsSL https://github.com/jbgh/lossless/releases/latest/download/install.sh | sh
lossless setup
lossless doctor
```

That writes a real binary to `~/.local/bin/lossless` (a dest symlink is replaced, not followed), then hooks, MCP, a skill, and a user service. Start a new agent session (Grok: `/hooks` then `r`; `/skills` then `r` if the session is already open). Everything stays on this machine.

Binaries: macOS and Linux, amd64 and arm64.

```bash
lossless update          # later
lossless update --check
lossless version
```

`update` is the only command that calls GitHub. A token is only needed if `LOSSLESS_UPDATE_REPO` points at a private fork.

### From source

```bash
go test ./...
go build -o lossless ./cmd/lossless
./lossless setup
./lossless doctor
```

### Basic usage

```bash
lossless ask --project owner/repo --goal "what the agent is about to do" --path src/app.ts
lossless remember --type decision --text "..." --project owner/repo
lossless inspect --project owner/repo --ask
```

MCP tools (`ask`, `remember`, `get_record`) use the same JSON as `POST /v1/ask`. The skill tells the agent to call `ask` on real work without anyone typing `/lossless`.

| | Local (default) | From source | Remote home |
|---|-----------------|-------------|-------------|
| **Best for** | This machine | Contributors | A box lossless already runs on |
| **Setup** | `install.sh` then `lossless setup` | `go build` then `./lossless setup` | TLS + `LOSSLESS_URL` + `LOSSLESS_TOKEN` |
| **Store** | `~/.lossless` | same | copy `raw/` or start empty |
| **Network** | none, except `update` | none, except `update` | client to home only |

A remote home is documented and manual. lossless does not provision a cloud or ship the store. See [docs/deploy.md](docs/deploy.md).

# Commands

```bash
lossless setup              # hooks + MCP + skill + user service
lossless doctor             # daemon, hooks, MCP, service
lossless inspect            # tape vs claims vs last packs; --ask --jsonl --prune
lossless ask --project KEY [--question "..."] [--goal "..."] [--path FILE]
lossless remember --type decision --text "..."
lossless catch-up --jsonl FILE --workspace DIR --harness grok
lossless serve              # REST + /mcp; watches session files
lossless mcp                # stdio MCP (talks to the daemon)
lossless bench --root testdata/bench
```

# Integrations

`lossless setup` writes hooks, MCP, a skill, and a short always-on rule for:

| Harness | Hooks | MCP |
|---------|-------|-----|
| Grok | yes | yes |
| Claude | yes | yes |
| Codex | yes | yes |
| Pi | yes | yes |
| OpenCode | yes | yes |

Any other agent that can call authenticated `/mcp` or `POST /v1/ask` can use the same store. Claims stay on `owner/repo`, so a pack from one harness is visible to the next.

# What it isn't

- **Not a hosted fact cache.** Extracting facts with an LLM and throwing the transcript away loses the failed path.
- **Not a second brain.** No LLM on retrieve. No hosted embeddings. Ranking is a fixed packer.
- **Not a dump.** Five records, not the whole log.
- **Not auto-injection.** If the skill is ignored, the store is a diary.
- **Not a cloud.** Default install does not upload transcripts.
- **Not company RAG.** Memory for coding sessions on a repo.

# Documentation

- [docs/README.md](docs/README.md): index
- [docs/architecture.md](docs/architecture.md): loop and store
- [docs/algorithm.md](docs/algorithm.md): how retrieve picks five records
- [docs/pipeline.md](docs/pipeline.md): who asks, what happens after
- [docs/write.md](docs/write.md) · [docs/ask.md](docs/ask.md) · [docs/retrieval.md](docs/retrieval.md)
- [docs/harnesses.md](docs/harnesses.md) · [docs/deploy.md](docs/deploy.md) · [docs/stack.md](docs/stack.md)

# Data

```
~/.lossless/raw/<owner>__<repo>/2026-08/<session>.jsonl   # owned corpus (forever)
~/.lossless/export/<owner>__<repo>/{id}.md               # claims
~/.lossless/index/claims.sqlite                          # rebuildable index only
~/.lossless/index/excerpts-YYYY-MM.sqlite                # monthly excerpt windows
```

`LOSSLESS_HOME` overrides the data dir. SQLite is not the corpus. Default config makes no outbound network calls.

# License

Apache-2.0. See [LICENSE](LICENSE).
