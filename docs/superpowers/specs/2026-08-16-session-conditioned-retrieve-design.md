# Design: session-conditioned retrieve

Date: 2026-08-16
Status: IMPLEMENTED
Amends: `docs/retrieval.md` sections 1–5 (normalize, candidates, features, score, pack)
API: unchanged. `POST /v1/ask` still returns ≤5 claims.

## Problem

The current engine is a good anti-regression ranker when the caller sends a rich
`ask` (goal + paths + question). It is a bad long-horizon memory when:

1. A new model opens the repo with an empty or thin `ask`.
2. A standing decision or constraint is months old. The 14-day recency
   half-life and the 180-day failed lookback treat age as irrelevance.
3. `ColdPriorityIDs` walks `failed → decision → constraint` until 30 ids.
   Thirty recent failures crowd every decision out of the candidate set.

lossless is named that because something that happened a long time ago can
still be the thing that stops today's mistake. Age never disqualifies a claim.
The session (what the agent is doing *now*) is the only relevance signal.

## Premises

1. Same `ask` JSON. Same pack cap of 5. No new endpoint.
2. No LLM on the retrieve path. No hosted embeddings required.
3. Trust a rich ask. Compile a thin ask from the session tail. If still empty,
   pack project HEAD (coverage), not "newest high-priority."
4. Recency is a tie-break, type-aware. It is not a gate and not the score.
5. Archival "why" stays `GET /v1/records/:id` plus the tape. `ask` does not
   dump excerpts.

## Modes

After normalize + optional compile, the engine is in one of two modes.

| Mode | When | Objective |
|------|------|-----------|
| **Session-conditioned** | Query has paths, symbols, or lookup tokens | Pack the covering set that prevents mistakes on *this work*, any age |
| **HEAD coverage** | Query still empty after compile | Pack the covering set of what is *true of this repo*: constraints, decisions, open failed |

"Cold" in today's code (`question` and `goal` both empty) is not a mode. It is
an input that may become session-conditioned after compile.

## 1. Query compile

`normalize` still builds a `Query` from the request. Then:

```
rich = len(path_keys) > 0
    OR len(question_tokens) + len(goal_tokens) >= 2

if rich:
    use the Query as-is          # do not overwrite caller fields
else:
    compile from session tail    # fill only empty fields
    recompute tokens, path_keys, symbols, lookup, cold
```

### What "compile" reads

No search string is invented. We copy structure out of the current work.

Locate a session, in order:

1. Newest owned raw file for `project_key` under `~/.lossless/raw/<key>/`.
2. Else, if `workspace_root` is set, the harness session file for that cwd
   (same locate rules as `docs/harnesses.md`).

Tail the last 40 usable messages (same skip list as extract: no `system`,
`reasoning`, synthetic skill dumps, no `ask`/`remember` I/O).

Fill only empty fields:

| Empty field | Source |
|-------------|--------|
| `question` | last human user line, trimmed, cap ~500 chars |
| `paths` | `pathRE` hits in the tail, plus paths on the newest claims for this session/project, cap 8 |
| `symbols` | identifier heuristic on those tokens and path stems (existing `identLower`) |
| lookup extras | tokens from tail lines that match `failedRE` (error language). These join `lookup_text` only |

If locate fails or the tail is empty, compile is a no-op.

After compile, if `path_keys`, `symbols`, and `lookup_tokens` are all empty,
mode = HEAD coverage. Otherwise mode = session-conditioned.

Compile is local file I/O. It is not a network call. If the tail is large,
stop at 40 messages / 32k chars (same budget as extract).

## 2. Candidate generation

Still cap **200**. Never scan the full project table.

### Session-conditioned (replaces today's "hot")

Union, then cap:

| Source | How | Cap |
|--------|-----|-----|
| A. FTS | `lookup_text` against claims FTS, active, this project | 80 |
| B. Path | posting list for each `path_keys` | 40 per, 80 total |
| C. Symbol | posting list for each `symbols` | 40 per, 80 total |
| D. Failed | **all** active `type=failed` for the project, no date filter | 40 |
| E. Decision | active `decision` whose path or symbol overlaps | 40 |
| E2. Constraint | active `constraint` whose path or symbol overlaps | 40 |
| F. Vector | optional on-box kNN of `lookup_text` | 80 |

`FailedLookback = 180` is removed. An 8-month failed on `auth.ts` is eligible
when today's work is `auth.ts`.

If A+B+C+F are empty, still keep D/E/E2. If the whole union is empty, return
empty context. Empty is valid.

### HEAD coverage (replaces today's "cold")

Type-capped so one type cannot eat the set:

| Source | Cap | Order inside the cap |
|--------|-----|----------------------|
| failed | 12 | recency (tie-break only) |
| decision | 10 | recency |
| constraint | 8 | recency |
| path postings if `path_keys` exist | 40 | — |
| state | 10 | only if no paths and the type caps did not fill 30 |

`ColdPriorityIDs` walking 30 failed then stopping is a bug. Replace it.

No FTS. No vectors. No excerpts.

### Dedup

By `id`. Same `claim_hash` → keep newest.

## 3. Features

Same feature table as `docs/retrieval.md` §3, with these changes:

**`recency` is type-aware.** Clock = request time.

| Type | Half-life | Why |
|------|-----------|-----|
| failed | 14 days | recent burns are more actionable when two failed overlap the same work |
| state, thread | 7 days | ephemeral |
| decision | 180 days | 8-month `jose` still scores ~0.4 |
| constraint | ∞ (`recency = 1`) | standing rules do not fade |

Age still never drops a candidate. Recency only orders ties inside a cell.

