# Roadmap

[Docs](README.md) · [pipeline](pipeline.md) · [harnesses](harnesses.md)

Reviewed against the shipped 0.1.1 surface, the standing decisions, and the holes the docs already name. This is the product order. It is not a dump of every later sentence in the specs.

The store is done. The roadmap is: make `ask` happen, keep extract honest, close the harness holes, then child sessions. Not more ranking.

## What lossless is

A work-context index for coding agents. Hooks copy the session file. Heuristics turn it into claims. `ask` checks out at most five records plus warnings. Failed work first, then what already shipped.

Write is push. Read is pull. The harness decides when to ask. lossless only packs. The agent acts. If the skill is ignored, the store is a diary.

Shipped in 0.1.0 / 0.1.1: install and update, five harness adapters, doctor / inspect, catch-up-before-ask when the session is already in the store, extract gates for product copy, named weights, pack of five.

## What must not change

| Keep | Why |
|------|-----|
| No LLM on retrieve or in the write hook | A second agent. Non-deterministic packs. Secrets leave the box. |
| No hosted embeddings | Same. |
| Age never disqualifies | A year-old constraint still wins. |
| Hooks fail-open | Compact must never stall. |
| No Stop-nag, no Stop-inject, no dump of the raw log | Standing decision: setup writes a skill and a marked rule. Stop copies the tape. It does not inject a pack. |
| Claims keyed by `owner/repo` | Same pack across Grok, Claude, Codex, Pi, OpenCode. |
| `WFailedOverlap` 4.0 / `WShippedOverlap` 2.5 | Retune only if the right claim scores and still loses to a weaker row. |
| Loopback default | This machine. Remote home is documented and manual. |
| Five records, not the log | Not cass. Not a hosted fact cache. |
| Cursor reset-on-shrink | A rewritten smaller JSONL must not leave catch-up no-op past EOF. |
| Pathless two-hop still packs the grounded failed | Do not blunt the pack filter. |

If a later idea needs one of those to work, it is the wrong idea.

## Review notes (what the first draft missed)

1. **`maybeCatchUp` is narrower than 0.1.1 sounds.** It runs only when `session_id` is set *and* that session is already in the store with a JSONL path. A first `ask` in a new session, or an MCP call that omits `session_id`, does not ingest the live harness file. Compile can read newest *owned raw*. It does not locate the live harness file.

2. **Caller B Claude inject is specified, not shipped.** No hook writes `additionalContext`. The Grok/Codex **pull** file `~/.lossless/active/<owner__repo>.md` is written after compact catch-up (0.1.5). Stop still does not inject.

3. **Stop-inject is forbidden, not deferred.** "Do not use Stop hooks to nag or auto-inject packs" is a decision. 0.2 may add a pack only at session start and just-compacted, and only where the harness can do that honestly. Stop stays write-only.

4. **OpenCode watcher is on `opencode.db` (0.1.6).** The plugin still POSTs catch-up. A missed plugin no longer loses the tape.

5. **Codex desktop empty rollout is a sqlite dump (0.1.6).** Watcher follows `state_*.sqlite` rollouts when the file exists, else `first_user_message` when cwd is known. `sessions/` may be empty.

6. **`archive`, `scrub`, excerpt-as-last-resort-ask, extract queue, in-process MiniLM, RRF** are specified and unbuilt. They are operator or index work. They are not the product.

7. **`UserPromptSubmit` is write observe (0.1.6).** Installed on Claude and Grok. Fail-open. Does not retrieve. Not inject.

8. **Do not schedule more dogfood waves, weight knobs, Windows, Homebrew, a marketing site, or company RAG.**

9. **The common 0.2a hole is an omitted `session_id`, not a missing store row.** Watcher and hooks usually already inserted the session. `maybeCatchUp` returns immediately when `session_id` is empty. MCP marks that field optional. Models often omit it.

10. **Do not walk the harness home for “newest mtime” on ask.** This machine already has two Grok sessions on the same repo. Newest-mtime can catch-up the other conversation. `CatchUp` with an empty `session_id` names a Grok file `chat_history` (basename of `chat_history.jsonl`). First ingest of a long session on the ask path is a historical import and blows the p95 budget. Ask catch-up is a *delta* on rows the store already knows, or an exact locate when `session_id` is set.

