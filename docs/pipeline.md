# Pipeline: who asks, what memory does, what happens after

Write path (`docs/write.md`) gets bytes in. Retrieval (`docs/retrieval.md`) ranks rows. This file is the **loop**: when a query is formed, who forms it, what lossless is allowed to be, and what the harness does with the packet.

This is the decision that decides whether we are a database or a second agent. We are neither a chatbot nor cass. We are a **work-context index**.

---

## Verdict

| Question | Answer |
|----------|--------|
| Is memory "smart"? | **No LLM inside lossless.** No second brain. Typed pack + warnings is the only intelligence. |
| Is memory "just a database"? | **Almost.** Indexes + a fixed ranker + a packer. Not raw FTS for the model to interpret. |
| Who writes the query? | **Nobody writes a search string as the product.** The harness or the model sends *work context* (goal, paths, last user text). Memory compiles that into FTS / path / symbol lookups. |
| Who is obligated to call? | **The harness layer** (skill always; hook when the harness can inject). If nobody calls, the store is a diary. |
| What happens after return? | Packet becomes an ordinary tool result (or one injected block). The **agent** decides. Warnings are blocking unless the user overrides. Then catch-up writes the turn as usual, **skipping our own ask I/O** so we do not remember our own output. |

Vectors and graphs are optional indexes later. They do not change this split.

---

## Three places intelligence can live

```
                    ┌─────────────────────┐
   work context     │   AGENT / HARNESS   │  when to ask, what we are doing,
   ─────────────►   │   (skill + hooks)   │  what to do with the packet
                    └─────────┬───────────┘
                              │ ask({ goal, paths, question })
                              ▼
                    ┌─────────────────────┐
                    │   AGENT-MEMORY      │  lookup, rank, pack, warn
                    │   (indexes+packer)  │  no LLM
                    └─────────┬───────────┘
                              │ { context[], warnings[] }
                              ▼
                    ┌─────────────────────┐
                    │   AGENT again       │  obey warnings, implement,
                    │                     │  optional remember()
                    └─────────────────────┘
```

| If we put this here | What we get | What dies |
|---------------------|-------------|-----------|
| **All intelligence in the model** (memory is raw FTS/grep) | Simple store. cass. | Every model writes a different query. Anti-regression is optional. We are not a product. |
| **All intelligence in memory** (LLM rerank, "figure out what they need") | Magical demos. | Network, cost, hook timeouts, a second agent that can hallucinate your past. Secrets leave the box if the reranker is hosted. |
| **Split (this design)** | Same packet from Grok, Claude, Codex. Warnings are deterministic. Store stays local and testable. | We must *force the ask* via skill/hook. Models that ignore the skill get nothing. |

Letta puts memory tools in the agent's hands (self-edit). claude-mem auto-injects compressed observations. Grok's own memory auto-searches on first turn and after compact. cass is a human search box.

We take Grok/claude-mem's **obligation** (something must call) and cass's **honesty** (no LLM in the store), and we pack for **anti-regression** (not "similar text").

---

## End-to-end loop

```
 harness running
    │
    ├─► WRITE (always, no query)
    │     Stop / PreCompact / SessionEnd / watcher
    │     → sidecar spools new JSONL bytes (local, fast)
    │     → sidecar POST /v1/append to home (async)
    │     → home appends raw/, derives excerpts + claims
    │
    ├─► READ (only when someone asks)
    │     1. Work context is assembled   ← harness or model
    │     2. POST /v1/ask                ← one round trip
    │     3. Memory pipeline             ← docs/retrieval.md
    │     4. Packet lands in the window  ← tool result or inject
    │     5. Agent acts                  ← obey warnings
    │     6. Later catch_up copies this turn, minus our ask I/O
    │
    └─► optional remember()
          Agent or human asserts a durable fact.
          Additive. Never a substitute for catch_up.
```

Write is push (hooks). Read is pull (ask). We do not retrieve on every tool call. We do not write on every ask.

---

## Who assembles work context

The product is **not** `q=rate limiting AND auth`. The product is:

```
{
  "goal": "add rate limiting",           // what we are about to do
  "paths": ["src/middleware/auth.ts"],   // files in play
  "question": "what do we already know?",// optional, human wording
  "workspace_root": "/…/api",
  "project": "acme/api"
}
```

Memory turns that into index operations. The caller never sees BM25.

### Caller A — the model (primary on Grok and Codex)

The shipped skill says: before implementing, call `ask` with the current goal and paths.

The **model** fills `goal` / `paths` / `question` from the user turn. That is "who writes the query" in practice: the same agent that is about to edit code.

This is the only portable path. Grok cannot hook-inject. Codex has no PreCompact inject.

Risk: the model forgets to call. Mitigation: skill is global and short; Claude SessionStart can add one line "call ask"; evals that ignore the skill fail in real use, not in our unit tests.

### Caller B — the adapter (when the harness allows)

Used for **cold** moments when the model has not spoken yet or just lost the window.

| Moment | Who | Work context |
|--------|-----|----------------|
| Session start | adapter | `project` + `cwd`; `paths` = nothing or files from the last claim in this repo; no goal → **cold ask** |
| PreCompact / just-compacted | adapter | last human user line → `goal`/`question`; recent edited paths from the tail of raw → `paths` → **hot ask** |
| Human CLI | you | whatever you type |

