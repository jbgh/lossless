# Retrieval engine

[Docs](README.md) · [architecture](architecture.md) · [algorithm](algorithm.md) · [ask](ask.md)

This is the read-path spec. [ask.md](ask.md) is the I/O contract. This file is how we pick the ≤5 records that go in `context`.

No LLM on the retrieve path. No network. Embeddings, when used, are **on-box** and only on **claims**.

---

## Jobs

`ask` does three jobs, in this order. Fusion weights exist to express the order, not to replace it.

| Priority | Job | Failure mode if we get it wrong |
|----------|-----|----------------------------------|
| 1 | Do not repeat failed work | Agent rebuilds the Redis limiter we already burned |
| 2 | Do not regress shipped work | Agent rewrites auth and drops `jose` |
| 3 | Answer the question | Agent cannot find "why we rejected X" |

Thin ask (`question` and `goal` both empty, no paths): compile work context from the session tail if one exists. If the query is still empty, pack project HEAD (type-capped failed/decision/constraint). Age never drops a claim.

---

## Lexical vs semantic (why not BM25-only, why not vectors-only)

A full-table scan plus "any word longer than 3 chars appears" is not a retrieval engine.

What the **spec was** before this revision: BM25 + path + symbol + type + recency. That is already more than BM25. For coding memory it is strong when the ask shares a **path** or a **symbol**. It is weak on paraphrase.

| Ask | Claim | BM25 / tokens | Path | Vector |
|-----|-------|---------------|------|--------|
| add rate limiting, `auth.ts` | Redis token bucket failed in `auth.ts` | maybe "rate"? miss | **hit** | hit (throttling ~ token bucket) |
| why not jsonwebtoken | Use jose, not jsonwebtoken, for Edge | **hit** (`jsonwebtoken`) | maybe | hit |
| why not a JWT library | Use jose, not jsonwebtoken, for Edge | **miss** (no shared token) | miss unless path given | **hit** |
| the cache idea we tried | Redis token bucket failed; pool exhausted | miss | hit if path given | **hit** |
| auth work | 40 old auth chats | noisy | useful filter | **noisy** without path/type |

Lexical is not enough for job 3 ("answer the question") when humans paraphrase. Vectors are not enough for jobs 1–2: cosine will happily surface a billing "rate limit" when you are in `auth.ts`.

**Design: hybrid, structure first.** Inspect on a year of multi-discipline work (25/25 asks, embedder none) showed path + type + failed/shipped overlap already do jobs 1–2. Vectors stay optional. Missing embedder is degraded mode, not a hole. Do not retune `WFailedOverlap` / `WShippedOverlap` unless a live miss forces it.

1. Filter / candidate with path, type, FTS, and **optional** claim-level kNN.
2. Rank with failed/path/type **outweighing** cosine when vectors exist.
3. Embed **claims only** (short texts). Do **not** embed raw excerpts or tool bodies.

Grok's own memory is 0.7 vector + 0.3 BM25 because session notes are chatty. Ours adds path/type because this is a repo. Cosine is a candidate source and a feature, not the product. Pack also drops out-of-neighborhood states and ungrounded pathless faileds so weekday chatter does not occupy a slot next to a real constraint.

On-box model (default): a small sentence embedder (e.g. `all-MiniLM-L6-v2`, 384-d). No API. Rebuildable from claim markdown if we change the model (store `embed_model` + `embed_dim` in index meta). HEAD coverage (nothing to condition on): skip vectors, same as skip BM25.

---

## Indexes

Two indexes. Claims are what `ask` returns. Excerpts are what `GET /v1/records/:id` can add. Do not dump excerpts into `ask` unless a claim is missing and the question is a lookup.

### Claims (`records`)

One row per extracted `MemoryRecord`. Source of truth remains the markdown export. The index is rebuildable.

Posting lists (all scoped by `project_key`, `status = active`):

| List | Key | Payload |
|------|-----|---------|
| FTS | tokens of `text` + `symbols` | record id, BM25 |
| path | repo-relative path, also basename | record id |
| symbol | identifier (`jose`, `tokenBucket`) | record id |
| type | `failed` / `decision` / … | record id, `created_at` |
| vector | embedding of `text` (+ `symbols` joined) | record id, 384-d float32 |

