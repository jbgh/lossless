# Changelog

## 0.1.30 — 2026-09-21

- `ask` recurrence warnings: an ask that shares no vocabulary with a standing trap can now still be warned. The 2026-09-21 audit-bundle miss had five prior trips of `pr-size-check` in FTS candidacy that died post-candidacy, and every warning route needed token, path, or symbol agreement with the ask. After emit, a store-level scan groups the project's active failed + constraint records of the last 60 days by code-shaped identifier (contains `-`/`_`/`.`, ≥5 chars, separator variants count as one trap; version numbers, ticket ids, file suffixes, and the project's own name are not traps), and the newest-rule cluster warns when it has ≥3 but at most max(10, 5% of the window) records, spans ≥2 distinct days or sessions (one session's retry burst is not recurrence), and contains at least one constraint. Firing is on cluster existence, not ask relevance — any ask-side gate is either too porous (stopword FTS hits) or misses exactly the incident it exists for; a freshly written standing rule surfaces on every ask until a newer one is written. The scan reads the store directly, so gate-named faileds count toward the trigger without entering a pack; if a member constraint already packed with the standing-constraint warning, the recurrence warning stays out. At most one per ask.
- Constraint pack rescue (`evictConstraint`): a strong constraint that lost the PackCap race can now evict in — shipped_overlap, a gate-level vector hit, or top-of-FTS bm25 ≥ 0.8 with at least two FTS candidates (rank 1 of 1 normalizes to bm25 = 1 regardless of match quality, so a lone FTS hit is not "top of FTS"). Never pathlessness or bare candidacy, or a noisy project's pathless constraints become a faucet. A bm25-rescued constraint ships as context only; the warning still requires shipped_overlap, so rescue cannot mint warnings on rank. Neither rescue evicts a job-1 failed or a warning-bearing record, and the budget walk stops rather than force-evicting a warning when nothing evictable remains.
- Weak-path parity: harnesses send agent-guessed paths, and guessed paths that match nothing were dead recall. A pathed ask whose path route returned zero rows now runs the same two-hop inference as a pathless ask. Scoring unchanged: hopped records on a dead-path ask keep the `oon=1` discount a pathless ask does not have.
- New bench: seven replay tests against stores shaped like the live memora miss — the audit-bundle incident, the clone-failure cluster, and negatives (no-constraint clusters silent, cross-project silent, no double warning, rarity cap). The Tailscale-stale-constraint case is an expected-fail for the constraint lifecycle follow-up; the negated-status-report false warning is the other recorded follow-up.
- Docs: algorithm.md §6-8 (weak-path parity, recurrence warnings, evictConstraint), retrieval.md and pipeline.md carve-out for the recurrence warning citing an id outside `context`, ask.md token carve-out, architecture.md emit step.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.29 — 2026-09-21

- OpenCode: a reply that is still streaming no longer reaches the tape. The dump renders the whole session on every catch-up and the copy is by byte cursor, while the watcher polls `opencode.db` mid-turn: a step rendered as `"content":null` (53 bytes) grew into its reply under the cursor, and the next copy started 53 bytes into the line. 37 of 155 live OpenCode tape lines were front-truncated JSON (`e":"text","text":"A look at the repo …`) that parse dropped, so the model's prose from 15 of 20 sessions never reached extract; 99 more lines were `"content":null`. The dump now stops at the first message that can still change: an assistant step counts once it has `time.completed` or an `error`, or once a later step exists (144 of 34,596 live steps died without either). Steps with nothing for the tape are skipped.
- OpenCode tool output is on the tape. The dump read `output` at the top of a tool part; OpenCode keeps it under `state.output` (`state.error` on a failed call), so no tool result had ever been copied. It lands as a `tool_result` part with the tool's name, clipped like a long turn, and stays out of claim prose like every other harness's tool output.
- Catch-up resets a cursor that is not on a line boundary the way it resets a cursor past EOF: seal the live tape and recopy from zero. Catch-up only advances over whole lines, so an append-only source never trips it; a source rewritten in place without shrinking did, and wrote a front-truncated line. This is also what moves an OpenCode session captured by 0.1.28 onto the new dump.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.28 — 2026-09-18

- Watcher: a tick no longer runs `git` for every idle session. `Discover` lists each OpenCode (and Codex desktop) session from the harness database with a workspace and no project, and `idleSeal` resolved every one through `git remote get-url` on every tick: on a store with 1,087 OpenCode sessions a one-second tick took 38 to 56 seconds and spawned about a thousand `git` processes, nonstop, since 0.1.0. A session the store knows reuses its stored project; a new workspace resolves once per ten minutes. The same tick now takes about 50ms.
- Extract is deterministic past the cap. Drafts were deduped through a map and ranged over, so a batch with more than twelve drafts kept a random subset: the same transcript gave five different results in five runs. Ties keep transcript order.
- `inspect` and extract-trace clips never cut a rune. An em dash at byte 88 produced invalid UTF-8 and made `grep` treat the whole report as binary.
- Gates: `I'll dispatch …` / `I'll re-dispatch …` / `I'll wait …` are planning; `failure-reason` is an object, not a failure; a sentence quoting ask's own `prior attempt failed` warning is meta talk; `the one-string revert` names a planned change, while `we had to revert …` still stores.
- `serve` usage and docs say that it watches harness session files unless `--watch=false`. A throwaway `serve --home /tmp/x` otherwise ingests every live session on the machine into `/tmp/x`.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.27 — 2026-09-18

- 0.1.26 was tagged but never released: its release run failed on a version literal pinned in `TestMainDispatch` (the test now compares against `version.Version`). This release carries the object-storage backup below.
- `ask` warnings: the word route to a strong overlap scales with the goal. Two shared content tokens still warn on a goal of up to sixteen; past that the bar rises by one per eight. Function words (`after`, `only`, `every`, three-letter words like `are`) and the project's own name do not count. Replaying September's 34 live asks, word-route warnings fell from 45 to 19 of 130 (`review`/`look`, `wave`/`count`, `lossless`/`mcp` gone; `account cancel stripe`, the backup design rows kept). Path, identifier, symbol, and vector routes are unchanged.
- Parse: a Claude Code subagent hand-back (`<agent-message>`, "[Subagent hand-back] …") and a turn that is only a `<task-notification>` extract as assistant text, without the harness frame. One memora session held 166 hand-backs whose self-reports (`Never ran approve-ci.sh, never merged, never added labels.`) stored as user constraints. A user who types next to a notification is still the user.
- Sentence split: a terminator inside a code span does not end the sentence, nor does the dot that opens a dotfile or relative path; a newline closes an unbalanced tick. ``Never touched `.woodpecker/**`.`` had stored as `Never touched .`. `Truncated` no longer drops every sentence that ends in a code span; the unbalanced-tick and object-less ` .` shapes still skip.
- Gates: a success report (`1,794 passed / 0 failed / 11 skipped`, `with no failures`) is not a failed, at extract and at read time; a sentence ending in a colon is a lead-in; `Looking into …` / `Diagnosing …` / `Investigating …` narration skips; a turn-scoped wait (`nothing independent to request this turn`, `arrives by notification`) is not state.
- `inspect`: a session whose source file is no longer on disk (Claude cleanup, a cleared `~/.grok/sessions`, swept staging) collapses into `N source gone, tape kept`, the cursor column reads `1 behind 4 ok 125 gone`, and a path-hash project with no live source folds into the `path-*` line. The live overview went from 181 lines to 29.
- Watcher: the spool-replay line logs only when a replay moved tape or failed, once per distinct failure, stamped with UTC time and version like `serve`. 103 of 193 `serve.log` lines were no-op replays: a turn hook gives the daemon 400ms, spools past that, and the daemon has finished by the time the tick replays the job.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.26 — 2026-09-17

- `backup init`, `backup`, and `restore`: opt-in encrypted copy of `raw/`, `export/`, and `VACUUM INTO` index snapshots to an S3-compatible bucket (AWS, Cloudflare R2, B2, MinIO). Per-file objects under HMAC names, chunked AES-256-GCM with a per-object key, manifest written last, the last five generations kept and objects deleted only when no kept generation references them. `restore --list` and `restore --at` pick a generation. A writer guard refuses a bucket last written by another install; `restore` adopts it, `--take-over` overrides.
- `serve --watch` runs backup on a due time from the last success (two minutes after start if overdue, retry in fifteen minutes on failure); `backup init` schedules hourly by default. `doctor` prints the backup line.
- Hand-rolled SigV4 client with signed payloads and region `auto` on R2 hosts; no new modules.
- Review fixes before release: a drop left pending by an earlier run protects every generation the pointer still lists (with `keep=1` it deleted objects the current generation referenced); the scheduler survives a panic in one run, logs drop failures, recomputes the due time when the interval or `last_ok` changes, and reports an empty store as `nothing to back up` instead of a success; a session token is used only with the `AWS_*` pair; a config error stamps `last_error` for `doctor`; a file removed mid-walk is skipped; `remember` takes the part lock; `restore` treats only lossless's `/health` JSON as a running daemon; a refused redirect fails at once with a region hint.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.25 — 2026-09-11

- `hook-claude` routes a Grok session back to the Grok adapter. Grok 1.0.13 runs the Claude-scope hooks and passes its `updates.jsonl` event stream as `transcript_path`; eight live sessions were stored as `claude` on that file. The locate maps it to the sibling `chat_history.jsonl` with harness `grok`; catch-up refuses `updates.jsonl` outright; `inspect --prune` drops the stored rows (session, cursor, and only that harness's claims).
- The watcher replays the hook spool on every tick and sweeps `virtual-*.jsonl` staging files older than a day. 74 spooled catch-ups and 1,088 staging files had waited three weeks for a manual `ensure`.
- `install-hooks` retargets a stale lossless hook command (dev-tree binary, old install path) instead of counting any `hook-claude` as installed; a duplicate lossless entry is dropped and foreign hooks stay. `doctor` reports hook target drift (`claude → /path/lossless (not this binary; lossless install-hooks)`) instead of `hooks ok`.
- Parse clips a long turn on sentence boundaries: 4KB stays whole, longer turns keep a 1.5KB head and tail cut at a terminator. The 400/400 mid-word tail was the source of every leading chop (`al/bench_test.go 0.95 floor …`, `n LLM process; …`) and dropped the middle of every subagent report. Claude `queue-operation` enqueues parse as user turns; a `<task-notification>` keeps only its `<result>` body, so `Background command "…" failed with exit code 1` summaries are no longer faileds.
- Gates: a sentence wrapped in one harness tag (`<summary>…</summary>`), test-runner status lines (XCTest `Executed N tests, with N failures`, `Test Suite '…' failed at`, go `--- FAIL:`, `npm ERR!`), and clip chops opening on a non-word (`e verbObjRE's`, `ve origin/main\``, `al/bench_test.go`, `or \`sharedCodeIdent\``) skip at extract and read time. `os/exec failed`, `ok so redis failed`, `env exists; do not print secrets` stay.
- `serve` waits up to 15s for the port while the old daemon drains after `launchctl kickstart -k`, and stamps its log lines with a UTC time and version. `inspect` collapses caught-up sessions into one count per harness and prints only rows that need an operator.
- `release-notes.sh` matches the version field exactly (`v0.1.2` no longer returns the 0.1.25 section). CI pins read-only permissions, a 15-minute timeout, `gofmt -l`, and `go vet`.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.24 — 2026-09-01

- Parse skips Claude Code chrome that arrives as user text: skill bodies (`Base directory for this skill:`), slash-command tags (`<command-name>`, `<local-command-stdout>`), `isMeta` shell output, and `isSidechain` subagent prompts. `isCompactSummary` marks the compact and is not re-extracted. A real user constraint in the same turn still stores.
- Continue needs a shared primary path (a root file counts; a derived basename like `main.go` does not). Strong overlap judges this ask's own symbols; inherited tape symbols rank but never force-pack. Two-hop infers files by full path. The one-content-token targeted rule needs an interrogative (`why not jose` warns, `add tests` does not); the symbol-Jaccard route needs two shared symbols.
- Thin asks compile from the asking session's own tape (this month or last), not the newest tape on the project.
- `remember` rows bypass every read-time gate; attached decision paths are trusted. Decisions ground on package frames (`replaced X with Y`, `keep using Y`, `Decision: Y`, `standardizing on` / `switched to` / `migrating to` / `settled on`). Process words (`step`, `plan mode`, `whatever`), `OK`/`FYI` acronyms, curly quotes, `Picked it over that`, and plain kebab words (`follow-up`) do not ground. Arrow chrome covers the arrow blocks only (`⭐` / `➕` are prose) and spares user-typed sentences and constraints.
- `evictFailed` copies its input and judges diversity against packed faileds only. `get_record` and `remember` carry `session_id` over MCP so dwell lands on the calling session. Dead `warned` hydration retired.
- Pack of five and 4.0 / 2.5 unchanged.

## 0.1.23 — 2026-09-01

- Parse locates harness chrome tags with a byte-preserving ASCII fold. `İ` or `K` in a user turn no longer panics the watcher; a tick that panics is recovered and logged, never fatal to the daemon.
- Posting channels return newest first (`ORDER BY record_id DESC`). A busy file's 40 newest faileds are the job-1 candidates, not its 40 oldest.
- Session files past 64MB ingest in 64MB deltas instead of being refused outright.
- Redact: PKCS#8 and OpenSSH private keys, Slack and Discord webhooks, `redis://` / `amqp://` / `mssql://` credentials, `Authorization: Basic`, `gho_` / `ghs_` / `ghu_`, `glpat-`, `ASIA`, `rk_live`, SendGrid, lowercase `bearer`, and name-keyed `password=` / `AWS_SECRET_ACCESS_KEY=` / `api_key:` values that look generated. `token: jsonwebtoken` stays. `remember` rejects secrets in `why` and `symbols` too.
- `update` never downgrades: a running build ahead of the latest release installs nothing unless `--version` pins a tag.
- Home push honors `accepted_through` on 200: a partial accept requeues the remainder from home's offset instead of dropping the job and wedging the cursor.
- Pack of five and 4.0 / 2.5 unchanged.

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