**`failed_overlap` / `shipped_overlap`** unchanged, except `shipped_overlap`
also applies to `constraint` (path or symbol or token overlap). A standing
"never log Authorization" is job 2, not a nice-to-have.

**`stale`** unchanged. Ephemeral `[verify]` prefix. Stat top 30 only.

## 4. Score

### Session-conditioned

```
score =
    4.0 * failed_overlap
  + 2.5 * shipped_overlap
  + 1.5 * (type_rank / 5)
  + 1.2 * path
  + 1.0 * symbol
  + 0.9 * bm25
  + 0.9 * vector
  + 0.2 * recency          # was 0.8
  - 0.7 * stale
```

Job 1 still outranks job 2 still outranks "sounds like." Recency cannot beat
an overlapping 10-month constraint.

### HEAD coverage

```
score =
    2.0 * (type_rank / 5)
  + 1.2 * path
  + 0.2 * recency
  - 0.7 * stale
```

No BM25. No overlap flags (the query is empty). Type and (if present) path
decide. Recency breaks ties.

Named constants stay in `internal/retrieve/weights.go`. A weight change that
breaks a required fixture is a failed change.

## 5. Pack

Still: sort by score, then `created_at`, then `id`. Token budget. Hard cap 5.
Text Jaccard ≥ 0.8 still skips near-duplicates.

**Coverage, then score.** After must-keep, greedy:

1. If any `failed_overlap == 1`, pack the highest-scored of those first
   (same contract as today's `evictFailed`: job 1 cannot lose to the cap).
2. Repeatedly pack the remaining candidate that maximizes
   `score - λ * max_sim(already packed)`.
3. `sim` is 1.0 if same type **and** sharing a path-cluster (basename),
   else text Jaccard.
4. `λ = 0.8` (`WCoverage`).

HEAD coverage uses the same packer. The effect is reserved *behavior* without
reserved slots in the JSON: five restated failures cannot crowd out the only
standing constraint.

`evictFailed` remains as a safety net after pack.

Warnings unchanged: only cite ids in the packet. Failed-overlap and
shipped-overlap (now including constraints) still emit.

## What this is not

- Not Mem0. Cosine is one candidate source, optional, on-box, claims only.
- Not an LLM "summarize the project" compile.
- Not a new `ask` field. `session_id` is not required. Locate is best-effort.
- Not dumping the transcript into the window.
- Not two public retrieve APIs. One `ask`, two internal modes.

## Eval

Existing fixtures stay the gate. New required cases:

| # | Setup | Must |
|---|-------|------|
| C1 | Thin ask `{}`. Session tail talks about `auth.ts` and "rate limit". Store has Redis failed + billing state | Pack Redis failed. Compile filled paths. |
| C2 | Rich ask with billing paths. Session tail is all auth | Trust caller. Do **not** pack auth Redis as #1. |
| C3 | `jose` decision 8 months old. Recent billing failed. Ask "JWT library" / auth paths | `jose` in the 5. Age did not bury it. |
| C4 | 30 recent failed on other files + 10-month "never log Authorization". Empty ask, no session | Constraint in the 5. Type cap works. |
| C5 | Failed on `auth.ts` from 200 days ago. Goal = rate limit, path = auth.ts | That failed is packed + warning. No 180-day gate. |
| C6 | Empty store / locate miss / empty tail | Empty context is valid. No crash. |
| C7 | Two restated jose decisions + one constraint + one failed, HEAD mode | Pack at most one jose. Mix of types. |
| C4-old | Existing `04-cold-ask` | Still prefers failed/decision at the top, but must not be failed-only if decisions exist. |
| C14-old | Existing `14-path-only-cold` | Still must not include billing invoices. |

C1 needs a fixture session file under `eval/` (a short JSONL tail). Do not
hit the developer's `~/.lossless` in tests. Inject a `LocateSession` func
on `Engine` the same way `Now` is injected.

## Implementation map

| File | Change |
|------|--------|
| `internal/retrieve/query.go` | `rich` predicate; compile fills empty fields |
| `internal/retrieve/compile.go` | new. Locate + tail + fill. Testable with a fake locator |
| `internal/retrieve/ask.go` | mode after compile; drop failed lookback; add E2; type-aware recency; coverage pack |
| `internal/retrieve/weights.go` | `WHotRecency = 0.2`, remove `FailedLookback`, add type caps + `WCoverage` + half-lives |
| `internal/store/search.go` | replace `ColdPriorityIDs` with type-capped HEAD ids; `ConstraintIDsOverlapping` |
| `docs/retrieval.md` | replace §§1–5 to match this file after implementation |
| `eval/ask/` | C1–C7 fixtures |

No schema change. Claims, FTS, and posting lists stay as they are.

## Success

- A new Claude with a thin `ask` still gets the overlapping 8-month decision
  if the session tail names the file or the work.
- A rich `ask` is not poisoned by an unrelated session tail.
- Thirty recent failures cannot hide a standing constraint.
- `go test ./...` plus the ask fixture suite, including C1–C7.
- p50 `ask` stays under 50ms at 10k claims on a laptop (compile is a 40-line
  tail, not a corpus scan).

## Open constants (named, not taste)

| Name | v1 | Tune by |
|------|----|---------|
| Rich token threshold | 2 | C1 vs C2 |
| Compile tail | 40 msgs / 32k chars | same as extract |
| Failed / decision / constraint HEAD caps | 12 / 10 / 8 | C4 |
| Decision half-life | 180 days | C3 |
| `WHotRecency` | 0.2 | C3, C5 |
| `WCoverage` | 0.8 | C7 |

Change one constant, re-run the suite. Do not bury numbers in `ask.go`.