### Excerpts (`chunks`)

One row per redacted transcript span (session id + byte range). Built by the write path ([write.md](write.md)). Used for `GET /v1/records/:id` and as a last-resort candidate source when claims FTS is empty.

Monthly partitions (`excerpts-YYYY-MM.sqlite`). `ask` opens the last 12 months plus the current session's partition. Older partitions attach only if claims miss.

v1 excerpt policy: chunk ~800–1600 chars, 200-char overlap. Full tool bodies live in raw; the excerpt index keeps head 400 + tail 400. Do not extract a claim from a chunk at retrieve time.

---

## Pipeline

```
ask request
  → normalize (project_key, tokens, paths, symbols)
  → compile if thin (session tail)
  → hydrate action tape (last packs, GETs, remembers)
  → candidate generation          # cheap, recall-oriented, cap N
  → feature extraction            # only on candidates
  → score = weighted sum          # named weights, below
  → stale mark (ephemeral)        # stat top-K only
  → pack                          # token budget, diversity, warnings
  → AskResponse
```

Ask is an MCP tool call, not a cache lookup. Target: **p50 < 500ms**, **p95 < 2s** at 10k claims on a laptop. Fail only on hang-class times (>5s). Write/hook paths stay fail-open and fast.

Never scan the full project table. The spike's `listActive` is banned.

---

## 1. Normalize, then compile if thin

From the request, build a `Query`:

```
project_key     required (from project or workspace_root)
question_tokens unicode word tokens, len > 1, lowercased
goal_tokens     same, from goal
path_keys       each paths[] as given, plus basename
symbols         tokens that look like identifiers (camel, snake, dotted, or a known path stem)
lookup_text     question_tokens ∪ goal_tokens ∪ symbols   # used for FTS
rich            path_keys nonempty OR (question_tokens + goal_tokens) >= 2
```

If **rich**, use the Query as-is. Do not overwrite caller fields.

After compile, **hydrate the action tape** for this `session_id` (or the newest session for the project): last pack ids, `get_record` / `remember` dwells, and extra lookup tokens. Caller paths on a rich ask are never overwritten. Packed hit paths are not copied onto the next ask.

If **thin**, compile from the session tail and fill only empty fields:

1. Newest owned raw `*.jsonl` for `project_key` under the store root (or `LocateSession` in tests).
2. Tail last 40 usable messages / 32k chars. Same skip list as extract.
3. Last human user line → `question` (cap 500).
4. `pathRE` hits + recent claim paths → `paths` (cap 8).
5. Failed/error language in the tail joins `lookup_text` only.

If locate fails or the tail is empty, compile is a no-op.

After compile:

| Mode | When | Objective |
|------|------|-----------|
| **Session-conditioned** | any path, symbol, or lookup token | covering set for *this work*, any age |
| **HEAD coverage** | still empty | covering set of what is true of the repo |

Identifier heuristic: token matches `[A-Za-z_][A-Za-z0-9_]{2,}` or contains `/` or `.` and a file extension.

No LLM query expansion. No hand-built synonym list. Paraphrase ("JWT library" vs `jose`) is the vector candidate source. Symbols still matter when extract tagged them; vectors cover the cases extract missed.

---

## 2. Candidate generation

Build a set of at most **`CANDIDATE_CAP = 200`** record ids.

### Session-conditioned

Union, then cap:

| Source | How | Cap before union |
|--------|-----|------------------|
| A. FTS | `lookup_text` against claims FTS, `project_key`, active. Order by BM25. | 80 |
| B. Path | posting list for each `path_keys` | 40 per path, 80 total |
| C. Symbol | posting list for each `symbols` | 40 per symbol, 80 total |
| D. Failed | active `failed` whose path or symbol overlaps | 40 |
| E. Decision | active `decision` whose path or symbol overlaps | 40 |
| E2. Constraint | active `constraint` whose path or symbol overlaps | 40 |
| D2. Inferred path | pathless only: take path keys from A–E hits, pull failed/decision/constraint on those files | 6 paths, 24 per type |
| D3. Recent failed | **only if A–E+D2 are empty** | 8 |
| F. Vector | embed `lookup_text` once; kNN on claim vectors, same `project_key`, active. Cosine. | 80 |

