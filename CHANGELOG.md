# Changelog

## 0.1.32 — 2026-09-22

- **Asks record the session that made them.** In Claude Code, lossless now reads the session id that Claude Code gives its MCP servers. It only does so when the server's parent process is Claude Code itself, so a Pi or OpenCode session started from a Claude shell does not borrow the Claude id, and a nested `claude -p` records its own. Claude subagents share their parent's MCP server and record under the parent session.
- **Pi and OpenCode attach their own session id.** The Pi extension and the OpenCode plugin add the harness's session id to every lossless `ask`, `remember`, and `get_record` call, including calls made through pi-mcp-adapter's proxy tools.
- **Made-up session ids are ignored.** A `session_id` sent by the model is kept only if it looks like a real harness id (a UUID, an OpenCode `ses_…` id, or a Claude `agent-…` id). A UUID with a suffix attached is trimmed back to the UUID. Anything else falls back to the harness fill.
- **Simpler instructions for models.** The tool descriptions and the skill no longer ask models to run `echo $PI_SESSION_ID`. Send `session_id` only when the prompt shows it. Grok and Codex have no automatic fill, so send it there whenever you have it.
- **Redaction removes the secret, not the whole line.** A transcript line containing a secret used to be replaced entirely with `{"_redacted":true}`, which also threw away any decision or failure in that turn. Now only the secret becomes `[redacted]`: a token, a connection-string password, the value of a `NAME=value` assignment, or a private key block. The blank runs to the end of the token, so no tail of the secret is left behind. A private key split across several JSON strings (as in an edit patch), and any line that is not JSON, still drop whole.
- **Fewer redaction false positives.** Placeholders (`<password>`, `${DB_PASSWORD}`, `$DB_PASSWORD`), code references (`process.env.API_KEY`, `session.refreshToken`), CI secret names (`from_secret: deploy_key`), package paths (`next/font`), and file paths or git refspecs are no longer treated as secrets. Each exception covers only that shape: weak snake_case passwords, passphrases, and base64 values that start with `/` are still redacted.
- **Less narration stored as memory.** Guesses about a failure ("the push may have failed", "Either … or …"), reverts offered to the user ("Revert them if you want …"), the agent's own rejected tool calls, and hypotheticals ("… would still pass") no longer become failed or decision records. Records of those shapes that are already stored stop appearing in asks, and `inspect --prune` retires them. A real failure or revert stated next to a guess still stores.
- **Project keys for self-referencing origins.** A repository whose `origin` points at its own `.git` directory got a malformed key ending in `/`. It now gets `parent/name`, like any other local-path origin. Records already stored under the old key are not moved automatically.

## 0.1.31 — 2026-09-22

- **Pi skill text no longer turns into constraints.** Pi injects skill documents into the conversation as user text, wrapped in `<skill …>` tags, and their rules and examples were being stored as user constraints. These wrappers are now stripped along with the rest of the harness markup. Tag stripping also handles tags with attributes, and leaves a user's own angle brackets alone.
- **MCP calls fill in a missing session id from the environment.** When a call omits `session_id`, lossless uses `LOSSLESS_SESSION_ID` or `PI_SESSION_ID` if either is set. The values `default`, `none`, `null`, and `undefined` count as missing. Pi does not expose `PI_SESSION_ID` to MCP servers, so this release also taught models to read it from the shell; 0.1.32 replaces that workaround.

## 0.1.30 — 2026-09-21

