# Changelog

## 0.1.7 — 2026-08-19

- Shape gates for extract residue. Truncated skips chopped leading file-fragments (`e_test.go …`), not a real `concurrent_test.go`-first failed; yaml-ish `tree:` dumps; and trailing chopped `0.1.` (not standing `4.0.` / `2.5.`). Extract-meta skips `still extracts;` / `still extracts.` with no object, `still store and pack` / `still stores and packs`, `lock the recap row`, and `recap-as-failed`. Inspect-status recaps skip by shape: `Live recent N are` / `Inspect recent on` leads, `recent_noise=` dumps, clock-time recap ids, recap-faileds in the recent window. `They found X: a, b, and c` is a review list; a They-found + Redis/path failed still stores, including one that says `in this session`. ProcessState leftovers skip only as type=state, not SkipProse. Contains-skips for `named locks` / `locks are tested` / `they found` / `inspect recent 8` / `a packed failed` are gone so a real named-lock or They-found failed still stores. Space-form "same failure twice" Redis, Authorization, I'll stick with JWT still-extracts, I'll cold-start, 0.1.3 remember, and slice-loop stay. Pack of five and 4.0 / 2.5 unchanged. `inspect --prune --project` supersedes already-stored residue for that project. 0.3 success (inspect recent 8 all obey-worthy) is not closed.

## 0.1.6 — 2026-08-19

- Watcher locates Claude cwd from the transcript (do not guess the project-dir slug). Unknown-cwd files still skip. `cleanupPeriodDays` is unchanged.
- Watcher tails OpenCode `opencode.db` so a missed plugin still copies the tape. Cap 16 sqlite sessions per tick.
- Codex desktop threads with empty or missing `rollout_path` still catch-up `first_user_message` when cwd is known. `sessions/` may be missing.
- Claude and Grok install `UserPromptSubmit` as write-only observe (fail-open, no retrieve, no `additionalContext`).

## 0.1.5 — 2026-08-19

- After compact catch-up, write `~/.lossless/active/<owner__repo>.md` from a real `ask`. Skill/rule: if that file exists and this turn has not asked, read it or call ask. Stop stays write-only. No Claude inject.

## 0.1.4 — 2026-08-18

- `Failed` is not a proper noun. Pathless "Failed work first…" does not extract or pack. `Failed to` / `Failure during` still ground. A Redis failed still grounds. Retrieve `extractNoise` uses the same `GroundedFailed` as write.

## 0.1.3 — 2026-08-18

- Gate README/roadmap residue so those sentences do not become faileds. Locks are the slogan (`failed work first, then`), the colon roadmap line (`the next product is:`), and `before it retries the failed work` — not `the next product is` / `retries the failed work` / `0.3 extract` (those swallow real claims). Real pathful faileds and `stick with` decisions stay.

## 0.1.2 — 2026-08-18

- Gate `I'll ask` / `I will ask` so planning narration is not stored as a decision.
- `ask` catch-up is store-first: omitted `session_id` still catch-up stored sessions for this workspace that are behind. A set-but-unknown id is exact locate only. Do not walk harness homes for newest mtime.

## 0.1.1 — 2026-08-18

- `ask` catch-up the asking session when the harness file is ahead of the cursor.
- Extract keeps everyday coding phrases (`use X, not Y`, `prefer X over Y`, `stick with`, tests don't pass).
- Gate advice-`failure` talk, unclosed `**` chrome, and README product copy so those sentences do not become faileds.
- `failed_overlap` needs a real symbol Jaccard (`OverlapSymbolMin`), not any shared token. 4.0 / 2.5 unchanged.
- `doctor` and `inspect` show last `ask` versus the tape.

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
