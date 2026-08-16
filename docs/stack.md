# Language and environment

**Decision: Go.** One static binary.

Rust is the runner-up. We are not picking it.

---

## Why this is not a language-scaling problem

Forever growth is already specified as storage, not as “rewrite it in a faster language”:

- Raw: append-only, `project/yyyy-mm`, zstd after seal. 10 years ≈ 3–10 GB compressed for one heavy user.
- Excerpt FTS: monthly files. `ask` opens ≤ 13 of them.
- Claims: small, hot for the life of the project.

A 17 MB Grok log after 8 compacts is a **streaming copy**, not a 17 MB heap object. Line-at-a-time catch-up is cheap in both Go and Rust. Loading the whole file and running FTS over all years is expensive in both.

If we build one global FTS over every tool payload, Rust will not save us. If we partition as designed, Go will not fail us.

---

## Go vs Rust for *this* daemon

| Need | Go | Rust |
|------|----|------|
| Stream copy + fsync + file lock | `io.Copy`, boring | Fine, more code |
| zstd seal of monthly raw | klauspost/compress | Fine |
| SQLite + FTS5, WAL, monthly files | mattn/go-sqlite3 (CGO) or modernc.org/sqlite | rusqlite |
| Five JSONL dialects + hook installers | Fast to write and change | Slowest part of the project |
| `ask` over ≤ 200 candidates | Irrelevant | Irrelevant |
| One `scp`-able binary, macOS arm64 + Linux | `GOOS/GOARCH` | Fine, slower CI |
| Long-running safety | GC pauses not the bottleneck (we do not hold the corpus in RAM) | Nice, not load-bearing |
| Later on-box embeddings | onnxruntime / sidecar | candle / ort, slightly better |

cass is Rust because it *is* a search engine over every session on the machine. We are a catch-up daemon plus a 200-row ranker. Different job.

The expensive work we will keep doing is **parsers and adapters** (Grok vs Claude vs Pi tree vs Codex rollout vs OpenCode). That work churns. Go wins on churn.

---

## Environment (locked)

- **Go 1.24+**, module `lossless`.
- Layout:
  ```
  cmd/lossless/         # serve, catch-up, ask, remember, install-hooks
  internal/write/       # catch-up, raw log, redact, cursors, spool
  internal/retrieve/    # later, against docs/retrieval.md
  internal/harness/     # grok, claude, … locate + parse
  testdata/             # fixtures
  ```
- **SQLite is the index, not the database of record.** See below. `modernc.org/sqlite` first (pure Go, FTS5). CGO sqlite only if a *monthly* excerpt FTS misses eval 20.

---

## Is SQLite right if this grows forever?

**Yes, if we never put the corpus in it. No, if we treat one `.sqlite` as the memory.**

| Data | Lives in | Grows | SQLite? |
|------|----------|-------|---------|
| What happened (session JSONL) | `raw/project/yyyy-mm/*.jsonl.zst` | Forever, 3–10 GB / 10 years compressed | **No.** Files. Append + seal. |
| Typed facts | `export/**/*.md` | Slow (50–150 MB / 10 years) | Index only (`claims.sqlite`) |
| Search over recent excerpts | `index/excerpts-YYYY-MM.sqlite` | One file per month, bounded | **Yes, partitioned** |
| Claim vectors | `claim_vectors` next to claims | Same order as claims | Side table, rebuildable |

The anti-pattern is one `memory.sqlite` that eats every tool result for a decade. WAL never shrinks, one writer blocks every hook, FTS bloats, a corrupt file loses everything. That would be the wrong database *and* the wrong shape.

Why not Postgres / Pebble / “just files”?

- **Postgres / PGLite:** right for gbrain (shared graph, many readers). We are one user, one machine, no graph. A server for a laptop daemon is extra failure mode.
- **Pebble / Rocks:** better if we were writing millions of excerpt keys per day. We write a few claims per turn and monthly FTS partitions. LSM is more ops than we need.
- **Files only (grep / walk markdown):** fine until ~10k claims. Then `ask` in 50ms needs an inverted index. SQLite FTS5 *is* that index. Rebuild from `export/` if it dies.
- **Tantivy / Bleve as the only store:** good search engines, worse “copy this directory to another Mac.” Markdown + monthly sqlite copies with `rsync`.

Concurrency: one human, a handful of harness hooks. Session file locks already serialize catch-up. `claims.sqlite` is a short WAL transaction per claim batch. That is SQLite’s happy path.

**Rule:** if a blob will still matter in 10 years and is large, it is a file in `raw/`. If it is a small derived fact we query in 50ms, it may live in SQLite. SQLite files stay **replaceable** (rebuild) and **bounded** (claims all-time is small; excerpts are monthly).
- **Compression:** `github.com/klauspost/compress/zstd`.
- **HTTP:** `net/http` on `127.0.0.1:7432` by default. `--listen` + bearer only for a box you own (`docs/deploy.md`). No framework.
- **No** required container, CGO, or Node at runtime.
- Cross-compile: `GOOS=darwin GOARCH=arm64` and `GOOS=linux GOARCH=amd64`.
- Tests: `go test ./...` plus the write/ask fixture suites.

---

## When we would revisit Rust

Only if measured: monthly excerpt FTS or catch-up of a 50 MB session cannot meet the hook budget / 50ms `ask` in Go after the partition design is implemented correctly. Do not start a rewrite on a guess.
