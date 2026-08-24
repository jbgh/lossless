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

The chat window is lossy. The session tape is not. lossless is the checkout: a small `context` of past work for **this** goal and files. The user should not have to type `/lossless` or say "ask memory."

The MCP tool is `ask` (Grok may show `lossless__ask`). Hooks already write the tape. Your job is the read.

## When to call ask

Call **once at the start of real work**, before the first edit or design:

- implementing or changing behavior
- new session, or the window was just compacted
- "continue", "don't regress", "what did we decide", "why not X"
- you are about to touch files that have a past in this repo

Call **again** when the topic or files change (auth → billing is a new ask).

If `~/.lossless/active/<owner__repo>.md` exists and this turn has not asked, read it or call ask. That file is a checkout from the last compact (cites with `id` / `has_excerpt`). It is not a substitute for ask on a new topic.

## When not to call

- trivia, chit-chat, or "what's in this file"
- you already asked this turn for the same goal and paths
- you are only answering from the context you just got

## How to call

```
ask({
  workspace_root: <absolute path of this git checkout>,
  goal: <one sentence: what you are about to do>,
  question: <the user's ask, or "what must I not forget">,
  paths: [<repo-relative files you will touch>],
  session_id: <harness session id>
})
```

`workspace_root` is this repo's checkout (the one with `origin`), not a sibling clone. `project` is optional then — lossless derives `owner/repo` from origin. Do not invent a search query. Do not rank.

Pass `session_id` whenever the harness has one (Grok/Claude/Codex/Pi/OpenCode session id). Omit it only if the harness did not give one. Do not send `default`.

Send current work. lossless returns ≤5 records in `context`.

## After context returns

The pack is a bibliography of at most five cites, not the tape. Packed `text` is the cite sentence. `has_excerpt` means `get_record` can open the source turn.

- Treat `warnings` as blocking unless the user overrides.
- Do not repeat a `failed` without new evidence.
- Do not undo a `decision` or violate a `constraint`.
- If a warning says a decision may already cover, or the packed sentence is not enough to act, call `get_record` on **that one** id when `has_excerpt` is true. Do not GET all five.
- `remember` only for a durable fact the tape will miss. It does not replace catch-up.

You are not searching memory. You are telling lossless what you are doing so it can keep the window honest.