If the harness can put the packet into the next prompt (`additionalContext` on Claude SessionStart / UserPromptSubmit), the adapter does that. If it cannot (Grok), the adapter writes `~/.lossless/active/<project_key>.md` and the skill says "if that file exists and you have not asked yet, read it or call ask."

The adapter **never** invents a clever search string. It copies last user text and paths. Memory ranks.

### Caller C — we do not do this

- Memory polling the harness on a timer and pushing unsolicited essays into the window.
- An LLM inside memory rewriting the user's goal into five hypothetical queries (HyDE).

---

## Once the request hits lossless

This is a database lookup plus a packer. Exact steps: `docs/retrieval.md`. In one picture:

```
ask(work context)
  normalize → project_key, tokens, path keys, symbols
  candidates (≤200)
      FTS(question+goal) if session-conditioned
    ∪ path postings
    ∪ symbol postings
    ∪ all active failed (no date gate)
    ∪ overlapping decisions and constraints
    or HEAD type caps if the query is still empty
  score each candidate (named weights, no model)
  mark [verify] if file mtime moved (stat top 30 only)
  pack ≤5, token budget, diversity
  if a failed-overlap missed the cap, evict something else
  warnings only for ids that are in the packet
  return
```

No scan of the whole store. No network. No embeddings in v1. Empty packet is valid.

`GET /v1/records/:id` is the only follow-up: full claim + raw excerpt. The agent asks for that, not memory deciding to dump the transcript.

---

## After the packet returns

1. **Land in the window.** Tool result on `ask`, or one injected block. Not a hidden system rewrite of history.
2. **Agent policy (skill, not memory):**
   - Read `warnings` first.
   - `failed` warning → do not repeat that approach unless the user says to.
   - `decision` warning → do not silently undo it.
   - If you need the original file-read or the paragraph around a claim → `GET /v1/records/:id`.
3. **Do the work.** Edits, tests, the usual harness loop.
4. **Optional `remember`.** Only when the agent (or user) has a durable sentence the extract heuristics will miss. Example: "We are not using Redis for rate limits, ever." Catch-up will still copy the turn; `remember` is for making that one sentence a first-class claim *now*.
5. **Catch-up later** copies the session log. **Skip** tool calls named `ask` / `remember` / `catch-up` and their results so the store does not ingest its own packets. If we do not skip, "everything remembered" becomes an echo chamber.

The packet is not written back as a new claim. It is already derived from claims.

---

## When we read (and the compact hole)

| When | Caller | Why |
|------|--------|-----|
| Before implementing | model + skill | Main anti-regression gate |
| New session | adapter cold ask + skill | Empty window |
| After compact | adapter hot ask if inject exists; else skill | Window just became a summary |
| User asks "did we already…" | model | Explicit |

**Hole:** auto-compact mid-turn. The next model call in the *same* turn may not go through UserPromptSubmit and Grok cannot inject on PostCompact. The model only has the compact summary plus the still-loaded skill. If it does not call `ask`, that turn is under-informed.

Mitigations, in order:

1. PreCompact catch-up has already saved raw (write path). Nothing is *lost*.
2. Skill: "If the conversation was just compacted, call ask before more edits."
3. Claude: next `UserPromptSubmit` injects a hot packet.
4. We do **not** pretend Grok PostCompact can inject.

So: compact cannot lose the past (write). Compact *can* lose a turn of awareness (read) until the model or the next user prompt asks. That is honest.

We do not retrieve on every `PostToolUse`. That would be expensive and noisy.

---

## Smarter later — what is allowed

Add **indexes**, not a brain.

| Add | When | Still |
|-----|------|--------|
| Claim-level vectors | **In the retrieval design now** (candidate source F + feature). On-box only. | Same `ask`, same packer. Path/type still win. |
| Excerpt embeddings | Never as the hot index | Raw stays files; excerpts stay lexical |
| Typed edges on claims | Already have path/symbol/supersede | Not a general knowledge graph |
| Query expansion / LLM rerank | Never | Would put a model in the store |

A graph of people/companies is gbrain. Out of scope.

If we ever want "smarter," it is a **better extract on the write path** (async, still local, still optional) so claims have the right `symbols[]`. Retrieval stays dumb and deterministic.

---

## Implications for the stack

This split does **not** change the Go decision. The hot path is:

- streaming file copy (write)
- SQLite FTS + a 200-row scorer (read)
- JSON HTTP

No in-process neural ranker in v1. If v1.1 adds local vectors, Go + a sidecar or onnxruntime is enough. We do not need Rust to "be smart."

---

## What we are not building

- Memory that decides when to speak.
- Memory that rewrites the agent's plan.
- Raw FTS as the only API (that is cass; the model becomes the ranker).
- An LLM in the retrieve or write hook.
- Auto-inject of the full raw log after compact.

---

## Pipeline eval (on top of write + retrieve cases)

| # | Case | Pass |
|---|------|------|
| P1 | Skill-shaped ask with goal+path returns failed + warning | same as retrieve #1 |
| P2 | Adapter cold ask (project only) does not require a question | retrieve #4 |
| P3 | Ingest of a session that called `ask` does not create a claim from the packet JSON | skip-list works |
| P4 | After compact, raw is complete even if the model never called ask | write path W3 |
| P5 | Model calls `GET` after ask | excerpt present, ask packet unchanged |

P3 is new and load-bearing. Without it, "everything" includes our own answers forever.
