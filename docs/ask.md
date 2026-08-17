# `ask` contract

This is the portable read surface. Any harness or model sends current work. lossless retrieves and packs the past. The caller does not search or rank.

Default: `POST http://127.0.0.1:7432/v1/ask`  
MCP tools: `ask`, `remember`, `get_record` (same JSON as the REST bodies).

- HTTP MCP: `http://127.0.0.1:7432/mcp` (same process as `serve`)
- stdio MCP: `lossless mcp` (HTTP client of the local daemon)

`lossless setup` is the one command: local hooks + MCP + a **skill** and a short **home rule** (Grok and Claude) so the model calls `ask` on real work without the user typing `/lossless`. The rule is always in session context; the skill is the full procedure when the turn matches. `lossless doctor` checks both. Default is this machine only. `lossless migrate` is optional, later, if you already have a home URL.

No other read verbs in v1. Full record + excerpt: `GET /v1/records/:id`.

How records are chosen: [retrieval.md](retrieval.md).

## Request

```json
{
  "question": "what do we already know about rate limiting on auth?",
  "project": "acme/api",
  "workspace_root": "/Users/you/dev/api",
  "goal": "add rate limiting",
  "paths": ["src/middleware/auth.ts"],
  "limit_tokens": 1200
}
```

| Field | Required | Notes |
|-------|----------|--------|
| `question` | no | Natural-language question. Empty + empty `goal` is a **cold** ask. |
| `project` | yes* | `owner/repo` or `owner__repo`. *If omitted, derived from `workspace_root`. One of the two is required. |
| `workspace_root` | no | Absolute repo path. Required for file mtime `[verify]`. Identity is `project`, not this path. |
| `goal` | no | What the agent is about to do. Used for anti-repeat / anti-regression. |
| `paths` | no | Repo-relative files in play. Boosts records tagged with those paths. |
| `session_id` | no | Binds the action tape (last packs, GETs, remembers). If omitted, newest session for the project. |
| `limit_tokens` | no | Max packed tokens. Default `1200`. Approx 4 chars = 1 token. |

`project` is normalized: strip `.git`, lowercase, accept `owner/repo` or `owner__repo`.  
`https://github.com/Acme/API.git` and `git@github.com:acme/api.git` are the same store: `acme/api`.

## Response

```json
{
  "context": [
    {
      "id": "01J...",
      "type": "failed",
      "text": "Redis token bucket failed in staging; connection pool exhausted.",
      "when": "2026-08-01T18:12:00Z",
      "paths": ["src/middleware/auth.ts"],
      "harness": "grok",
      "status": "active"
    }
  ],
  "warnings": [
    "A prior attempt at this goal failed (see 01J...). Do not repeat it without new evidence."
  ],
  "tokens": 412,
  "project": "acme/api"
}
```

| Field | Meaning |
|-------|---------|
| `context[].type` | `decision` \| `failed` \| `constraint` \| `state` \| `thread` \| `excerpt` |
| `context[].text` | The durable claim. This is what the model should read. |
| `context[].status` | `active` or `superseded`. `[verify]` is a **prefix on `text`**, not a persisted status. |
| `warnings` | Anti-regression signals. Treat as blocking unless the user overrides. |
| `tokens` | Estimated tokens of `context` + `warnings`. Always `<= limit_tokens`. |

## Ranking (owned by lossless)

See [retrieval.md](retrieval.md). Short version:

1. Don't repeat failed work.
2. Don't regress shipped work.
3. Answer the question.

Thin ask (`question` and `goal` empty, no paths): compile from the session tail if present. If still empty, pack project HEAD (type-capped failed/decision/constraint). Age never drops a claim. Recency is a tie-break.

Stale files: ephemeral `[verify]` prefix. Do not persist stale.

## Errors

| HTTP | When |
|------|------|
| `400` | Missing `project` and `workspace_root`, or invalid JSON |
| `200` | Always for a valid ask, including empty `context` |

Empty context is not an error. It means the store has nothing relevant.

## Skill rule (every harness)

Before implementing or changing behavior, call `ask` with the current goal and paths. Treat `warnings` as blocking unless the user overrides. After compact or on a new session, ask again.
