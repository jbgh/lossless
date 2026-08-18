# lossless

The session log is the memory. Compact is lossy. We are not.

Coding agents rebuild the *window* on every turn and replace early work with a summary. The *session file* is still append-only. lossless copies that file into an owned corpus, derives small claims from it, and answers one question: given this work, what must you not forget?

Same shape as a log stream (Splunk / Grafana Loki) or git: keep the tape, build indexes, check out a few records. Do not paste the stream into the next prompt.

Transcripts stay on your machine. Apache-2.0. **0.1.0** is the first public cut — [release notes](CHANGELOG.md), [GitHub Releases](https://github.com/jbgh/lossless/releases/tag/v0.1.0).

```
ask({ question, project, paths, goal })
  → context + warnings
```

See [docs/architecture.html](docs/architecture.html) for a one-page map of the loop and store, and [docs/algorithm.html](docs/algorithm.html) for the retrieve algorithm with diagrams. [docs/pipeline.md](docs/pipeline.md) is the loop in prose, [docs/deploy.md](docs/deploy.md) is the portable REST + MCP contract (local by default; a remote home is manual). Then [docs/write.md](docs/write.md), [docs/ask.md](docs/ask.md), [docs/retrieval.md](docs/retrieval.md), [docs/harnesses.md](docs/harnesses.md), [docs/stack.md](docs/stack.md).

## Why

Mem0, Cognee, and other “agent memory” tools extract facts with an LLM and throw the transcript away. That is a good hosted fact cache. It is a bad record of work: failed approaches and tool results disappear into a summary, and you cannot replay.

lossless keeps the event log (like git objects). Claims are HEAD — a derived view you can rebuild. `ask` is the checkout: five records, failed and shipped first, not a grep over ten years of `cat`.

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

`ask` is hybrid retrieve (path/type/FTS first; claim vectors when an on-box embedder is attached). Vectors are optional — missing embedder is degraded mode, not a hard failure. Cosine cannot beat a failed-on-path record.

Reload Grok hooks after install: `/hooks` then `r`.