11. **Streaming “behind” is complete-line wait, not a bug.** Catch-up already drops a trailing incomplete JSONL line. Do not “fix” that.

12. **Docs honesty for Caller B and `stack.md` already landed.** Do not redo Task 4 as if it were unfinished.

## Order

```
0.2a ask is complete when someone asks     (shipped 0.1.2)
  → 0.4 harness holes                      (shipped 0.1.6)
  → 0.3 extract / inspect-clean            (shape gates 0.1.8; success bar open)
  → 0.2b Claude inject                     (active file already 0.1.5; inject after README rewrite)
  → 0.6 child-session locate
  → 0.5 indexes, only after a named miss
```

The tape is infinite. The checkout is five records. Infinite context is not a bigger pack and not retuned 4.0 / 2.5. It is: catch-up every harness file (including children), extract only obey-worthy claims, check out after compact. Multi-model is already `owner/repo`. Multi-agent is still parent-only locate — that is 0.6.

0.3 comes before 0.2b Claude inject so a session-start pack cannot inject extract residue. Claude `additionalContext` is a copy change, not a silent exception to "if the skill is ignored, the store is a diary." Do not start 0.5 without a named paraphrase miss. 0.1.8 gated still-stores / loop-residue / example-drop / never-lose-memo / unclosed-paren; 0.3 stays open until live inspect recent is obey-worthy. Do not start 0.2b until then.

---

### 0.2a — Ask is complete when someone asks

The model already calls `ask`. The pack must include this session's latest claims.

| Feature | What it is | What it is not |
|---------|------------|----------------|
| Gate `i'll ask` | Planning narration is not a decision. Same class as 0.1.1 product-copy. | Not a new ranker. |
| Ask catch-up without `session_id` | If the store already has sessions for this workspace, catch-up any whose harness file is ahead of the cursor. If `session_id` is set but not stored, exact `Locate*` for that id only (Grok/Claude with cwd; Codex/Pi with **empty** cwd so they cannot fall back to newest-mtime). Always pass the real session id into `CatchUp`. | Not a walk of `~/.grok/sessions` for newest mtime. Not MCP auto-fill of `session_id`. Not ingest of fixtures. The "no 17 MB import" ban is for **omitted** `session_id`. A set id may first-ingest that one file. |

Success: an MCP `ask` that sends only `workspace_root` + `goal` + `paths` still catch-up stored sessions for that workspace that are behind. An `ask` with `session_id` set, file on disk, row not yet in the store, still catch-up that file. `inspect` recent claims do not include "I'll ask lossless…". After the gate: `inspect --prune`.

First-run doctor `ask` is not this slice. Empty tape stays valid. `docs/algorithm.md` step 1 still says "if `session_id` is set" and must move with this slice.

### 0.2b — Checkout at session start and just-compacted

This is the product-shaped hole. The tape survives compact. The window does not. Do not start this slice until 0.2a is shipped and the inject shape is still wanted.

| Feature | What it is | What it is not |
|---------|------------|----------------|
| Claude cold/hot inject | SessionStart and just-compacted: one `additionalContext` block from a real `ask`. | Not every turn. Not Stop. Not a hidden rewrite of history. |
| Grok / Codex active file | After compact / session start, write `~/.lossless/active/<project>.md`. Skill says: if that file exists and this turn has not asked, read it or call `ask`. | Not pretending Grok PostCompact can inject. |
| Skill line | Point at the active file. Keep the rule one screen. | Not a longer skill. |

Do not start until 0.2a is on the channel **and** `inspect --project` recent claims are obey-worthy (0.3). Claude `additionalContext` ships only after README "Not auto-injection" is rewritten to: not every turn, not Stop, not the raw log. Grok/Codex `active/<project>.md` is still pull (the model must read it). Split those two if the copy fight is unresolved: keep the active file, cut Claude inject.

SessionStart / just-compacted hooks stay fail-open. Catch-up must still skip own ask payloads after a pack lands in the window. Stop stays write-only. UserPromptSubmit every turn is not this slice.

### 0.3 — Extract is the intelligence

Retrieve is already better than write. The only allowed "smarter" is better extract so claims have the right `symbols[]`. Ranking stays dumb.

