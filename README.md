# lossless

Agent memory for coding sessions.

The session log is the memory. Compact is lossy. We are not.

When you work with a coding agent, the *window* is rebuilt every turn. Early failed approaches, library choices, and “don’t do that again” get crushed into a summary. The session file on disk is still there. lossless is how the next turn checks out the parts that still matter.

It is memory for *this* kind of work: one repo, one problem, a trail of tries. The agent calls `ask` with what it is about to do. lossless returns up to five records and a few warnings — failed first, then what you already shipped. Transcripts stay on your machine. Apache-2.0.

**0.1.0** is the first public cut — [release notes](CHANGELOG.md), [GitHub Releases](https://github.com/jbgh/lossless/releases/tag/v0.1.0).

```
ask({ question, project, paths, goal })
  → context + warnings
```

See [docs/architecture.html](docs/architecture.html) for a one-page map of the loop and store, and [docs/algorithm.html](docs/algorithm.html) for the retrieve algorithm with diagrams. [docs/pipeline.md](docs/pipeline.md) is the loop in prose, [docs/deploy.md](docs/deploy.md) is the portable REST + MCP contract (local by default; a remote home is manual). Then [docs/write.md](docs/write.md), [docs/ask.md](docs/ask.md), [docs/retrieval.md](docs/retrieval.md), [docs/harnesses.md](docs/harnesses.md), [docs/stack.md](docs/stack.md).

## What it is

lossless sits next to Grok, Claude, Codex, Pi, or OpenCode and keeps a work log those models can check out.

- **The tape.** Hooks copy the harness session file into `~/.lossless/raw`. That copy is the corpus. Full JSONL. Yours. Kept.
- **Claims.** A small derived index — failed, decision, constraint, state, thread. Rebuildable. Not a substitute for the tape.
- **ask.** The checkout: at most five records for *this* goal and these files. Warnings if you are about to repeat a failed approach. The agent decides what to do; lossless does not write the next reply.

Same shape as a log stream or git: keep the event log, build indexes, check out a few records. Do not paste the stream into the next prompt.

Write is push (hooks on compact, stop, session end). Read is pull (`ask`). Nothing is retrieved until someone asks. Compact hooks copy the tape; they do not inject a pack.

## What it isn't

- **Not a hosted fact cache.** Tools like Mem0 extract facts with an LLM and throw the transcript away. That is a good cache of “preferences.” It is a bad record of work: the failed path disappears into a summary, and you cannot replay.
- **Not a second brain.** There is no LLM inside retrieve. No hosted embeddings. Ranking is a fixed packer, the same for every model. Missing an on-box embedder is degraded mode, not a hard failure.
- **Not a dump.** It will not stuff months of session text into the window. Five records. Failed and shipped first — not a grep over ten years of `cat`.
- **Not auto-injection.** We do not nag, and we do not paste memory into every turn. If the skill is ignored, the store is a diary.
- **Not a cloud.** Default install is this machine (`127.0.0.1`, no token, nothing uploaded). We do not host your transcripts. A remote home is documented and manual.
- **Not company RAG.** It is not a wiki, a ticket search, or a general knowledge base. It is memory for coding sessions on a repo.

Claims are shared by project (`owner/repo`) across harnesses. The action tape — what this session already asked — stays on that session.

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

`update` is the only command that calls GitHub. `doctor` and the daemon do not phone home. The repo is public; a token is only needed if you point `LOSSLESS_UPDATE_REPO` at a private fork.

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

A remote home is optional and manual: run `serve` on any box, put TLS in front, set `LOSSLESS_URL` + `LOSSLESS_TOKEN`, point MCP at `/mcp`. We do not provision a cloud or ship your store for you. See [docs/deploy.md](docs/deploy.md).

## Data

Data dir: `~/.lossless/` (`LOSSLESS_HOME` to override).

```
~/.lossless/raw/<owner>__<repo>/2026-08/<session>.jsonl   # owned corpus (forever)
~/.lossless/export/<owner>__<repo>/{id}.md               # claims
~/.lossless/index/claims.sqlite                          # rebuildable index only
~/.lossless/index/excerpts-YYYY-MM.sqlite                # monthly excerpt windows
```

Default config makes **zero outbound network calls**. SQLite is not the corpus.

`ask` is hybrid retrieve (path/type/FTS first; claim vectors when an on-box embedder is attached). Cosine cannot beat a failed-on-path record.

Reload Grok hooks after install: `/hooks` then `r`.
