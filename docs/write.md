# Write path

This is how memory gets in. `docs/ask.md` is how it comes out. `docs/retrieval.md` ranks what is already stored.

Claude deletes JSONL after 30 days. If we only point at harness files, we do not remember everything. Catch-up copies into owned `raw/`.

---

## Window vs session file (measured)

These are not the same thing. Getting this wrong is how the write path becomes either lossy or a 300k-token dump through the model.

**The context window** is the prompt the model sees on the *next* API call. It is rebuilt every turn. It includes:

- system prompt and tool schemas (fresh)
- injected now: `user_info`, skills catalog, git status, CLAUDE.md / AGENTS.md, memory hits
- recent conversation the harness still has room for
- after compact: a **summary** in place of early turns and old tool results

**The session file** is an append-only event log on disk. Compact does **not** rewrite it.

This conversation (Grok, 0 compacts, ~310k tokens in the live window):

| File | Size | What it is |
|------|------|------------|
| `chat_history.jsonl` | 1.4 MB / 334 lines | Model-facing log |
| `updates.jsonl` | 3.5 MB / 539 lines | ACP UI stream. Do **not** ingest. |
| `system_prompt.txt` | 6 KB | Snapshot. Re-injected every session anyway. |

`chat_history.jsonl` line types: `system` 1, `user` 14, `assistant` 68, `tool_result` 117 (~397 KB), `reasoning` 106, `backend_tool_call` 31. Largest line ~94 KB (a file read). One "user" line is a 70 KB synthetic skills dump, not something the human typed.

A **different** Grok session with **8 compacts**: live window ~343k tokens, `chat_history.jsonl` still **17.6 MB / 581 lines / 328 tool_results**, and zero summary lines in the file. If the file were the window, 17 MB would not fit. The file is the full history. The window is a view.

So:

| | In the window now | In `chat_history.jsonl` |
|--|-------------------|-------------------------|
| Human chat | yes, recent | yes, all turns |
| Tool calls + file-read bodies | recent / clipped after compact | **all of them**, still on disk |
| Reasoning traces | maybe | yes |
| Fresh skills / git / memory inject | yes, every turn | only whatever was logged at start |
| Compact summary | yes, after compact | usually **not** (Grok keeps originals) |

"Just the chat" would be user + assistant text. That is a fraction of the file (here: 14 user + 68 assistant vs 117 tool results). We would lose "we read `auth.ts` and it used jose."

"The whole window" would include live injections and, after compact, **drop** the original tool results. Asking the model to dump the window is therefore both slow and **lossy**.

**What we copy:** the session event log (`chat_history.jsonl` / Claude `transcript_path` / Codex rollout). That is the input. It is *more complete than the post-compact window* and *more than chat*.

**What we do not do:** ask the model to serialize its current prompt. That is the "don't dump the window" line.

**What we still filter at derive time:** `system`, `reasoning`, synthetic skill dumps (`synthetic_reason` / huge `<system-reminder>` user lines). They stay in raw. They must not become claims. Tool bodies stay in raw; excerpt FTS clips them.

---

## Strategy review (write input)

Holds:

- One catch-up core + thin adapters is right. The five harnesses differ in locate/event/parse only.
- Owned raw is right. Claude deletes JSONL in 30 days. After 8 Grok compacts the file is 17 MB of originals we would lose if we only trusted the window.
- PreCompact is a safety net, not the only writer. Turn-level catch-up + a session-dir watcher cover Codex (no compact hook) and crashed hooks.
- `updates.jsonl` is the wrong Grok file (2.5× larger, ACP). Primary is `chat_history.jsonl`.

Fix in the write/harness specs:

- Claim extract **must skip** system, reasoning, and synthetic user injections or we remember SKILL.md 500 times a year.
- Tool results are most of the bytes. Raw keeps them. Excerpt index clips. Claims are not "the file we read."
- Do not treat OpenCode's snapshot object store as JSONL until we prove a tail-able file. Plugin `messages[]` POST is the honest fallback.

---

## Jobs