Query symbols skip English leftovers (`library`, `choice`, `add`). `jwt` stays. FTS still sees every lookup token.

If A+B+C+F are empty, still keep D/E/E2. Recent faileds are a last-resort safety net, not a default. A year of warehouse timeouts must not ride along with a JWT ask. If the whole union is empty, return empty context. Empty is a valid answer.

If the embedder is unavailable (first run, model not downloaded, `LOSSLESS_EMBED_CMD` unset), skip F and continue. Lexical+structure still works. Missing vectors is degraded mode, not a hard failure.

Vectors are written at `WriteClaim` when `Store.Embedder` is set. `lossless embed-backfill` walks active claims and fills gaps after you attach a model. Model name is stored on each row; a different model ignores old vectors.

### HEAD coverage

Type-capped so one type cannot eat the set:

| Source | Cap | Order inside the cap |
|--------|-----|----------------------|
| failed | 12 | recency (tie-break only) |
| decision | 10 | recency |
| constraint | 8 | recency |
| path postings if `path_keys` exist | 40 | — |
| state | 10 | only if no paths and the type caps did not fill 30 |

No FTS. No vectors. No excerpts.

### Dedup

By `id`. If two candidates share `claim_hash` (should not happen for active), keep the newest.

Then drop older same-path decisions/constraints when token Jaccard ≥ 0.35. Then drop a decision/constraint if a **newer** same-path `failed` shares topic tokens (Jaccard ≥ 0.2). Tried it, shipped it, it failed again: the dead decision is not current. An unrelated jose decision on the same file is kept.

---

## 3. Features

Compute only on the candidate set. Every feature is in `[0, 1]` except `type_rank` (0–5).

| Feature | Formula | Notes |
|---------|---------|--------|
| `type_rank` | `failed=5, decision=4, constraint=3, state=2, thread=1, excerpt=0` | Same as schema |
| `recency` | `0.5 ** (age_days / half_life)` | Type-aware. Clock = request time. `failed` 14d, `state`/`thread` 7d, `decision` 180d, `constraint` = 1 (does not fade). |
| `path` | Jaccard of query `path_keys` vs record paths (basename counts as a hit) | 0 if either side empty |
| `symbol` | Jaccard of query symbols vs record `symbols` | 0 if either side empty |
| `bm25` | min-max normalize BM25 over **this candidate set** | 0 if record was not an FTS hit |
| `vector` | cosine(query, claim) clipped to `[0,1]` | 0 if embedder off or record has no vector |
| `agree` | (isFTS + path>0 + symbol>0) / 3 | Structure agreement. A claim in two sources beats a one-word FTS hit. |
| `failed_overlap` | 1 if `type=failed` AND **strong** overlap | Job 1. Forces pack + warning. |
| `shipped_overlap` | 1 if `type in {decision,state,constraint}` AND **strong** overlap | Job 2. |
| `failed_weak` / `shipped_weak` | 1 if the type matches and overlap is **weak** (exactly one content token) | Score bump only. Does not force pack or warn. |
| `stale` | 1 if `workspace_root` set and any tagged file mtime > stored `path_mtime` | Ephemeral. Stat at most the current top 30 by pre-stale score. |

Overlap uses `goal_tokens ∪ question_tokens` against `text` tokens, plus `ExpandIdent` aliases (`jwt` ↔ `jsonwebtoken`). Function words (`the`, `library`, `choice`, `add`) do not count.

**Strong:** `path>0` OR `symbol>0` OR ≥2 content tokens OR `vector>=0.55`.  
**Weak:** exactly one content token and none of the above. One shared "rate" is not job 1.

Do not persist `stale`. Prefix `[verify] ` on `text` at pack time if `stale=1`.

---

## 4. Score

### Session-conditioned

Overlap weights are sacred and do not change with the query:

```
4.0 * failed_overlap + 2.5 * shipped_overlap
```

The rest is a **query-conditional mix** (`selectProfile`):

