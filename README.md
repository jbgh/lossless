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

# The problem

Coding agents forget. Not because the model is weak, but because the harness keeps only a small *window* of the session in front of the model at each turn. When that window fills up, the harness **compacts**: early turns are replaced by a short summary so the next call fits the context limit. Over a long project this happens again and again.

Say an agent tries a Redis token bucket for rate limiting, fails in staging on connection-pool exhaustion, and ships a fixed-window counter instead. Four compacts later the window holds one clause — *"tried Redis, switched to fixed window."* A new session asks "why not Redis?" and gets nothing back. The token bucket is tried again, burns another afternoon, and fails the same way.

The same erosion hits every kind of past. A shipped decision gets re-litigated. A constraint the user stated once gets violated. A dead end becomes indistinguishable from a fresh idea. And none of it survives a new session, a new model, or a different tool.

The irony: the session *file* on disk is already append-only. The full record exists. Only the window forgets.

# What lossless is

lossless is a small local service that keeps the whole record and checks out what must not be forgotten.

- **A daemon watches the session files.** Grok, Claude Code, Codex, Pi, and OpenCode all write session logs to disk. Hooks copy each one onto an append-only **tape** in `~/.lossless/raw` — full JSONL, kept even if the harness deletes its own copy.
- **From the tape, lossless distills claims.** One-line records of failed work, decisions, constraints, states, and threads, each pointing back to the tape excerpt it came from. The index is rebuildable; the tape is the corpus.
- **The agent calls one tool, `ask`,** with what it is about to do and which files it will touch. It gets back at most five records plus warnings. The agent writes the next reply. lossless does not.

```
ask({ question, project, paths, goal })
  → context + warnings
```

Failed work packs first, then what already shipped. Warnings call out prior attempts at this goal, standing constraints, and recurring traps — including traps that share no vocabulary with the ask.

Memory is keyed by `owner/repo`, not by model or harness. Switch tools mid-project and `ask` returns the same faileds and decisions.

## What an ask looks like

```json
{
  "question": "what's already known about rate limiting on auth?",
  "project": "acme/api",
  "workspace_root": "/Users/you/dev/api",
  "goal": "add rate limiting",
  "paths": ["src/middleware/auth.ts"]
}
```

```json
{
  "context": [
    {
      "id": "01J...",
      "type": "failed",
      "text": "Redis token bucket failed in staging; connection pool exhausted.",
      "when": "2026-08-01T18:12:00Z",
      "paths": ["src/middleware/auth.ts"],
      "status": "active",
      "has_excerpt": true
    }
  ],
  "warnings": [
    "A prior attempt at this goal failed (see 01J...). Do not repeat it without new evidence."
  ],
  "tokens": 412,
  "project": "acme/api"
}
```

Each record is one of five types:

| `type` | What it holds |
|--------|---------------|
| `failed` | An approach that did not work, and why. Packed first — this is the expensive past. |
| `decision` | Something that shipped: a library, a shape, a settled tradeoff. |
| `constraint` | A rule stated once that must keep holding ("never touch `.woodpecker/**`"). |
| `state` | Where work stopped: a pending migration, a half-done refactor. |
| `thread` | An open question to return to. |

`id` is a citation. `get_record` opens the tape excerpt behind any record, so a one-line claim is never the whole story.

# Install

```bash
curl -fsSL https://github.com/jbgh/lossless/releases/latest/download/install.sh | sh
lossless setup
lossless doctor
```

That writes a real binary to `~/.local/bin/lossless` (a dest symlink is replaced, not followed), then hooks, MCP, a skill, and a user service — home rules only inside `<!-- lossless:start -->` … `<!-- lossless:end -->` markers. Start a new agent session so the MCP tools appear (Grok: `/hooks` then `r`; `/skills` then `r` if the session is already open). Everything stays on this machine.

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

### Where it runs

| | Local (default) | From source | Remote home |
|---|-----------------|-------------|-------------|
| **Best for** | This machine | Contributors | A box lossless already runs on |
| **Setup** | `install.sh` then `lossless setup` | `go build` then `./lossless setup` | TLS + `LOSSLESS_URL` + `LOSSLESS_TOKEN` |
| **Store** | `~/.lossless` | same | copy `raw/` or start empty |
| **Network** | none, except `update` | none, except `update` | client to home only |

A remote home is documented and manual. lossless does not provision a cloud or ship the store. See [docs/deploy.md](docs/deploy.md).

### Basic usage

After the new session starts, the agent calls `ask` on its own — no typing required. The same JSON is available three ways: MCP tools (`ask`, `remember`, `get_record`), `POST /v1/ask`, or the CLI:

