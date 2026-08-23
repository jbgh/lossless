# lossless

Before implementing, changing behavior, or continuing after compact, call the lossless MCP tool `ask` (may appear as `lossless__ask`) with `workspace_root`, `goal`, `paths`, and `session_id` when the harness has one. Treat `warnings` as blocking unless the user overrides. After compact, if `~/.lossless/active/<owner__repo>.md` exists and this turn has not asked, read it or call ask. Do not wait for the user to mention lossless. Skip trivia and chit-chat. The user should not have to type `/lossless`.
