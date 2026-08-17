# lossless

The session log is the memory. Compact is lossy. We are not.

Coding agents rebuild the *window* on every turn and replace early work with a summary. The *session file* is still append-only. lossless copies that file into an owned corpus, derives small claims from it, and answers one question: given this work, what must you not forget?

Same shape as a log stream (Splunk / Grafana Loki) or git: keep the tape, build indexes, check out a few records. Do not paste the stream into the next prompt.

Transcripts stay on your machine. Apache-2.0.

```
ask({ question, project, paths, goal })
  → context + warnings
```

See [docs/architecture.html](docs/architecture.html) for a one-page map of the loop, store, and retrieve algorithm. [docs/pipeline.md](docs/pipeline.md) is the loop in prose, [docs/deploy.md](docs/deploy.md) is the portable REST + MCP contract (local by default; a remote home is manual). Then [docs/write.md](docs/write.md), [docs/ask.md](docs/ask.md), [docs/retrieval.md](docs/retrieval.md), [docs/harnesses.md](docs/harnesses.md), [docs/stack.md](docs/stack.md).

## Why

Mem0, Cognee, and other “agent memory” tools extract facts with an LLM and throw the transcript away. That is a good hosted fact cache. It is a bad record of work: failed approaches and tool results disappear into a summary, and you cannot replay.

lossless keeps the event log (like git objects). Claims are HEAD — a derived view you can rebuild. `ask` is the checkout: five records, failed and shipped first, not a grep over ten years of `cat`.

## Quick start

```bash
go test ./...
make cover
go build -o lossless ./cmd/lossless
./lossless catch-up --jsonl ~/.grok/sessions/.../chat_history.jsonl --workspace "$(pwd)" --harness grok
./lossless remember --type decision --text "Use jose, not jsonwebtoken, for Edge." --project acme/api
./lossless ask --project acme/api --question "why not jsonwebtoken" --goal "pick a jwt library"
./lossless bench --root testdata/bench
./lossless setup            # hooks + MCP for every harness, keep the daemon up
./lossless doctor           # daemon, hooks, MCP, user service
./lossless serve            # REST + /mcp; watches session files
./lossless install-hooks    # Grok + Claude + Codex + Pi + OpenCode
./lossless install-mcp      # MCP for Grok, Claude, Codex, Pi, OpenCode
./lossless mcp              # stdio MCP (talks to the daemon)
```

One-time install (any supported harness):

```bash
./lossless setup
./lossless doctor
```

That writes hooks, MCP, a skill, and a short always-on home rule for Grok, Claude, Codex, Pi, and OpenCode so the model calls `ask` on real work without you typing `/lossless`. Then start a new agent session (Grok: `/hooks` then `r`; `/skills` then `r` if the session is already open). Everything stays on this machine.

A remote home is optional and manual: run `serve` on any box, put TLS in front, set `LOSSLESS_URL` + `LOSSLESS_TOKEN`, point MCP at `/mcp`. We do not provision a cloud or ship your store for you. See [docs/deploy.md](docs/deploy.md).

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