| Profile | When | type | path | symbol | bm25 | vector | recency | agree |
|---------|------|------|------|--------|------|--------|---------|-------|
| path | `paths` nonempty | 1.5 | **1.8** | 0.8 | 0.4 | 0.4 | 0.2 | 0.4 |
| ident | no path, has a code identifier (`jose`, `jwt`, `token_bucket`) | 1.5 | 0.8 | **1.6** | 0.5 | 0.9 | 0.2 | 0.4 |
| prose | tokens only ("what do we know") | 1.2 | 0.6 | 0.6 | **1.3** | **1.3** | 0.2 | 0.4 |

Plus named job heads (same numbers):

```
P_fail    = failed_overlap + 0.25 * failed_weak
P_regress = shipped_overlap + 0.24 * shipped_weak
P_answer  = profile mix (type, path, symbol, bm25, vector, agree, recency)
score     = 4.0*P_fail + 2.5*P_regress + P_answer + 0.8*dwell - 0.9*served
```

`dwell` is a claim the agent `GET`/`remember`ed, and only when this ask is still the same work (thin continue, same question, or same files). A rich ask on a new path does not inherit last GET’s symbols — that was leaking Redis into a billing context. `served` is last context, cancelled by strong overlap so job 1/2 cannot lose to “we already showed you.”

When the caller named files, claims on other files get an out-of-network discount (`P_answer × 0.5`), same idea as X’s OON weight. Job 1/2 overlap is not discounted.

Weak never outranks sacred overlap.

Job 1 outranks job 2 outranks "sounds like." Recency cannot beat an overlapping 10-month constraint. A failed-on-same-path record beats a higher cosine hit in another file.

`bm25` and `vector` stay equal *inside* a profile. Optional RRF (`1/(60+rank_fts) + 1/(60+rank_knn)`) may replace those two terms when claim vectors exist. Same weights file, measured, not taste.

### HEAD coverage

```
score =
    2.0 * (type_rank / 5)
  + 1.2 * path
  + 0.2 * recency
  - 0.7 * stale
```

No `bm25`. No overlap flags (the query is empty). Type and (if present) path decide. Recency breaks ties.

Weights are named constants in `internal/retrieve/weights.go` and `profile.go`. A mix change that breaks a required fixture is a failed change.

No learned ranker in v1.

---

## 5. Pack

Sort candidates by `score` desc, then `created_at` desc, then `id`.

Walk with **coverage, then score**:

1. If any `failed_overlap == 1`, pack the highest-scored of those first (job 1 cannot lose to the cap).
2. Repeatedly pack the remaining candidate that maximizes `score - 0.8 * max_sim(already packed)`.
3. `sim` is 1.0 if same type and (HEAD, or sharing a path basename). Else text Jaccard.
4. On a score tie, prefer the lower sim (uncovered type/path).
5. Drop `score <= 0` after the first kept hit.
6. **Diversity:** skip a record if its `claim_hash` or normalized text Jaccard to an already-packed hit is ≥ 0.8.
7. Estimate tokens as `ceil(json_len / 4)` of the `AskHit` object. Stop when the next hit would exceed `limit_tokens` (default 1200) and at least one hit is packed.
8. Hard cap **5** hits.

### Warnings (first-class, not a side effect of rank)

After packing, scan the **packed** hits plus any `failed_overlap=1` candidate that was cut only by the cap:

| Condition | Warning |
|-----------|---------|
| Any packed or uncapped-but-overlapping `failed` | `A prior attempt at this goal failed (see {id}). Do not repeat it without new evidence.` |
| Any packed `decision` with `shipped_overlap=1` | `Existing implementation may already cover part of this goal (see {id}).` |
| Any packed `constraint` with `shipped_overlap=1` | `A standing constraint applies (see {id}). Do not violate it without an explicit override.` |

If a `failed_overlap` record did not fit in the 5, **evict the lowest-score non-failed packed hit** and insert it. Job 1 is not allowed to lose to the cap.

Warnings must cite ids that appear in `context`. If you warn, the record is in `context`.

---

## 6. `GET /v1/records/:id`

Return the full claim. If `transcript_ref` is set, also return the redacted excerpt (the chunk covering that span). If the session file is gone, omit the excerpt. Do not 404 the claim.

`ask` does not include excerpt text.

---

## Eval