```bash
lossless ask --project owner/repo --goal "what the agent is about to do" --path src/app.ts
lossless remember --type decision --text "..." --project owner/repo
lossless inspect --project owner/repo --ask
```

# How it works

### Compact only

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

One store per project. A pack written by one harness is visible to the next.

```
Grok   --+
Claude --+--> project store --> ask
Codex  --+
```

Write is push (hooks on compact, stop, prompt submit, session end). Read is pull (`ask`). Hooks copy the tape. They do not inject a pack.

# Why not just a summary?

| | The harness | lossless |
|---|---|---|
| Keeps | a summary that shrinks each compact | the full session file, forever |
| Knows what **failed** | usually gone | failed work packs first |
| Survives a new session | no — starts from the latest summary | yes |
| Survives a new model or tool | no | yes — memory is keyed by `owner/repo`, not by model or harness |

# Key features

- **Tape.** Full session JSONL, append-only, in `~/.lossless/raw`. Kept even if the harness deletes its own copy.
- **Claims.** A rebuildable index of failed, decision, constraint, state, and thread records. Not a substitute for the tape — a lossless re-derivable view of it.
- **`ask`.** At most five records plus warnings, sized to a token budget (default 1200). Warnings include prior attempts at this goal, shipped decisions to check first, standing constraints, and recurrence traps — a trap is detected in the store, not in the wording, so an ask that shares no vocabulary with a standing failure can still be warned.
- **Across models and harnesses.** Grok, Claude Code, Codex, Pi, and OpenCode write to one store. Switch tools on the same repo and `ask` still returns the same faileds and decisions.
- **Local by default.** `127.0.0.1`, no token, nothing uploaded. No LLM on retrieve, no hosted embeddings. `doctor` does not phone home. `update` is the only command that calls GitHub.
- **Backup is yours.** Optional `lossless backup` copies the store to an S3-compatible bucket you own (AWS, R2, B2, MinIO) — encrypted with a key you hold, on a schedule, generations restorable with `lossless restore`. `ask` still reads local files. See [docs/deploy.md](docs/deploy.md).

# Commands

```bash
lossless setup              # hooks + MCP + skill + user service
lossless doctor             # daemon, hooks, MCP, service, backup
lossless inspect            # tape vs claims vs last packs; --project --ask --jsonl --prune

lossless ask --project KEY [--question "..."] [--goal "..."] [--path FILE] [--session ID] [--workspace DIR]
lossless remember --type decision --text "..."
lossless catch-up --jsonl FILE --workspace DIR --harness grok

lossless serve [--listen 127.0.0.1:7432] [--token TOKEN]  # REST + /mcp; watches session files (--watch=false to only serve)
lossless mcp                # stdio MCP (talks to the daemon)
lossless watch              # poll harness session files
lossless ensure             # replay the spool after the daemon was down

lossless backup init s3://BUCKET/PREFIX [--endpoint URL] [--region R] [--every 1h] [--keep 5]
lossless backup [--dry-run] [--verbose] [--take-over]  # one incremental run
lossless restore [--at GEN] [--list] [--force]  # pull a generation into an empty store

lossless embed-backfill     # embed active claims if a local embedder is configured
lossless bench --root testdata/bench
lossless update
lossless version
lossless token              # print a random bearer (for non-loopback serve)
```

Hooks (`hook-grok`, `hook-claude`, `hook-codex`, `hook-pi`, `hook-opencode`) and `install-hooks` / `install-mcp` exist for setup and debugging; `setup` wires them.

# Integrations

`lossless setup` writes hooks, MCP, a skill, and a short always-on rule for:

| Harness | Hooks | MCP |
|---------|-------|-----|
| Grok | yes | yes |
| Claude Code | yes | yes |
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
- [docs/eval.md](docs/eval.md) · [docs/roadmap.md](docs/roadmap.md)

# Data

```
~/.lossless/raw/<owner>__<repo>/2026-08/<session>.jsonl   # owned corpus (forever)
~/.lossless/export/<owner>__<repo>/{id}.md                # claims
~/.lossless/active/<owner>__<repo>.md                     # checkout left at compact
~/.lossless/index/claims.sqlite                           # rebuildable index only
~/.lossless/index/excerpts-YYYY-MM.sqlite                 # monthly excerpt windows
~/.lossless/spool                                         # hook writes while the daemon is down
~/.lossless/backup.env · backup.key                       # backup config + encryption key (if enabled)
```

`LOSSLESS_HOME` overrides the data dir. SQLite is not the corpus. Default config makes no outbound network calls.

# License

Apache-2.0. See [LICENSE](LICENSE).
