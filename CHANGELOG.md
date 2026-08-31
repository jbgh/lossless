# Changelog

## 0.1.22 — 2026-08-31

- Decisions need a referent to extract: a path, tick/bold span, code-shaped token (camelCase, kebab like `react-query`, version, acronym, `jwt` alias), a mid-sentence proper noun, or a use-X-not-Y / picked-X-over-Y / X-instead-of-Y shape. `I'll stick with keep.` and planning narration skip; `I'll stick with JWT next` and `We'll use postgres next` still store. A neighbor sentence's path grounds — and attaches, so the row stays retrievable. Bare digits, `e.g.`, and pronoun-only instead-of do not ground.
- Unicode arrow and box-drawing sentences are chrome unless pathful or tick/bold-anchored (whole arrow + box blocks, not a character list). Workflow findings phrased with arrows still store as faileds.
- Read-time noise gates apply only to automatic extraction: `remember`, `import`, and unknown-provenance rows pack whatever their shape. FixtureTalk spares sentences naming a real fixture artifact (dotted file with a real extension, deep path, tick); `e.g.` and `read/write` do not spare self-talk.
- Foreign-repo bench: webshop (React/zustand/cypress) and pyapi (Python/Alembic/httpx) cases with planted noise that must not extract; mean-recall floor 0.95 gated in eval, mean counts only asking cases. `lossless bench` CLI homes read as test stores again — the scorecard had been silently refusing every fixture session (6/19); now 19/19, recall 1.00, contract pinned.
- `inspect --prune` superseded 13 legacy residue claims on the live store. Pack of five and 4.0 / 2.5 unchanged.

## 0.1.21 — 2026-08-31

- Strong overlap needs a code-shaped shared identifier (camelCase, digits, separators, `jwt` ↔ `jsonwebtoken`) or a one-content-token targeted ask (`why not jsonwebtoken`). One shared plain word (`staging`) is weak again: score only, no warning, no force-pack. Gold Redis / jose / tokenBucket asks still warn.
- `remember` rejects secret-bearing text before the manual tape or the store sees it. Messages catch-up redacts the virtual spool on write and removes it after ingest.
- Tokenless serve refuses a foreign Host or Origin (DNS rebind, cross-origin POST). Loopback clients and token-mode remotes unchanged.
- `inspect --ask` writes no ask/warn action rows. Observing the tape does not change the next pack.
- `evictFailed` respects `limit_tokens`: lowest score out first, job-1 faileds last, a lone record may still exceed. Home-push cursor advances only on a durable enqueue. `Append` check-and-set runs under the session lock. A lost claim-hash race indexes nothing — no orphan FTS, export, or vector. Excerpt lookup misses stop littering empty monthly shards. Local `make lossless` stamps the latest tag, not 0.1.12.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.20 — 2026-08-27

- Extract/ask skip I'll-call / I'll-shrink / I'll-point and `Let me check/get/look` planning. `Let me ask` / `I'll stick with` / `I'll clearly` stay.
- `That failed \`agent-verify\`` already-fixed talk and `The earlier failed … already superseded` skip as pack echo. Pathless Redis still stores.
- Mid-word chops (`tially but restrict…`) skip. `env exists; do not print secrets` stays.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.19 — 2026-08-24

- Watcher skips Claude `subagents/` and `agent-*.jsonl` dumps, including rows already in the session table. Nested JSONL with cwd that is not a subagent file still catch-up. Unknown-cwd still skips.
- Extract skips READ-ONLY (prefix, quoted, or `(READ-ONLY`) / APPROVE-or-REQUEST_CHANGES / `For each: title, severity` reviewer chrome and `Now I understand the failure`. Prefix `Lossless will` and `Lossless ask returned` are pack echo. Mid-sentence lossless will still stores.
- Child-loop leftover prose after a leading findings fence still extracts. `asked` must be true. Parse keeps findings by compacting the loop body, not a 32KB prefix.
- Ask/extract drop `tmp/` prefix and exact `qa-report.md`. `src/tmp/…` and `docs/qa-report.md` stay. Skill: omit `session_id` if the prompt does not have one; do not invent; do not send `default`.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.18 — 2026-08-24

