# Install prompt

Paste the block below into a coding agent. The agent does the rest.

```
Install lossless from https://github.com/jbgh/lossless.

1. Install the binary (macOS or Linux):
   curl -fsSL https://github.com/jbgh/lossless/releases/latest/download/install.sh | sh
   If that is blocked, clone and build:
   git clone --depth 1 https://github.com/jbgh/lossless.git /tmp/lossless &&
   cd /tmp/lossless && go build -o lossless ./cmd/lossless &&
   mkdir -p "$HOME/.local/bin" && mv lossless "$HOME/.local/bin/lossless"
2. Put "$HOME/.local/bin" on PATH if lossless is not found.
3. Run: lossless setup && lossless doctor
4. If doctor is not all ok, fix what it reports. Do not skip the daemon.
5. Tell the user to start a new agent session so MCP tools appear.
   Grok: /hooks then r. /skills then r if this session is already open.
6. On later real work in a repo, call the lossless MCP tool ask before
   implementing or changing behavior. Send workspace_root, goal, and paths.
   Do not wait for the user to type /lossless.

Default install is this machine only. Nothing is uploaded.
Do not auto-inject packs. Do not retune retrieve weights.
```