- **Recurring traps warn even when the ask does not mention them.** Every warning route used to need the ask to share words, paths, or symbols with the stored record, so a rule that kept being broken could go unmentioned. After packing, lossless now groups the project's active failures and constraints from the last 60 days by code-like identifier (a CI job, a flag, a script name). A group warns when it has at least three records, no more than max(10, 5% of the window), spans at least two days or sessions, and includes a constraint. There is at most one such warning per ask, and none when the constraint has already warned.
- **Strong constraints can take a pack slot.** A constraint that lost the race for the five slots can now displace a weaker record when it has shipped overlap, a strong vector match, or the top full-text score among at least two candidates. It never displaces a top failure or a record that carries a warning, and a constraint added on text score alone does not raise a warning.
- **Guessed paths no longer switch off inference.** Agents often send paths that match nothing. When none of an ask's paths match, lossless now runs the same two-hop inference it runs for an ask with no paths.
- **Bench:** seven replay cases for recurring-trap warnings, including negative cases (no constraint in the group, another project's records, double warnings, identifiers that are too common).
- Docs: the algorithm, retrieval, pipeline, ask, and architecture docs describe the new routes.

## 0.1.29 — 2026-09-21

- **OpenCode: a reply that is still streaming no longer corrupts the tape.** lossless renders each OpenCode session from `opencode.db` and copies the result by byte offset. A reply that grew between two reads shifted the lines after it, producing truncated JSON that was then skipped, so much of the model's text from OpenCode sessions never reached extraction. The render now stops at the first message that can still change: an assistant step counts once it has completed, errored, or been followed by another step. Empty steps are skipped.
- **OpenCode tool output is captured.** It was read from the wrong field and had never been copied. It is now stored as a clipped `tool_result`, as for other harnesses, and stays out of extracted records.
- **Catch-up recovers from a cursor that is not on a line boundary**, the same way it recovers from a file that shrank: it seals the tape and copies again from the start. This also moves OpenCode sessions captured by 0.1.28 onto the new format at their next catch-up.

## 0.1.28 — 2026-09-18

- **The watcher no longer runs `git` for every idle session on every tick.** Each OpenCode (and Codex desktop) session without a known project was resolved with `git remote get-url` every second. On a store with a thousand or so sessions, one tick took most of a minute and spawned `git` constantly. Known sessions now reuse their stored project, and a new workspace resolves at most once every ten minutes. Ticks are back to tens of milliseconds.
- **Extraction is deterministic.** When a batch had more than twelve candidate records, the subset kept was random. It now follows transcript order.
- **`inspect` never splits a multi-byte character** when it clips text, which had produced invalid UTF-8.
- Filters: "I'll dispatch / re-dispatch / wait …" is planning; `failure-reason` is a field name, not a failure; quoting ask's own "prior attempt failed" warning is not a new failure; "the one-string revert" names a change rather than reporting a revert, while "we had to revert …" still stores.
- `serve` usage and docs state that it watches harness session files unless you pass `--watch=false`.

## 0.1.27 — 2026-09-18

- 0.1.26 was tagged but its release build failed. This release includes the object-storage backup from 0.1.26.
- **Fewer word-overlap warnings on long asks.** The number of shared words needed for a warning now grows with the length of the goal, and function words and the project's own name no longer count. Path, identifier, symbol, and vector warnings are unchanged.
- **Claude Code subagent reports are read as assistant text.** Subagent hand-backs and turns that only carry a `<task-notification>` were read as user text, so a subagent's self-report ("never merged, never added labels") could be stored as a user constraint. A user typing next to a notification is still the user.
- **Sentence splitting respects code.** A period inside a code span, or the dot that starts a dotfile or relative path, no longer ends a sentence: ``Never touched `.github/**`.`` used to store as "Never touched .". Sentences that end in a code span are no longer dropped.
- Filters: test summaries with zero failures ("1,200 passed / 0 failed"), lead-ins that end in a colon, "Looking into / Diagnosing / Investigating …" narration, and waits scoped to the current turn are no longer stored.
- **Shorter `inspect` output.** Sessions whose source file is gone collapse into one count, and path-only projects with no live source fold into one line.
- The watcher logs a spool replay only when it moved data or failed.

## 0.1.26 — 2026-09-17

Tagged but not released; shipped in 0.1.27.

- **Opt-in encrypted backup:** `backup init`, `backup`, and `restore` copy `raw/`, `export/`, and index snapshots to an S3-compatible bucket (AWS S3, Cloudflare R2, Backblaze B2, MinIO). Each file gets its own key (chunked AES-256-GCM) and an HMAC'd object name, the manifest is written last, the last five generations are kept, and an object is deleted only when no kept generation uses it. `restore --list` and `restore --at` pick a generation. A bucket last written by another install is refused unless you `restore` from it or pass `--take-over`.
- `serve --watch` runs backups on a schedule: hourly by default, two minutes after start when overdue, and again after fifteen minutes when a run fails. `doctor` shows backup status.
- No new dependencies: the S3 client (SigV4 signing) is built in.
- Fixed before release: with `keep=1`, retention could delete objects the current generation still needed; the scheduler survives a failed run and picks up interval changes; an empty store reports "nothing to back up"; configuration errors show in `doctor`; files removed during a backup are skipped; `restore` only treats lossless's own `/health` response as a running daemon; a refused redirect fails at once with a region hint.

## 0.1.25 — 2026-09-11

- **Grok sessions captured through Claude hooks are filed correctly.** Grok 1.0.13 runs Claude-scope hooks and passes its `updates.jsonl` event stream as the transcript, so Grok sessions were stored as Claude sessions. lossless now maps them to Grok's `chat_history.jsonl`, refuses `updates.jsonl` outright, and `inspect --prune` removes the misfiled rows.
- **The hook spool replays automatically.** Catch-ups queued while the daemon was down used to wait for a manual `ensure`. The watcher now replays them on every tick and cleans up staging files older than a day.
- **`install-hooks` repairs stale hook commands** (an old binary path or a development build) instead of treating any lossless hook as installed, and removes duplicates while leaving other tools' hooks alone. `doctor` reports hooks that point at a different binary.
- **Long turns are clipped on sentence boundaries.** Turns up to 4 KB are kept whole; longer ones keep 1.5 KB from the start and 1.5 KB from the end. The old 400-byte mid-word clip caused records that started mid-word, and dropped the middle of long subagent reports.
- Claude `queue-operation` entries are read as user turns, and a `<task-notification>` keeps only its result, so "Background command … failed" summaries are no longer stored as failures.
- Filters: sentences wrapped in a single harness tag, test-runner status lines (XCTest, `go test`, `npm ERR!`), and clipped fragments that start mid-word.
- `serve` waits up to 15 seconds for its port during a restart and timestamps its log lines. `inspect` summarizes caught-up sessions and lists only the ones that need attention.
- `release-notes.sh` matches versions exactly. CI runs with read-only permissions, a timeout, `gofmt`, and `go vet`.

## 0.1.24 — 2026-09-01

- **Claude Code markup no longer becomes memory.** Skill bodies, slash-command tags, meta shell output, and subagent prompts that arrive as user text are skipped, and compact summaries are not extracted again. A real user constraint in the same turn still stores.
- **Ranking:** "continue" needs a shared primary path; strong overlap uses the ask's own symbols (symbols inherited from the session still rank but never force a record in); two-hop inference matches full paths; the one-word targeted rule needs a question ("why not jose" warns, "add tests" does not); symbol similarity needs two shared symbols.
- Short asks fill in from the asking session's own recent tape, not the newest tape in the project.
- **`remember` records skip every read-time filter**, and paths attached to decisions are trusted. Decisions also recognize "replaced X with Y", "keep using Y", "Decision: Y", "standardizing on", "switched to", "migrating to", and "settled on". Process words, acronyms such as OK and FYI, and plain hyphenated words do not count as a decision's subject.
- `get_record` and `remember` accept `session_id` over MCP, so the read is recorded on the calling session.

## 0.1.23 — 2026-09-01

- **Unusual characters can no longer crash the watcher.** Some Unicode letters (such as `İ`) in a user turn caused a panic. Tag detection now uses a byte-preserving fold, and a tick that panics is recovered and logged.
- Newest records come first: for a busy file, the 40 newest failures are the candidates, not the 40 oldest.
- Session files over 64 MB are ingested in 64 MB chunks instead of being refused.
- **More secret formats are redacted:** PKCS#8 and OpenSSH private keys, Slack and Discord webhooks, `redis://`, `amqp://`, and `mssql://` credentials, `Authorization: Basic`, GitHub `gho_` / `ghs_` / `ghu_` tokens, GitLab `glpat-` tokens, AWS `ASIA` keys, Stripe `rk_live` keys, SendGrid keys, lowercase `bearer`, and generated-looking values of `password=`, `AWS_SECRET_ACCESS_KEY=`, and `api_key:`. `remember` also rejects secrets in `why` and `symbols`.
- `update` never downgrades unless `--version` pins a tag.
- Remote push: a partial accept re-queues the remainder instead of dropping it.

## 0.1.22 — 2026-08-31

- **Decisions need something concrete to store.** A decision must name a path, a code span, a code-like token, a proper noun, or have a "use X, not Y" / "X instead of Y" shape. "I'll stick with JWT next" stores; "I'll stick with keep." does not. A path in the neighboring sentence counts, and is attached to the record.
- Arrow diagrams and box-drawing blocks are skipped unless they carry a path or a code span. Findings written with arrows still store.
- Read-time filters apply only to automatically extracted records. `remember` and imported records always pack.
- **The bench covers other stacks:** a React web shop and a Python API, with planted noise that must not extract, and a recall floor of 0.95. `lossless bench` had been refusing its own fixtures (6 of 19 cases); it now runs 19 of 19.
- `inspect --prune` retires legacy noise records.

## 0.1.21 — 2026-08-31

- **Warnings need a code-like shared identifier** (camelCase, digits, separators, or a known alias such as `jwt` for `jsonwebtoken`) or a pointed one-word question ("why not jsonwebtoken"). A single shared plain word no longer warns.
- `remember` rejects text containing a secret before writing anything. Message-based catch-up redacts its staging file and deletes it after ingest.
- **Without a token, the daemon rejects foreign `Host` and `Origin` headers** (DNS rebinding, cross-origin POSTs). Loopback clients and token-authenticated remotes are unchanged.
- `inspect --ask` no longer records ask actions, so inspecting does not change the next pack.
- Fixes: `limit_tokens` is respected when trimming failures; the remote push cursor advances only after a durable enqueue; a lost write race leaves no orphan index entries; missing excerpt lookups no longer create empty monthly files; `make lossless` stamps the latest tag.

## 0.1.20 — 2026-08-27

- Filters: more planning narration ("I'll call …", "Let me check …"), remarks about failures that were already fixed or superseded, and fragments clipped mid-word.

## 0.1.19 — 2026-08-24

- The watcher skips Claude subagent transcript dumps (`subagents/`, `agent-*.jsonl`).
- Filters: reviewer prompt boilerplate (READ-ONLY, APPROVE / REQUEST_CHANGES, severity templates) and echoes of lossless's own output.
- In agent-loop output, prose after a findings block still extracts.
- `ask` and extraction ignore `tmp/` paths and a root `qa-report.md`. The skill says to omit `session_id` when the prompt does not show one.

## 0.1.18 — 2026-08-24

- Agent-loop findings (`findings[].issue` with a `severity`) are stored as failures. Stray JSON fragments are skipped.
- Filters: more echoes of lossless's own output, more planning phrases, and remarks that a failure is unrelated.
- `ask` ignores `/tmp` paths, and `session_id=default` counts as missing. The watcher catches up nested Claude transcripts when their working directory is known. The skill tells subagents to pass their own session id.

## 0.1.17 — 2026-08-24

- Filters: more planning verbs, matched as whole words only.
- Pi compaction: the watcher writes the compact checkout when a new chunk contains Pi's compaction line.

## 0.1.16 — 2026-08-24

- **After a compact, `~/.lossless/active/<owner__repo>.md` holds a ready-made ask** built from the tape (the last user line and its paths) plus citations. It is written before compaction, never injected.
- An ask without `session_id` catches up this project's sessions that are behind, current workspace first, within a budget.

## 0.1.15 — 2026-08-23

- Skill and rule: pass `session_id` when the harness has one; `workspace_root` is the git checkout. Use `get_record` to open a cited excerpt when a one-line record is not enough to act on.

## 0.1.14 — 2026-08-23

- **Project keys work without `PATH`** (as in user services): git is found at a known location. `doctor` flags a checkout that has an origin but still gets a path key. The user service sets `PATH`.
- File names such as `LoginView.swift` count as paths.
- A local debug log at `~/.lossless/debug/events.jsonl` (never uploaded); `inspect` shows its last lines.

## 0.1.13 — 2026-08-20

- **Records cite their source turn.** `get_record` opens the excerpt around it, and `ask` returns `source` and `has_excerpt` instead of excerpt text. `remember` records can be cited too, and `inspect` shows record ids and whether an excerpt exists.

## 0.1.12 — 2026-08-20

- Removed a set of over-broad phrase filters from 0.1.8 and 0.1.9 that could drop real records. Shape-based filters stay.

## 0.1.11 — 2026-08-19

- Removed phrase filters that were dropping real failures and decisions.
- "Bench" counts as an identifier again, so a failure that names it still stores.

## 0.1.10 — 2026-08-19

- Concurrent catch-ups retry when SQLite is busy, so ingesting several sessions at once no longer fails.

## 0.1.9 — 2026-08-19

- Filters for recap and status noise.
- "Bench" at the start of a sentence is not treated as an identifier.

## 0.1.8 — 2026-08-19

- Filters for recap noise, clipped slogans, and unclosed-parenthesis fragments.

## 0.1.7 — 2026-08-19

- Filters for truncated recaps, `inspect` status dumps, and review lists. Real failures in the same shapes still store.
- `inspect --prune --project` limits the cleanup to one project.

## 0.1.6 — 2026-08-19

- The watcher reads a Claude session's working directory from the transcript instead of guessing it from the folder name.
- The watcher reads OpenCode's `opencode.db` directly, so a session the plugin missed is still captured.
- Codex desktop threads without a rollout file are captured from their first message when the working directory is known.
- Claude and Grok install a write-only `UserPromptSubmit` hook: it records, never retrieves or injects.

## 0.1.5 — 2026-08-19

- After a compact, lossless writes `~/.lossless/active/<owner__repo>.md` from a real ask. The skill and rule say to read it, or call ask, when the turn has not asked yet. Nothing is injected.

## 0.1.4 — 2026-08-18

- "Failed" at the start of a sentence is not a proper noun, so "Failed work first…" is not stored. "Failed to …" still stores.

## 0.1.3 — 2026-08-18

- README and roadmap phrasing is no longer stored as failures. Real failures and decisions with paths still store.

## 0.1.2 — 2026-08-18

- "I'll ask" is planning, not a decision.
- `ask` catch-up works from the store: without `session_id`, it catches up this workspace's sessions that are behind. An unknown `session_id` is only looked up exactly, never guessed.

## 0.1.1 — 2026-08-18

- `ask` catches up the asking session when the harness file is ahead of the tape.
- Extraction keeps everyday coding phrasing ("use X, not Y", "prefer X over Y", "stick with", "tests don't pass").
- Advice about failures, unclosed `**` markup, and README copy are no longer stored as failures.
- Failure overlap needs real shared symbols, not any shared word.
- `doctor` and `inspect` show the last ask next to the tape.

## 0.1.0 — 2026-08-17

First public release. A local work log for coding agents: keep the tape, check out five records.

### Product

- Catch-up copies harness session files into `~/.lossless/raw`, which is kept. Records are a derived index. `ask` returns at most five records plus warnings.
- No LLM on retrieval and no hosted embeddings. Age never disqualifies a record.
- The default install stays on this machine: `127.0.0.1:7432`, no token, nothing uploaded.
- A remote home is documented, not automatic.

### Harnesses

- `lossless setup` writes hooks, MCP config, a skill, and a short always-on rule for Grok, Claude Code, Codex, Pi, and OpenCode.
- `lossless doctor` checks the daemon, hooks, MCP, skills, rules, and the user service (launchd or systemd --user).
- Compact hooks wait for the raw copy. Turn hooks never block. Packs are not injected automatically.

### Visibility

- `lossless inspect` shows the tape, the records, and the last packs. `--ask` explains a live retrieval, `--jsonl` reads a session file, and `--prune` drops test ingest.

### Install and update

- Channel: [GitHub Releases](https://github.com/jbgh/lossless/releases).
- `scripts/install.sh` and `lossless update` download `lossless-<os>-<arch>`, verify `SHA256SUMS`, refuse non-HTTPS or off-host redirects, and replace `~/.local/bin/lossless` (a symlink there is replaced, not followed).
- `update` then retargets hooks and the user service. `doctor` does not phone home.

### Platforms

- `darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`