- Extract lifts `findings[].issue` from child-loop JSON that has `asked` and `severity` as **faileds** (even `instead of` QA English). Leftover prose in the same turn still extracts. JSON `"severity"` shards skip. Ask packets stay skipped. Findings JSON without `asked` is not a loop body.
- Prefix `Lossless flagged` is pack echo. I'll-search / I'll-map / I'll-open skip as whole-word planning. `The failed X record is unrelated` and `Prior failure was another surface` skip. `I'll stick with` / `I'll clearly` / `I'll open-source` still store. Pathless Redis still grounds.
- Ask paths drop `/tmp` and `qa-report.md`. Sent `session_id=default` is omitted (project catch-up, not exact locate of a fake id). Watcher catch-up nested Claude JSONL when cwd is in the transcript; unknown-cwd still skips. Skill/rule: subagents pass their own session id; workflow children ask only if the tool exists.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.17 — 2026-08-24

- Extract: I'll-merge / I'll-clear / I'll-measure skip when the verb is a whole word (`I'll clear it`). `I'll clearly` / `I'll cleartext` / `I'll stick with` still store. Prefix `Lossless returned` is pack echo, same class as `Lossless flags`.
- Watcher writes the compact checkout when a **new** chunk (cursor already set) contains `type=compaction` — Pi's compact line. Grok compact does not write that line; Grok checkout is still PreCompact. A turn catch-up that copies that line still writes the pull file. Not an inject. Stop stays write-only.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.16 — 2026-08-24

- After compact, `~/.lossless/active/<owner__repo>.md` is a hot ask from **owned raw** (last user line + paths) and a bibliography (`id`, `has_excerpt`). Cite lines stay blockquoted. Written on PreCompact / compacting only, not PostCompact. Not an inject. The live harness file is not the input; compact checkout does not catch-up that file again while it may be shrinking. Stop stays write-only.
- Omitted `session_id` catch-up stored sessions for this `owner/repo` that are behind, with a budget. This workspace is first so other worktrees cannot spend the cap. A shrink still reset-on-shrink; the first-ingest cap does not apply. Exact locate only when `session_id` is set and unknown. Do not walk harness homes for newest mtime. A 17 MB first ingest still stays off this path.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.15 — 2026-08-23

- Skill and always-on rule: pass `session_id` when the harness has one; `workspace_root` is this git checkout. `get_record` opens one packed cite when `has_excerpt` and the sentence is not enough to act. MCP `ask` copy matches. Pack of five and 4.0 / 2.5 unchanged.

## 0.1.14 — 2026-08-23

- `FromWorkspace` finds git at a known absolute path when PATH is empty. A checkout with origin keys `owner/repo`; a checkout with no origin still keys `path-*`. `doctor` identity FAILs only when origin exists and the key is still `path-*`. The user service sets PATH.
- Extract: file stems (`LightboxView.swift`) become paths on that sentence. QA tap-failures, prefix `Lossless flags`, and I'll-match / I'll-slow / I'll-verify planning skip. `Search` / `Home` / `Albums` are sentence starters. Pathless Redis still grounds. Nearby stays user-only.
- Local debug JSONL at `debug/events.jsonl`: ask identity and catch-up extract skip counts. `inspect` prints the last lines. Not uploaded. `doctor` does not phone home.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.13 — 2026-08-20

- Claims cite the source turn on tape. Catch-up and append offsets are file-absolute. The cite is the message envelope. `get_record` opens the covering excerpt.
- `ask` packs `source` and `has_excerpt`, not excerpt text. A shipped-decision warning says to `get_record` that id before treating it as done.
- `remember` stamps a span on the manual line so `get_record` can open that page.
- `inspect` recent prints claim id and page / no-page.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.12 — 2026-08-20

- Drop remaining 0.1.8/0.1.9 recap contains-skips (`gates mostly match`, `inspect-recap`, `obey-worthy`, hyphen `still-store`, and the rest of that denylist). `go-first` mash is the comma form only.
- Shape skips stay: example-drop, They-found hyphen lock-lists, inspect-status leads, truncated recaps.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.11 — 2026-08-19

- Drop fail-closed recap contains-skips that swallowed real claims: `ok=false` health faileds, "this cut makes" product faileds, "never lose memo" inside memoization, space-form "inspect recap" inside recapture, and colon `still store:` session writes.
- Pathless Bench is an identifier again. A Bench failed still grounds.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.10 — 2026-08-19

- Concurrent catch-up retries SQLITE_BUSY on cursor, session, excerpt, and action writes so parallel session ingest does not fail the release job.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.9 — 2026-08-19

- Skip inspect-recap, gates-mostly-match, go-first mash, ok=false, hyphenated They-found lock-lists, and still-keep / still-ground extract-meta so those sentences do not become claims.
- Pathless Bench is a sentence starter, not an identifier. Pathful Bench still grounds when a path is present.
- Hyphenated still-store lock-list recaps skip. A concurrent_test.go-first keep is not a go-first mash.
- Pack of five and 4.0 / 2.5 unchanged. 0.3 (inspect recent all obey-worthy) is not closed.

## 0.1.8 — 2026-08-19

- Skip still-stores extract-meta (punctuation forms; "and pack" is not required) so those recaps do not become claims.
- Loop-residue keep-talk and example-drop recaps skip after list-marker and quote trim. A They-found Redis/path failed still stores.
- Chopped never-lose slogans and unclosed-paren fragments do not become claims.
- Pack of five and 4.0 / 2.5 unchanged. 0.3 (inspect recent all obey-worthy) is not closed.

## 0.1.7 — 2026-08-19

- Skip truncated recaps, extract-meta talk, and inspect-status dumps so those sentences do not become claims.
- Review-list "They found X: a, b, and c" is skipped. A They-found Redis/path failed still stores.
- Process-state leftovers skip only as type=state, not the shared skip list.
- Global contains-skips for named-lock and they-found phrases are gone. Real lock and They-found faileds still store.
- `inspect --prune --project` supersedes residue for that project only.
- Pack of five and 4.0 / 2.5 unchanged. 0.3 (inspect recent all obey-worthy) is not closed.

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