0.1.8 gated still-stores meta (punctuation forms, no "and pack" required), loop-residue keep-talk, example-drop recaps, chopped never-lose-memo, and unclosed-paren fragments. ProcessState leftovers skip only as type=state. A They-found + Redis/path failed still stores. The success bar is not closed: live inspect recent can still include operator recap.

- Remaining narration, process-state, product-copy leftovers
- Stronger symbols from path stems and identifiers (hop and overlap eat these)
- `inspect` as the operator loop: recent noise, last packs, prune of extract residue
- Extract queue only if a measured hook blows the compact budget (copy raw first, claims a second later)

Success: `inspect --project` recent claims are all things a future session should obey.

### 0.4 — Finish the five adapters

One catch-up core. These are locate bugs.

| Gap | Why it matters |
|-----|----------------|
| OpenCode watcher on `opencode.db` | Shipped 0.1.6. Plugin miss no longer loses the tape. 16 sqlite sessions per tick. |
| Codex desktop when `sessions/` is empty and sqlite has no rollout file | Shipped 0.1.6. Threads with cwd + `first_user_message` catch-up when the rollout file is missing. |
| Claude watcher skip when cwd is unknown | Shipped 0.1.6. Watcher peeks `cwd` from the transcript. Unknown-cwd still skips. Do not rewrite `cleanupPeriodDays` in setup. |
| `UserPromptSubmit` observe (write only) | Shipped 0.1.6. Claude and Grok. Fail-open. Does not retrieve. No `additionalContext`. |
| Cross-harness check on one real repo | Shipped 0.1.6. Watcher catch-up from two harnesses; `ask` in one session packs the other's claim. |

A new harness after that is still: locate + event map + line parser + installer.

### 0.6 — Child sessions are tape

The pitch is multi-agent: a parent that spawns reviewers still has one project pack. Today locate follows the parent JSONL. Grok `spawn_subagent`, Claude subagents, and Codex child threads write their own files. If those are not catch-up, the child's failed work dies when the parent window thins.

| Feature | What it is | What it is not |
|---------|------------|----------------|
| Locate child transcripts | Same catch-up core. Discover the child's JSONL / sqlite row. Claims stay `owner/repo`. | Not a per-agent store. Not newest-mtime across children. Not injecting the child's raw log. |
| Session id stays the child's | Action tape per `session_id`. Ask in the parent still packs project claims. | Not merging two conversations into one session file. |

Success: a child session that burned Redis is in the project pack when the parent asks. A sibling child's shop claims do not leak.

Do not start until 0.3 has cleaned recent claims so child ingest does not flood residue.

### 0.5 — Indexes, not a brain

Only after a **named paraphrase miss**: path empty, symbols empty, lexical miss, the right claim exists.

- On-box claim vectors (`LOSSLESS_EMBED_CMD` already exists; live embedder is none)
- In-process MiniLM when there is a pure-Go runtime
- Same `ask`, same packer. Path / type / overlap still win
- Optional RRF only if measured against that miss
- Older excerpt partitions attach on demand only if claims miss and the question looks like a lookup

Never: excerpt embeddings as the hot index, hosted APIs, HyDE, LLM rerank, embedding raw transcripts.

### Operator, when someone needs it

Specified, not user-facing product:

- `archive` a project (detach from hot ask, keep raw)
- offline `scrub` of raw
- `embed-backfill` already exists as a CLI; useful only after an embedder is attached
- Historical directory import (single-file `catch-up --source import` already exists)

Remote home stays manual: TLS + token + local sidecar. No cloud image, no org ACL, no S3.

## Not on the roadmap

- Windows
- Memory that decides when to speak
- Auto-inject of the full raw log
- Retrieve on every tool call
- Learned ranker / Phoenix
- Age gates
- Company RAG, hosted fact cache
- Per-user ACL, invites, billing
- Another README pass or marketing site
- More retrieve-weight knobs
- Invented dogfood waves

## How to use this file

Visibility is the loop. After each slice: `lossless inspect --project <owner/repo>` and `lossless doctor`. Do not retune 4.0 / 2.5. Do not start 0.5 without a named miss. Do not start 0.2b Claude inject until 0.3 has cleaned recent claims. Do not start 0.6 until 0.3 is clean.
