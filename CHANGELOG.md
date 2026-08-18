# Changelog

## 0.1.0 — 2026-08-17

First public release. Local work log for coding agents: keep the tape, check out five records.

### Product

- Catch-up copies harness session files into `~/.lossless/raw` (kept). Claims are a derived index. `ask` returns at most five records plus warnings.
- No LLM on retrieve. No hosted embeddings. Age never disqualifies a record.
- Default install is this machine: `127.0.0.1:7432`, no token, nothing uploaded.
- A remote home is documented, not automatic.

### Harnesses

- `lossless setup` writes hooks, MCP, a skill, and a short always-on rule for Grok, Claude, Codex, Pi, and OpenCode.
- `lossless doctor` checks the daemon, hooks, MCP, skills, rules, and the user service (launchd / systemd --user).
- Compact hooks wait for the raw copy. Turn hooks stay fail-open. Packs are not auto-injected.

### Visibility

- `lossless inspect` shows tape vs claims vs last packs. `--ask` explains a live retrieve. `--jsonl` reads a session file. `--prune` drops test ingest.

### Install and update

- Channel: [GitHub Releases](https://github.com/jbgh/lossless/releases) for `jbgh/lossless`.
- `scripts/install.sh` and `lossless update` download `lossless-<os>-<arch>`, verify `SHA256SUMS`, refuse non-https / off-host redirects, and rename over `~/.local/bin/lossless` (a dest symlink is replaced, not followed).
- `update` then retargets hooks and the user service. `doctor` does not phone home.

### Platforms

- `darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`
