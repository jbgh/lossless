# Retrieval engine

This is the read-path spec. `docs/ask.md` is the I/O contract. This file is how we pick the 5 records that go in the packet.

No LLM on the retrieve path. No network. Embeddings, when used, are **on-box** and only on **claims**.

---

## Jobs

`ask` does three jobs, in this order. Fusion weights exist to express the order, not to replace it.

| Priority | Job | Failure mode if we get it wrong |
|----------|-----|----------------------------------|
| 1 | Do not repeat failed work | Agent rebuilds the Redis limiter we already burned |
| 2 | Do not regress shipped work | Agent rewrites auth and drops `jose` |
| 3 | Answer the question | Agent cannot find "why we rejected X" |

Cold ask (`question` and `goal` both empty): skip job 3. Return recent high-priority claims for the project, path-boosted if `paths` is set.

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

**Design: hybrid, structure first.**

1. Filter / candidate with path, type, FTS, **and** claim-level kNN.
2. Rank with failed/path/type **outweighing** cosine.
3. Embed **claims only** (short texts, tens of thousands in a decade). Do **not** embed raw excerpts or tool bodies. That would recreate the "one giant index over forever" problem.

Grok's own memory is 0.7 vector + 0.3 BM25 because session notes are chatty. Ours adds path/type because this is a repo. Cosine is a candidate source and a feature, not the product.

On-box model (default): a small sentence embedder (e.g. `all-MiniLM-L6-v2`, 384-d). No API. Rebuildable from claim markdown if we change the model (store `embed_model` + `embed_dim` in index meta). Cold ask (no question/goal): skip vectors, same as skip BM25.

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

One row per redacted transcript span (session id + byte range). Built by the write path (`docs/write.md`). Used for `GET /v1/records/:id` and as a last-resort candidate source when claims FTS is empty.

Monthly partitions (`excerpts-YYYY-MM.sqlite`). `ask` opens the last 12 months plus the current session's partition. Older partitions attach only if claims miss.

v1 excerpt policy: chunk ~800–1600 chars, 200-char overlap. Full tool bodies live in raw; the excerpt index keeps head 400 + tail 400. Do not extract a claim from a chunk at retrieve time.

---

## Pipeline

```
ask request
  → normalize (project_key, tokens, paths, symbols)
  → candidate generation          # cheap, recall-oriented, cap N
  → feature extraction            # only on candidates
  → score = weighted sum          # named weights, below
  → stale mark (ephemeral)        # stat top-K only
  → pack                          # token budget, diversity, warnings
  → AskResponse
```

Target: **< 50ms** at 10k claims / 100k excerpts on a laptop, p50. **< 150ms** p95 at 100k claims.

Never scan the full project table. The spike's `listActive` is banned.

---

## 1. Normalize

From the request, build a `Query`:

```
project_key     required (from project or workspace_root)
question_tokens unicode word tokens, len > 1, lowercased
goal_tokens     same, from goal
path_keys       each paths[] as given, plus basename
symbols         tokens that look like identifiers (camel, snake, dotted, or a known path stem)
cold            question_tokens and goal_tokens both empty
lookup_text     question_tokens ∪ goal_tokens ∪ symbols   # used for FTS
```

Identifier heuristic: token matches `[A-Za-z_][A-Za-z0-9_]{2,}` or contains `/` or `.` and a file extension.

No LLM query expansion. No hand-built synonym list. Paraphrase ("JWT library" vs `jose`) is the vector candidate source. Symbols still matter when extract tagged them; vectors cover the cases extract missed.

---

## 2. Candidate generation

Build a set of at most **`CANDIDATE_CAP = 200`** record ids.

### Hot ask (not cold)

Union, then cap:

| Source | How | Cap before union |
|--------|-----|------------------|
| A. FTS | `lookup_text` against claims FTS, `project_key`, active. Order by BM25. | 80 |
| B. Path | posting list for each `path_keys` | 40 per path, 80 total |
| C. Symbol | posting list for each `symbols` | 40 per symbol, 80 total |
| D. Failed | all `type=failed` for project with created_at in last 180 days | 40 |
| E. Decision | all `type=decision` for project whose path or symbol overlaps query | 40 |
| F. Vector | embed `lookup_text` once; kNN on claim vectors, same `project_key`, active. Cosine. | 80 |

If A+B+C+F are empty, **do not** fall back to scanning all records. Return empty context. Empty is a valid answer.

If the embedder is unavailable (first run, model not downloaded), skip F and continue. Lexical+structure still works. Missing vectors is degraded mode, not a hard failure.

### Cold ask

Skip A. Take:

| Source | How | Cap |
|--------|-----|-----|
| D' | `failed` then `decision` then `constraint`, recency order | 30 |
| B' | path posting lists if `paths` present | 40 |
| F | `state` recency | 10 |

No FTS. No vectors. No excerpts.

### Dedup

By `id`. If two candidates share `claim_hash` (should not happen for active), keep the newest.

---

## 3. Features

