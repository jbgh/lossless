# lossless

Agent memory for coding sessions.

The session log is the memory. Compact is lossy. lossless is not.

## Why compact forgets

A coding agent does not keep the whole session in mind. Each turn the *window* is rebuilt: system prompt, recent turns, whatever still fits. When that fills up, the harness **compacts** — early work is replaced by a summary so the next call fits the context limit.

A summary is not the work. Failed approaches become a clause. A library choice becomes “picked something.” A constraint disappears. Over a long project that happens again and again. Each compact is thinner than the last. A new session starts from the latest summary. A new model or a new harness has none of it. That is how burned work gets retried, and how a shipped decision gets undone: the model and the harness simply do not have the record anymore.

The session *file* on disk is still append-only. The window is not. lossless copies that file, derives a small index, and lets the next turn check out what must not be forgotten.

## What lossless is

Memory for *this* kind of work: one repo, one problem, a trail of tries. The agent calls `ask` with what it is about to do. lossless returns up to five records and a few warnings — failed first, then what already shipped. Transcripts stay on this machine. Apache-2.0.

**0.1.0** is the first public cut — [release notes](CHANGELOG.md), [GitHub Releases](https://github.com/jbgh/lossless/releases/tag/v0.1.0).

```
ask({ question, project, paths, goal })
  → context + warnings
```

- **The tape.** Hooks copy the harness session file into `~/.lossless/raw`. That copy is the corpus. Full JSONL. Kept.
- **Claims.** A small derived index — failed, decision, constraint, state, thread. Rebuildable. Not a substitute for the tape.
- **ask.** The checkout: at most five records for *this* goal and these files. Warnings if a failed approach is about to be repeated. The agent writes the next reply; lossless does not.

Same shape as a log stream or git: keep the event log, build indexes, check out a few records. Do not paste the stream into the next prompt.

Write is push (hooks on compact, stop, session end). Read is pull (`ask`). Nothing is retrieved until someone asks. Compact hooks copy the tape; they do not inject a pack.

## Across models and harnesses

lossless is model-agnostic and harness-agnostic. Claims are keyed by project (`owner/repo`), not by Grok, Claude, Codex, Pi, or OpenCode, and not by which model ran the turn.

Switch harnesses or models on the same repo and `ask` still returns the same faileds and decisions. The action tape — what *this* session already asked — stays on that session so a thin continue does not inherit another agent’s last pack.

`lossless setup` writes hooks, MCP, a skill, and a short rule for every supported harness so the agent calls `ask` without anyone typing `/lossless`.

Docs: [index](docs/README.md), [architecture](docs/architecture.md), [algorithm](docs/algorithm.md), [pipeline](docs/pipeline.md), [deploy](docs/deploy.md), [write](docs/write.md), [ask](docs/ask.md), [retrieval](docs/retrieval.md), [harnesses](docs/harnesses.md), [stack](docs/stack.md).

## What it isn't

- **Not a hosted fact cache.** Tools like Mem0 extract facts with an LLM and throw the transcript away. That is a good cache of preferences. It is a bad record of work: the failed path disappears into a summary, and it cannot be replayed.
- **Not a second brain.** There is no LLM inside retrieve. No hosted embeddings. Ranking is a fixed packer, the same for every model. Missing an on-box embedder is degraded mode, not a hard failure.
- **Not a dump.** lossless will not stuff months of session text into the window. Five records. Failed and shipped first — not a grep over ten years of `cat`.
- **Not auto-injection.** lossless does not nag, and does not paste memory into every turn. If the skill is ignored, the store is a diary.
- **Not a cloud.** Default install is this machine (`127.0.0.1`, no token, nothing uploaded). lossless does not host transcripts. A remote home is documented and manual.
- **Not company RAG.** Not a wiki, a ticket search, or a general knowledge base. Memory for coding sessions on a repo.

## Install

```bash
curl -fsSL https://github.com/jbgh/lossless/releases/latest/download/install.sh | sh
lossless setup
lossless doctor
```

That writes a real binary to `~/.local/bin/lossless` (a dest symlink is replaced, not followed), then hooks, MCP, a skill, and a user service. Start a new agent session (Grok: `/hooks` then `r`; `/skills` then `r` if the session is already open). Everything stays on this machine.

Binaries: macOS and Linux, amd64 and arm64.

```bash
lossless update          # later: pull the latest release and retarget hooks/service
lossless update --check  # look only
lossless version
```

`update` is the only command that calls GitHub. `doctor` and the daemon do not phone home. The repo is public; a token is only needed if `LOSSLESS_UPDATE_REPO` points at a private fork.

From source:

```bash
go test ./...
make cover
go build -o lossless ./cmd/lossless
./lossless setup
./lossless doctor
```

## Commands

```bash
lossless setup              # hooks + MCP + skill + user service
lossless doctor             # daemon, hooks, MCP, service
lossless inspect            # tape vs claims vs last packs; --project KEY --ask --jsonl FILE --prune
lossless ask --project KEY [--question "..."] [--goal "..."] [--path FILE]
lossless remember --type decision --text "..."
lossless catch-up --jsonl FILE --workspace DIR --harness grok
lossless serve              # REST + /mcp; watches session files
lossless mcp                # stdio MCP (talks to the daemon)
lossless bench --root testdata/bench
```

A remote home is optional and manual: run `serve` on any box, put TLS in front, set `LOSSLESS_URL` + `LOSSLESS_TOKEN`, point MCP at `/mcp`. lossless does not provision a cloud or ship the store. See [docs/deploy.md](docs/deploy.md).

## Data

Data dir: `~/.lossless/` (`LOSSLESS_HOME` to override).

```
~/.lossless/raw/<owner>__<repo>/2026-08/<session>.jsonl   # owned corpus (forever)
~/.lossless/export/<owner>__<repo>/{id}.md               # claims
~/.lossless/index/claims.sqlite                          # rebuildable index only
~/.lossless/index/excerpts-YYYY-MM.sqlite                # monthly excerpt windows
```

Default config makes **zero outbound network calls**. SQLite is not the corpus.

`ask` is hybrid retrieve (path and type first; claim vectors only if an on-box embedder is attached).

Reload Grok hooks after install: `/hooks` then `r`.