| Priority | Job | Failure mode |
|----------|-----|----------------|
| 1 | Nothing that entered a session is lost after compact, crash, or harness cleanup | "Everything" was a cursor into a file that got deleted |
| 2 | Writes stay cheap on the hot path | PreCompact hook times out, compact proceeds, tail never copied |
| 3 | Store grows forever without making `ask` or ingest fall over | One SQLite FTS over 10 years of tool output |
| 4 | Derived views stay rebuildable | We "remember" only summaries and cannot reconstruct |

Remembering everything means **retain the raw stream we own**. It does not mean dump the raw stream into every `ask`. Retrieval stays bounded.

---

## Three layers

```
 harness JSONL  ──copy──►  RAW LOG (owned, append-only, forever)
                                │
                                ├── chunk + redact ──► EXCERPT INDEX (derived, partitionable)
                                └── extract claims ──► CLAIMS (derived, markdown SoT)
```

| Layer | What it is | Delete? | Rebuild? |
|-------|------------|---------|----------|
| **Raw** | Byte-for-byte (redacted) copy of every ingested session event. **Files, not SQLite.** | Never by default | N/A. This is the corpus. |
| **Excerpts** | Searchable spans over raw | Index can be dropped | Yes, from raw |
| **Claims** | Typed facts (`failed`, `decision`, …) | Supersede, never hard-delete | Yes, from raw (lossy: heuristics) |

Claims are a view. Raw is the memory. If these disagree, raw wins for "what was said"; the active claim wins for "what we believe now."

---

## Growth (design for forever)

Back-of-envelope, one person, heavy use:

| | Per long session | Per year (500 sessions) | 10 years |
|--|------------------|-------------------------|----------|
| Raw JSONL | 2–8 MB | 1–4 GB | 10–40 GB |
| zstd raw | ~0.5–2 MB | 0.3–1 GB | 3–10 GB |
| Excerpt FTS (all time, hot) | — | blows up | do not do this |
| Claims markdown | ~5–30 KB | 5–15 MB | 50–150 MB |

A laptop can keep **raw forever** if it is append-only and compressed after the session ends. It cannot keep one global FTS over every tool payload forever and still hit the 50ms `ask` budget.

Rules that follow:

1. Raw is partitioned by `project_key` + `yyyy-mm` and compressed when a session is idle > 24h or on `SessionEnd`.
2. Excerpt FTS is partitioned the same way. `ask` opens **hot partitions only** (default: last 12 months + any partition touched in the current session). Older partitions attach on demand if claims miss and the question looks like a lookup.
3. Claims stay small and stay in the hot index for the life of the project (active + superseded). That is the anti-regression set. 150 MB of markdown in 10 years is not a problem.
4. No automatic deletion. The operator may `archive` a project (move raw+export to a directory, detach indexes). Archive is still on disk, still local, just not in the hot `ask` set.

Default retention: **keep forever**. There is no `cleanupPeriodDays`.

---

## Layout on disk

`LOSSLESS_HOME` (default `~/.lossless/`):

```
raw/
  acme__api/
    2026-08/
      <session_id>.jsonl          # live, being appended
      <session_id>.jsonl.zst      # sealed
    2026-07/
      ...
export/
  acme__api/
    <claim_id>.md                 # claims SoT (unchanged from design)
index/
  claims.sqlite                   # all claims + postings, rebuildable from export/
  excerpts-2026-08.sqlite         # FTS for that month
  excerpts-2026-07.sqlite
cursors.sqlite                    # harness path → offset, raw path → offset
spool/                            # durable queue if a write misses the daemon
hooks.log
```

Harness files are **sources**, not storage. After a successful copy into `raw/`, we do not need the harness file to survive.

---

## When we write

Every trigger does the same thing: **catch up**. Copy new bytes, then derive. Triggers differ only in when they fire.

| Trigger | Who | When | Budget |
|---------|-----|------|--------|
| **Turn** | `Stop` / `UserPromptSubmit` (observe) | As they go, every completed turn | 200ms fail-open |
| **Tool batch** | optional `PostToolUse` every N tools (default 8) | Long turns with no Stop yet | 200ms fail-open |
| **PreCompact** | hook | Last chance before the window dies | **< 1s**, fail-open |
| **SessionEnd** | hook | Seal the session, compress raw | 2s fail-open |
| **Remember** | `POST /v1/remember` | Agent or human says "keep this" | sync |
| **Import** | CLI | Historical JSONL | unbounded, background |