The engine is not done until the fixture suite passes. Fixtures live in `eval/ask/` (to be filled). Each case is:

```json
{
  "id": "rate-limit-failed",
  "store": "eval/stores/acme-api",
  "request": {
    "project": "acme/api",
    "question": "what do we know about rate limiting on auth?",
    "goal": "add rate limiting",
    "paths": ["src/middleware/auth.ts"]
  },
  "must_include_types": ["failed"],
  "must_include_substrings": ["Redis"],
  "must_warn": "failed",
  "must_not_include_substrings": ["billing invoices"]
}
```

### Required cases (the 20)

Seed store: the acme/api records already in `test/ask.eval.test.ts`, plus extras called out below.

| # | Ask | Must retrieve | Must warn |
|---|-----|---------------|-----------|
| 1 | goal=add rate limiting, path=auth.ts | Redis **failed** | failed |
| 2 | "why not jsonwebtoken" | jose **decision** | shipped |
| 3 | project key `Acme/API` vs `acme__api` | same ids | — |
| 4 | cold ask, no question | failed/decision first, not billing **state** | — |
| 5 | auth.ts mtime newer than stored | same decision, `[verify]` prefix; still present on next cold ask | — |
| 6 | goal=add rate limiting, no paths | still hits Redis failed (token overlap) | failed |
| 7 | goal=export invoices, path=billing/export.ts | billing state, **not** Redis failed as #1 | — |
| 8 | "Authorization headers" | never-log **constraint** | — |
| 9 | empty store | empty context, no warning | — |
| 10 | two restatements of the jose decision | pack **one** (diversity) | — |
| 11 | 8 other high-BM25 decoys + 1 failed-on-path | failed still packed (cap eviction) | failed |
| 12 | superseded jose claim + newer jose claim | only the active id | — |
| 13 | secret string in a transcript, ask for it | zero hits containing the secret | — |
| 14 | path-only cold ask on auth.ts | auth records, not billing | — |
| 15 | question uses "JWT library" only, **no** path, claim has no `jwt` token | jose decision via **vector** kNN | shipped |
| 16 | failed on auth from 200 days ago, recent failed on billing | rate-limit ask prefers **path** overlap over the old failed | failed (auth) |
| 17 | `limit_tokens=80` | ≥1 hit, tokens ≤ 80 | — |
| 18 | GET record id of Redis failed | body + excerpt if ref present | — |
| 19 | ingest Grok JSONL, ask as if Claude | same ids (portability) | — |
| 20 | 10k decoy claims, 1 gold failed | gold in top 5, p50 < 500ms on the test machine | failed |

Cases 1–5 already exist as unit tests against the spike. They remain the gate after the rewrite. Cases 6–20 are the engine gate.

A weight change that breaks any required case is a failed change.

---

## What the engine explicitly is not

- Vectors **instead of** path/type. Cosine is one candidate source.
- Embedding **raw excerpts** or tool bodies.
- Hosted embedding APIs.
- Query expansion, HyDE, LLM rerank.
- Scanning all records.
- Persisting stale.
- Returning raw transcripts in `ask`.
- Cross-project retrieval (`project_key` is a hard filter).

If the embedder is missing, the engine still runs (A–E only). JWT paraphrase still works via `jwt`↔`jsonwebtoken`. “Add throttling” / “the cache idea we tried” needs vectors. Cases 1–14 must not depend on the embedder.

---

## Implementation notes

- Candidate gen is SQL / FTS / posting tables. Ranking is a tight loop over ≤ 200 rows.
- `stat` at most 30 paths, and only after a first-pass score without `stale`.
- Rebuild from export must restore posting lists, not only the claims table.
- Weights and caps (`CANDIDATE_CAP`, FTS 80, pack 5, half-life 14) live in one module.
- Claim vectors are written at derive time (same catch-up as claims). Rebuild: walk `export/**/*.md`, re-embed. Store `embed_model` in index meta; mismatch triggers rebuild.
- Language: this spec is language-agnostic. The daemon is **Go**. Local embeddings via `LOSSLESS_EMBED_CMD` (see `scripts/embed_minilm.py`) or a future in-process MiniLM. Not a cloud API. Not CGO. Missing model is degraded mode.

---
