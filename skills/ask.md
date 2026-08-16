# lossless

Before implementing or changing behavior, call lossless `ask` with the current goal and paths.

Prefer the MCP tool `ask` when it is available (same fields). Pass `session_id` when the harness has one so a thin follow-up ask can reuse the last pack and any `get_record` dwells. Otherwise:

```
POST http://127.0.0.1:7432/v1/ask
{
  "question": "<what are you about to do>",
  "project": "<owner/repo if known>",
  "workspace_root": "<absolute repo path>",
  "goal": "<current goal>",
  "paths": ["<files you will touch>"]
}
```

Treat `warnings` as blocking unless the user overrides. After compact or on a new session, ask again.

If the daemon is not up: `lossless serve`.

See `docs/ask.md`.
