# lossless

The session log is the memory. Compact is lossy. We are not.

Coding agents rebuild the *window* on every turn and replace early work with a summary. The *session file* is still append-only. lossless copies that file into an owned corpus, derives small claims from it, and answers one question: given this work, what must you not forget?

Same shape as a log stream (Splunk / Grafana Loki) or git: keep the tape, build indexes, check out a few records. Do not paste the stream into the next prompt.

Transcripts stay on your machine. Apache-2.0.

```
ask({ question, project, paths, goal })
  → packed context + warnings
```

See [docs/pipeline.md](docs/pipeline.md) for the loop, [docs/deploy.md](docs/deploy.md) for **local-only or one home / many sidecars**. Then [docs/write.md](docs/write.md), [docs/ask.md](docs/ask.md), [docs/retrieval.md](docs/retrieval.md), [docs/harnesses.md](docs/harnesses.md), [docs/stack.md](docs/stack.md).

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
./lossless serve            # REST + /mcp; watches session files
./lossless install-hooks    # Grok + Claude + Codex + Pi + OpenCode
./lossless install-mcp      # Grok HTTP MCP + Claude stdio MCP
./lossless mcp              # stdio MCP (talks to the daemon)
```

Data dir: `~/.lossless/` (`LOSSLESS_HOME` to override).

```
~/.lossless/raw/<owner>__<repo>/2026-08/<session>.jsonl   # owned corpus (forever)
~/.lossless/export/<owner>__<repo>/{id}.md               # claims
~/.lossless/index/claims.sqlite                          # rebuildable index only
~/.lossless/index/excerpts-YYYY-MM.sqlite                # monthly excerpt windows
```

Default config makes **zero outbound network calls**. SQLite is not the corpus.

`ask` is hybrid retrieve (path/type/FTS first; claim vectors later). Vectors are optional — missing embedder is degraded mode, not a hard failure.

Reload Grok hooks after install: `/hooks` then `r`.