PreCompact is a safety net, not the only writer. If turn-level catch-up is healthy, PreCompact is a no-op (cursor already at EOF).

The model is **not** asked to dump the window. After compact the window is already a summary; the session file still has the original tool results (measured: 8-compact Grok session, 17.6 MB history, 0 summary lines). Copy the file. See [Window vs session file](#window-vs-session-file-measured).

Agents *may* also write structured claims via `remember` when they know something is durable ("we rejected Redis"). That is additive. It does not replace raw catch-up.

---

## Catch-up algorithm

Idempotent. Safe to run twice. Safe if the process dies mid-way.

```
catch_up(harness_path, project_key, session_id, workspace_root, harness, source):
  1. lock session (file lock on raw/<project>/<yyyy-mm>/<session_id>.lock)
  2. src_off = cursors.harness[harness_path] or 0
  3. if src_off >= size(harness_path): unlock; return
  4. read harness_path[src_off:] as bytes
  5. redact_stream(bytes) → clean_bytes     # drop secret lines, keep the rest
  6. append clean_bytes to raw/.../<session_id>.jsonl
     (temp + flush + size-check; do not rename away a live append file)
  7. cursors.harness[harness_path] = src_off + consumed
     cursors.raw[raw_path] = previous_raw_size + len(clean_bytes)
  8. derive(new clean_bytes):
       a. split JSONL lines → messages
       b. write excerpt chunks covering those lines
       c. extract claims (heuristics only), store.write each
  9. unlock
```

If step 8 fails after 6–7, raw is still complete. Derive is replayable from `cursors.raw`.

**Hot path (turn / PreCompact) may skip 8c if over budget:** copy raw + excerpts first, enqueue claim extract on a local queue. Claims can land a second later. Raw must land before compact returns.

Never call a model inside catch-up.

---

## Redaction (at copy time)

Applied to bytes **before** they hit `raw/`. Once in raw, we do not go back and rewrite history except via an explicit `scrub` command (operator, offline).

- Same deny-list as the design (AWS keys, `BEGIN PRIVATE`, `Bearer`, `ghp_`, `sk-`, `.env` bodies).
- A redacted line is replaced with `{"_redacted":true}` so offsets stay line-oriented. We do not silently drop lines (that desyncs cursors).
- Claims that would still contain a secret after extract are not written.

Raw is "everything we are willing to keep on disk." Secrets are not remembered.

---

## Derive

### Excerpts

From new messages only:

- Concatenate role + text.
- Window 800–1600 chars, 200 overlap.
- Tool / tool-result bodies > 2000 chars: store full text in **raw**, but the excerpt index keeps head 400 + tail 400. Retrieval can still `GET` the raw span if needed.
- Write into `excerpts-YYYY-MM.sqlite` for the session's month.
- Chunk id = `sha256(session_id + start_offset + end_offset)`.

### Claims

Same heuristic extract as the spike (`failed` / `decision` / `constraint` / `state` / `thread`), same 12-per-batch cap, same `claim_hash` supersede.

**Skip on ingest:** `system`, `reasoning`, synthetic user dumps (`synthetic_reason`, or user text that is only a `<system-reminder>` / skills catalog), and any tool call/result named `ask`, `remember`, or `catch-up`. Those stay in raw. They must not become claims (see `docs/pipeline.md`).

`remember` bypasses heuristics: the payload *is* the claim. Still redacted. Still gets a raw line in a `manual/<date>.jsonl` so it is part of "everything."

After a claim is written, embed `text` (+ symbols) into `claim_vectors` if `Store.Embedder` is set. Missing embedder is fine (degraded retrieve). Do not embed raw lines or tool bodies. A write never fails because embed failed. `lossless embed-backfill` fills gaps after you attach a model (`LOSSLESS_EMBED_CMD` or `LOSSLESS_EMBED_MODEL`).

Supersede: new active row, old `status=superseded`, file rewritten. Never delete the markdown file.

---

## `remember` and ingest APIs

```
POST /v1/catch-up
{
  "path_to_jsonl": "/Users/you/.grok/sessions/.../chat_history.jsonl",
  "project": "acme/api",
  "workspace_root": "/Users/you/dev/api",
  "harness": "grok",
  "session_id": "...",
  "source": "turn" | "compact" | "session_end"
}

POST /v1/remember
{
  "project": "acme/api",
  "workspace_root": "...",
  "type": "decision",
  "text": "Use jose, not jsonwebtoken, for Edge.",
  "paths": ["src/middleware/auth.ts"],
  "why": "Edge runtime"
}
```

`POST /v1/ingest` in the spike becomes `catch-up` (file) or `remember` (records). One implementation, two doors.

CLI: `lossless catch-up --jsonl FILE --project KEY` and `lossless remember --type decision --text "..."`.

---

## Hooks (write only)

Harness-specific locate / event / parse lives in [harnesses.md](harnesses.md). Same catch-up binary, different events.

| Harness | As they go | Before compact | End |
|---------|------------|----------------|-----|
| Grok | `Stop` (reason=end_turn only) | `PreCompact` | `SessionEnd` |
| Claude | `Stop` | `PreCompact` | `SessionEnd` |
| Codex | session-end + file mtime poll (no PreCompact today) | — | session-end |

Budgets as in the trigger table. Fail-open. If the daemon is down, write a spool file `{harness_path, offset, meta}` and let `--ensure` replay. Spool is durable; losing the hook process must not lose the fact that work happened — the harness JSONL is still there until cleanup, so replay from cursor 0 against raw is safe (append is idempotent by offset).

Do not retrieve from hooks. Do not inject context. Write only.

The hook only talks to a **local sidecar**. Bytes reach a remote home via incremental `POST /v1/append` in the background (`docs/deploy.md`). The hook never POSTs to a VPS.

---

## Concurrency and portability

- One writer per `session_id` (file lock). Different sessions append in parallel.
- `claims.sqlite` uses WAL. Claim writes are short transactions.
- Two harnesses on the same repo share `project_key`. Their sessions are different raw files. Claims collide only via `claim_hash` (same fact twice → supersede).
- Moving machines: copy `raw/` + `export/`. Delete `index/` and rebuild. Cursors are machine-local (harness paths differ); first catch-up on a new machine re-reads harness files and no-ops if those sessions already exist in raw (match `session_id` + raw size).

---

## Sealing and compression

On `SessionEnd`, or if a live `.jsonl` has not grown for 24h:

1. Final catch-up.
2. `zstd` the raw file next to itself, fsync, then delete the uncompressed file.
3. Further catch-up for that session is a no-op unless the harness file grows (resume). If it grows, decompress-or-open-append: write a new `.jsonl` part `session_id.part2.jsonl` rather than mutating the zstd. Raw is append-only even across resume.

---

## What we never do

- Ask the model to serialize the context window into memory.
- Treat harness JSONL as the long-term store.
- Put full tool outputs into the hot excerpt FTS.
- One FTS table over all years.
- Delete raw because an index is large.
- Block compact on derive or on a model call.
- Rewrite raw in place (except offline `scrub`).

---

## Write-path eval

| # | Case | Pass |
|---|------|------|
| W1 | Catch-up copies new JSONL lines into `raw/` | File exists, byte content matches redacted source |
| W2 | Second catch-up with no new bytes is a no-op | Raw size unchanged, no new claims |
| W3 | PreCompact after two turns: raw contains both turns | Even if claim extract is skipped for budget |
| W4 | Delete the harness JSONL after catch-up | `ask` still finds claims; raw still readable |
| W5 | Secret line in source | Raw has `_redacted`, claims have no secret |
| W6 | `remember` a decision | Claim file + a line in `manual/yyyy-mm-dd.jsonl` |
| W7 | Same claim twice | Second supersedes, both files exist |
| W8 | SessionEnd seals to `.jsonl.zst` | Uncompressed gone, decompress matches |
| W9 | Resume after seal writes `part2` | Both parts readable, no mutation of zstd |
| W10 | Daemon down: spool + `--ensure` | Raw complete after replay |
| W11 | Two harnesses, same `project_key` | Two raw session files, shared claims |
| W12 | Import 1k historical sessions | Background, raw partitioned by month, hot FTS only current month + claims |
| W13 | 10 years of empty-ish monthly partitions | `ask` opens ≤ 13 excerpt DBs (12 months + current) |
| W14 | Turn hook > 200ms | Fail-open, next PreCompact still catch-up |

---