Compute only on the candidate set. Every feature is in `[0, 1]` except `type_rank` (0–5).

| Feature | Formula | Notes |
|---------|---------|--------|
| `type_rank` | `failed=5, decision=4, constraint=3, state=2, thread=1, excerpt=0` | Same as schema |
| `recency` | `0.5 ** (age_days / 14)` | Half-life 14 days. Clock = request time. |
| `path` | Jaccard of query `path_keys` vs record paths (basename counts as a hit) | 0 if either side empty |
| `symbol` | Jaccard of query symbols vs record `symbols` | 0 if either side empty |
| `bm25` | min-max normalize BM25 over **this candidate set** | 0 if record was not an FTS hit |
| `vector` | cosine(query, claim) clipped to `[0,1]` | 0 if embedder off or record has no vector |
| `failed_overlap` | 1 if `type=failed` AND (`path>0` OR `vector>=0.55` OR token overlap of goal∪question with text ≥ 1 token len>3) | Job 1; vector gate is how paraphrase still warns |
| `shipped_overlap` | 1 if `type in {decision,state}` AND (`path>0` OR `symbol>0` OR same token overlap) | Job 2 |
| `stale` | 1 if `workspace_root` set and any tagged file mtime > stored `path_mtime` | Ephemeral. Stat at most the current top 30 by pre-stale score. |

Token overlap for `failed_overlap` / `shipped_overlap` uses `goal_tokens ∪ question_tokens` against `text` tokens. One hit is enough. This is a **boolean gate**, not a score.

Do not persist `stale`. Prefix `[verify] ` on `text` at pack time if `stale=1`.

---

## 4. Score

### Hot

```
score =
    4.0 * failed_overlap
  + 2.5 * shipped_overlap
  + 1.5 * (type_rank / 5)
  + 1.2 * path
  + 1.0 * symbol
  + 0.9 * bm25
  + 0.9 * vector
  + 0.8 * recency
  - 0.7 * stale
```

Job 1 outranks job 2 outranks "sounds like." A failed-on-same-path record beats a higher cosine hit in another file.

`bm25` and `vector` are **equal** features. We do not pick a winner between lexical and semantic; we union candidates and let structure decide. Optional RRF (`1/(60+rank_fts) + 1/(60+rank_knn)`) may replace the two 0.9 terms if mixing raw BM25 with cosine is messy in eval. Same weights file, measured, not taste.

### Cold

```
score =
    2.0 * (type_rank / 5)
  + 1.2 * path
  + 1.0 * recency
  - 0.7 * stale
```

No `bm25`. No overlap flags (query is empty).

Weights are named constants in one file (`weights.ts` / `weights.go`). Changing a weight is a one-line change plus a re-run of the eval. Do not bury them in `ask.ts`.

No learned ranker in v1.

---

## 5. Pack

Sort candidates by `score` desc, then `created_at` desc, then `id`.

Walk the list:

1. Drop `score <= 0` after the first kept hit.
2. **Diversity:** skip a record if its `claim_hash` or normalized text Jaccard to an already-packed hit is ≥ 0.8. Same decision restated five times is a packing bug.
3. Estimate tokens as `ceil(json_len / 4)` of the `AskHit` object.
4. Stop when adding the next hit would exceed `limit_tokens` (default 1200) and at least one hit is packed.
5. Hard cap **5** hits.

### Warnings (first-class, not a side effect of rank)

After packing, scan the **packed** hits plus any `failed_overlap=1` candidate that was cut only by the cap:

| Condition | Warning |
|-----------|---------|
| Any packed or uncapped-but-overlapping `failed` | `A prior attempt at this goal failed (see {id}). Do not repeat it without new evidence.` |
| Any packed `decision` with `shipped_overlap=1` | `Existing implementation may already cover part of this goal (see {id}).` |

If a `failed_overlap` record did not fit in the 5, **evict the lowest-score non-failed packed hit** and insert it. Job 1 is not allowed to lose to the cap.

Warnings must cite ids that appear in `context`. If you warn, the record is in the packet.

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
| 20 | 10k decoy claims, 1 gold failed | gold in top 5, p50 < 50ms on the test machine | failed |

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

If the embedder is missing, the engine still runs (A–E only). Eval case 15 may fail in that degraded mode; 1–14 must not.

---

## Implementation notes

- Candidate gen is SQL / FTS / posting tables. Ranking is a tight loop over ≤ 200 rows.
- `stat` at most 30 paths, and only after a first-pass score without `stale`.
- Rebuild from export must restore posting lists, not only the claims table.
- Weights and caps (`CANDIDATE_CAP`, FTS 80, pack 5, half-life 14) live in one module.
- Claim vectors are written at derive time (same catch-up as claims). Rebuild: walk `export/**/*.md`, re-embed. Store `embed_model` in index meta; mismatch triggers rebuild.
- Language: this spec is language-agnostic. The daemon is **Go**. Local embeddings via onnxruntime or a small sidecar, not a cloud API.

---
