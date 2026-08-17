---
name: lossless
description: >
  Call lossless ask before implementing, changing behavior, or continuing
  after compact. Use when the user wants work done in a repo, when they
  say continue / don't regress / what did we decide, or after a new
  session or compact. Do not wait for the user to mention lossless.
when-to-use: >
  implementing, changing code, after compact, new session, continue,
  don't regress, what did we decide, why not, pick a library, fix the
  bug we already hit
---

# lossless

The chat window is lossy. The session tape is not. lossless is the checkout: a small pack of past work for **this** goal and files. The user should not have to type `/lossless` or say "ask memory."

The MCP tool is `ask` (Grok may show `lossless__ask`). Hooks already write the tape. Your job is the read.

## When to call ask

Call **once at the start of real work**, before the first edit or design:

- implementing or changing behavior
- new session, or the window was just compacted
- "continue", "don't regress", "what did we decide", "why not X"
- you are about to touch files that have a past in this repo

Call **again** when the topic or files change (auth → billing is a new ask).

## When not to call

- trivia, chit-chat, or "what's in this file"
- you already asked this turn for the same goal and paths
- you are only answering from the pack you just got

## How to call

```
ask({
  workspace_root: <absolute repo path, usually cwd>,
  goal: <one sentence: what you are about to do>,
  question: <the user's ask, or "what must I not forget">,
  paths: [<repo-relative files you will touch>],
  session_id: <if the harness gave you one>
})
```

`project` is optional if `workspace_root` is set. Do not invent a search query. Do not rank. Send current work; lossless packs ≤5 records.

## After the pack

- Treat `warnings` as blocking unless the user overrides.
- Do not repeat a `failed` without new evidence.
- Do not undo a `decision` or violate a `constraint`.
- `get_record` only if you need the full claim behind a hit id.
- `remember` only for a durable fact the tape will miss. It does not replace catch-up.

You are not searching memory. You are telling lossless what you are doing so it can keep the window honest.
