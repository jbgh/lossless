# Instance-audit remediation — design (2026-09-25)

Audit of the local instance (`~/.lossless`, daemon 0.1.32) found six issues. This
spec fixes all six. Decisions were made with the owner on 2026-09-25. Revised the same
day after a review against the code and the live store; the verification log at
the end records what was checked and what changed. Re-reviewed 2026-09-26
against the code and the live store (every code:line claim re-verified; PR-A's
tally drop gained a seen-before guard).

## Decisions (binding)

1. **Sequencing: ops-first, then code PRs.** Phase 0 (backup + housekeeping) runs
   before any PR merges.
2. **Retention: compress now, delete never.** The product is called lossless; no
   raw tape is ever deleted. zstd is lossless compression; originals are removed
   only after a verified decompression round-trip. Tiered offload to R2 is
   explicitly deferred and out of scope.
3. **Stale constraints: `supersede` CLI + warning decay** (both; the CLI is the
   primitive, decay is the blast-radius guard).
4. **Backup target: new `lossless-backup` R2 bucket**, S3 credentials from a
   bucket-scoped R2 API token. Existing memory constraints still bind: binary
   swaps use `launchctl bootout` + `bootstrap`, never `kickstart`; the two live
   bugs stay separate PRs (decision `2026092121252206f6f123f7bdbffa`); the
   read-time `statusFailed` gate is unchanged; `session_id` plumbing is untouched
   in every PR.

## Program shape

| Phase | Workstream | Type | Branch |
| --- | --- | --- | --- |
| 0 | Housekeeping + R2 backup + restore drill | ops | — |
| 1 | PR-A: false `failed`-typing | code | `fix-false-failed-typing` |
| 2 | PR-B: stale constraints | code | `fix-stale-constraint-warnings` |
| 3 | PR-C: unsealed tapes | code | `seal-orphaned-raw-tapes` |
| 4 | PR-D (conditional): path attachment | code | only if metrics justify |

Each PR is its own version bump with its own CHANGELOG entry.

## Phase 0 — ops

**Housekeeping (all lossless, nothing hard-deleted immediately).** Move to
`~/.lossless/trash/2026-09-25/`; hard-delete after 7 days (a manual step — no
code owns it):

- 15 leftover `bench-*` / `TestBench*` dirs (~35 MB, reproducible fixtures)
- 12 zero-byte `excerpts-*.sqlite` placeholders, 2025-08 through 2026-07 (all
  past months, so none is open; pre-0.1.21 bug leftovers)
- the zero-byte `~/.lossless/lossless.db` stub (no code references it)

The `TestBench*` dirs will come back: `lossless bench` creates
`TestBench*000` inside `-home` (default the live `~/.lossless`) and never
removes it (`cmd/lossless/main.go:391`). PR-A carries the one-line fix (remove
the dir after the run, or create it under the OS temp dir).

The `sessions` rows whose source transcripts are gone (~2,460 of 2,570) are
**kept**: they are the session-id → tape mapping.

**R2 backup.** `backup init` writes `backup.env` from scratch (tmp + rename) and
copies credentials into it only from the shell environment, so credentials are
exported first, then init runs.

1. `npx wrangler r2 bucket create lossless-backup`
2. The owner creates an R2 API token scoped to that bucket (Object Read & Write) in the
   Cloudflare dashboard and provides the S3 access key + secret.
3. In the shell: `export LOSSLESS_BACKUP_ACCESS_KEY=… LOSSLESS_BACKUP_SECRET_KEY=…`.
4. `lossless backup init s3://lossless-backup/<prefix> -endpoint https://<account-id>.r2.cloudflarestorage.com -every 1h -keep 5`
   — writes `backup.env` (with the exported credentials) and a fresh `backup.key`.
5. **Copy `backup.key` off this machine** (password manager or another device).
   Every object is encrypted with it; restore is impossible without it, so a
   backup whose key lives only on the machine it protects is not a backup.
6. `lossless backup --dry-run`, then the first real backup.
7. **Restore drill:** `lossless restore --list`, then restore the latest
   generation into a scratch home (copy `backup.env` and `backup.key` into it,
   then `lossless restore -home <scratch>`) and diff
   `raw/` and `export/` against the live home. This is the proof the backup
   works on real data.
8. Run the never-run live test. It reads only the process environment, not
   `backup.env`, and writes under `<url>/<generation>`; point it at a test
   prefix:
   `LOSSLESS_BACKUP_LIVE_TEST=1 LOSSLESS_BACKUP_URL=s3://lossless-backup/livetest LOSSLESS_BACKUP_ENDPOINT=… LOSSLESS_BACKUP_ACCESS_KEY=… LOSSLESS_BACKUP_SECRET_KEY=… go test ./internal/backup/ -run TestLiveR2 -v`
9. Confirm `lossless doctor` shows the `backup` check healthy; the
   `serve --watch` scheduler owns the hourly run afterwards.

The first generation uploads `raw/` as-is (2.1 GB, of which 1.74 GB is already
zstd) — protection before elegance. PR-C re-uploads ~455 MB of plain tapes as
~50–60 MB of `.zst` once, then steady state.

## PR-A — false `failed`-typing

**Problem.** Verification and status reports are typed `failed`. Of the 77 active
`failed` records created since 0.1.32 went live (2026-09-23T00:47Z), roughly 40–49
are status-report shaped, and ~25 of those are one flaky test (a worktree-only
font check in `export.spec.ts`; 27 counted on 09-25, 24 on 09-26)
re-reported on every gate run. They pollute packs
and feed recurrence warnings (recurrence reads the store directly). Known bug
`20260819165849f964c3efedb8a7e6` ("… CI is pending (not failed) on all open
PRs"), slated as a separate follow-up PR.

**Observed shapes (from the live store, not guessed).** Ranked by frequency:

- test tallies that include a failure count: "vitest 711 passed/16 skipped/1
  failed (…)", "playwright 26 passed, 1 failed", "Gate summary: tsc clean; …"
- known / pre-existing / flaky failures named as such: "the failure is the known
  worktree-only … test", "the documented load-flake", "passes in isolation",
  "ignored per common rules", "unrelated / outside my files"
- aggregate pass reports: "5/5 consecutive full `npm test` runs pass",
  "everything else passes", "clean before/after", "all green"
- non-failure statuses: "pending (not failed)", "no blocked tickets"

The last two groups alone (the original draft's list) match only 8 of the 77;
the first two carry the bulk.

**Fix (capture-time only).**

- A **new, separate predicate** (e.g. `gate.StatusReport`) for these shapes,
  called only from the extract path (`internal/write/extract.go`,
  `extract_classify.go`). It is **not** added to the `statusFailed` list and not
  folded into `write.NotAFailure`: both are also evaluated at read time
  (`internal/retrieve/query.go:416`), so extending either would change the
  read-time gate, which the binding decision forbids.
- Matching sentences are dropped (not stored), with one guard applied to
  **every** shape rather than only to tallies (implementation deviation,
  2026-09-27): a sentence is dropped only when it names nothing, or when every
  test name it mentions has failed before (a store lookup in the recurrence
  spirit; `RecordsForRecurrence` (`internal/store/recurrence.go:26`) and
  `recurrenceClusters` (`internal/retrieve/recurrence.go:59`) already cluster
  failing identifiers). So a brand-new failure whose only report is a tally
  line still extracts as `failed`, and the design's rationale holds: a known
  flake is captured by its first real report, which is exactly the one the
  guard keeps. Widening the guard to all shapes was forced by an adversarial
  review of the predicate: no lexical rule distinguishes "the failure is the
  known worktree-only bundled-font check" (a re-report, drop) from "the known
  flaky suite aside, `AuthService.refresh` failed with a nil pointer" (a
  first-time failure, keep), so the store is the only reliable discriminator.
  Sentences that name nothing (aggregate pass reports, "pending (not failed)")
  still drop unconditionally.
- Carried from Phase 0: the `lossless bench` temp-dir fix (remove the
  `TestBench*000` dir after the run, or create it under the OS temp dir).
- Storing some shapes as `state` is deferred: it needs its own rule for which
  ones, and dropping is enough to stop the pollution.
- Negatives that must still extract as `failed`: a real failure that mentions a
  pass elsewhere ("three tests fail, and the commit went through anyway — the
  pipe hid the failure"), a root-caused recurring failure ("the recurring
  tool-call failure is the image-read cap"), a first-time failure with a cause,
  and a test tally naming a first-time failing test.

**Retroactive sweep.** `inspect --prune` gains a status-report-`failed` rule
using the same predicate that **supersedes** (never deletes) the existing
misfiled records; run it against the live store after the fix ships and record
the count it touched.

**Tests.** Bench fixtures distilled from the post-0.1.32 misfires (redacted),
plus the negatives above; extraction determinism holds since 0.1.28 so
single-run comparisons are valid. Also: a regression check that `lossless
bench` leaves nothing behind in `-home`.

## PR-B — stale constraints

**Problem.** Incident-shaped constraints with time anchors keep warning long
after the incident. Known bug `2026091304482500000000000000aa` (type
`constraint`, active): "Since 2026-09-13T04:29:03Z api-prod's Tailscale node
key is EXPIRED …". Slated as a separate follow-up PR.

**Fix.**

1. **Investigate first.** Reproduce the exact warning path for
   `2026091304482500000000000000aa`; confirm the incident is actually resolved;
   measure typing (incident state filed as `constraint`) vs. staleness. The
   decay rule below — including which anchor words count and the window — is a
   starting hypothesis the evidence can change.
2. **`lossless supersede <id> [reason]`.** A thin CLI over the existing
   `Store.Supersede` (`internal/store/prune.go`): sets `status=superseded`; row
   and text survive; works offline against the store. `records` has no reason
   column; the PR picks where the reason lives (a linked `decision` record
   preferred over a schema change) and says so in the docs.
3. **Warning decay.** Anchors differ in meaning, so they decay differently.
   The two rules are **independent sufficient conditions** (implementation
   deviation, 2026-09-27): a future end anchor is not a veto, so the
   start-anchor rule still applies to text that carries both. The live repro
   is exactly that case — `Since 2026-09-13T04:29:03Z … EXPIRED … (eu expires
   2026-10-06T22:28:49Z)` — where the trailing date describes a *different*
   host that the closing state record also resolved, so reading the end anchor
   as governing would keep the warning until 2026-10-07 and make the spec's own
   acceptance criterion unmeetable.
   - end anchors ("until <date>", "expires <date>"): stop warning once the date
     passes.
   - start anchors on incident-shaped text ("since <date>" / "as of <date>"
     together with incident vocabulary: expired, down, broken, blocked, not
     deployed, outage): stop warning after a tunable window (**14 days
     default**; `--stale-window` / env override).
   - "since <date>" on a standing rule ("since 2026-01, CI requires Go 1.23")
     never decays.
   Decayed constraints still pack, with their visible date. Constraints without
   anchors are unaffected.
4. **Re-confirmation.** A newer active constraint re-enables warnings only by a
   deterministic match: same `claim_hash`, or the same rare code-shaped
   identifier set used by recurrence detection — reuse
   `Store.RecordsForRecurrence` (`internal/store/recurrence.go:26`) and
   `recurrenceClusters` (`internal/retrieve/recurrence.go:59`) rather than a
   second identifier extractor, so the two features cannot drift apart.
   Anything fuzzier is the auto-supersede-on-contradiction problem, which is out
   of scope.
5. **`inspect --prune` lists stale candidates**: decayed, not superseded — a
   prompt to run `supersede`.

**Tests.** Tailscale-shaped bench fixtures: decay suppresses the warning;
`supersede` stops packing; deterministic re-confirmation re-enables; undated
constraints and standing-rule "since" constraints never decay; an "until" date
in the future still warns.

## PR-C — unsealed tapes (delete never)

**Problem.** `raw/` is 2.1 GB, but 1.74 GB of it is already sealed `.jsonl.zst`.
Only 456 tapes (455 MB) are plain: 447 in 2026-08, 9 in 2026-09. The original
estimate of 300–500 MB after compaction was wrong; sealing the plain tapes saves
~400 MB, leaving `raw/` at roughly **1.7–1.8 GB**. Recompressing existing `.zst`
at a higher level is not planned.

**Root causes (from the code).**

- 432 of the 447 plain August tapes are Claude Code subagent dumps
  (`agent-*.jsonl`, all mtime 08-24), ingested before `claudeSubagentDump`
  (`internal/watch/watch.go`) excluded them from watch targets. Idle-seal runs
  only for watch targets, so nothing ever revisits these tapes.
- `idleSeal` resolves the tape path with `time.Now()`
  (`internal/watch/watch.go:276`), so a session's tape in an earlier month's
  folder is never found once the month rolls over.
- Tapes whose source transcript is gone have no target either.

**Fix.**

1. **One sealing path.** Extend the existing `write.SealRaw`
   (`internal/write/seal.go`) instead of writing a second compactor; idle-seal
   gets the same guarantees:
   - verify: decompress the `.zst.tmp`, compare SHA-256 with the plaintext,
     then rename, fsync the directory, and only then unlink the original.
   - lock: take the same `flock` the append path takes (`internal/write/append.go:90`)
     for the whole copy-verify-unlink, and make the append path re-check after
     locking that the file it opened is still linked (else reopen via
     `LiveRawPath`). Without both, a line appended mid-seal, or by a writer
     that opened the file before the unlink, is lost.
   - no clobber: refuse when a `.zst` sibling already exists (today `os.Rename`
     would overwrite it). `manual/<month>/remember.jsonl` has no `.partN`
     rollover, so sealing the current month's file and remembering again would
     otherwise overwrite the earlier `.zst` on the next seal. None of these pairs
     exist today.
2. **Sweep for orphans.** A daemon-side pass (so it shares the process with the
   watcher and appends) walks `raw/` for plain tapes idle ≥ the idle-seal window
   (24h) and seals them through `SealRaw`. Skips the current month's
   `manual/remember.jsonl`. Exposed as `lossless inspect --seal-raw` — a dry
   run only (implementation deviation, 2026-09-27: the CLI flag reports, the
   daemon applies; the sweep function itself takes an apply flag that the
   daemon passes true). Reports tapes and bytes saved. Nothing outside this
   path is ever deleted.
3. **Fix `idleSeal`** to seal the tape the session actually wrote (from the
   catch-up's `RawPath` or the session's month), not the current-month path.
4. Docs + CHANGELOG.

**Backup interaction.** Sealed files are new objects; the next backup generation
uploads them once (~50–60 MB) and drops the plain ones after `-keep` generations
roll. Content addressing and restore paths are unchanged.

## PR-D — path attachment (conditional)

After PR-A and its sweep, re-measure the pathless share of active `failed`
records (921/1,167 on 2026-09-25). If still far above 50%, design a capture-side
context-attach pass; if the typing fix collapses the junk, close with data. No
code before the measurement.

## Success criteria

- `lossless doctor` green including a scheduled backup; `backup.key` exists off
  this machine; a restore of the latest generation into a scratch home matches
  the live `raw/` and `export/`; `LOSSLESS_BACKUP_LIVE_TEST=1` has passed once
  against `lossless-backup`.
- Zero new status-report `failed` records over one week of use; the legacy
  misfires are superseded by the prune rule, with the count recorded.
- The Tailscale-shaped stale-constraint repro no longer warns in bench;
  `supersede` is documented and used by `inspect --prune`'s stale listing.
- No plain tape idle more than 24h outside the current month's
  `manual/remember.jsonl`; `raw/` at roughly 1.7–1.8 GB; every seal
  round-trip-verified; no tape ever deleted.
- `lossless bench` leaves nothing behind in the live home.
- Every change traceable to a shipped PR with tests; rollouts use
  `bootout`+`bootstrap` and confirm `/health`.

## Local growth and the offload trigger

"Delete never" plus R2-as-backup means the local store only grows. Measured on
2026-09-25:

| | Size |
| --- | --- |
| `raw/` total | 2.1 GB |
| 2026-08 | 2.0 GB, almost all a one-time import of existing history (1,087 OpenCode sessions, 432 subagent dumps) |
| 2026-09 (25 days) | 206 MB |
| `index/` + `export/` | ~150 MB (2026-08 excerpts 114 MB; 2026-09 9 MB) |

Steady state is ~250 MB/month (~3 GB/year) at current usage; it scales with
agent volume. PR-C recovers ~400 MB once; a higher zstd level or a trained
dictionary may shrink `raw/` further (unmeasured).

Retrieval does not read tapes: `ask`, `get_record`, and packs read the index and
excerpts. Tapes are read only for re-extraction, replay, and `inspect`. So
cold tapes could live in R2 without changing retrieval, which is why offload is
the eventual answer to growth.

**Order.** Keep everything local for now. Offload is revisited when **either**
`~/.lossless` exceeds 10 GB **or** free disk drops below 20 GB (both tunable at
that time). Its **precondition** is an archive-grade bucket, because an
offloaded tape's only copy is in R2:

- tape objects are never deleted when a generation rolls off, except a version
  proven to be a prefix of its successor (append-only growth) or a plain tape
  whose `.zst` decompresses to the same bytes; a rewritten, truncated, or
  missing tape keeps its last good copy forever
- a bucket-side delete lock (R2 bucket lock rules) on the object prefix, so
  neither a bug nor a leaked token can erase history
- `backup.key` held in at least two places off this machine, and a restore
  drill that uses one of those copies

Today's `-keep 5` hourly mirror does not meet this: a tape lost or corrupted
locally leaves the bucket about five hours later. The archive-grade changes are
not yet scheduled as a PR.

## Out of scope

- Tiered raw offload to R2 (trigger and precondition above).
- Recompressing already-sealed `.zst` at a higher level.
- Auto-supersede on contradiction (non-deterministic by design).
- Storing status reports as `state`.
- Retrieval/pack changes beyond warning decay (the 0.1.30 rescue routes stand;
  the read-time `statusFailed` gate is untouched).
- `sessions` row GC.
- Work in the affected downstream project.

## Verification log (2026-09-25 and 2026-09-26 reviews)

Claims in the first draft, checked against the code and the live store:

| Claim | Result |
| --- | --- |
| 15 `bench-*`/`TestBench*` dirs, ~35 MB | ✓ 15, 35 MB; source found (`lossless bench` writes into the live home) |
| 12 zero-byte `excerpts-*.sqlite` | ✓ 2025-08 … 2026-07, all past months |
| zero-byte `lossless.db` stub | ✓ 0 bytes; no code references it |
| 2,468 `sessions` rows with gone sources | ≈ 2,462 of 2,570 now (live drift) |
| backup env names, `init -endpoint -every -keep`, live-test gate | ✓ names correct; draft missed the required bucket URL argument, the init-overwrites-`backup.env` ordering, the live test's env-only config, and `backup.key` |
| `lossless doctor` checks backup | ✓ `backup.DoctorCheck` |
| 41 of 76 post-0.1.32 `failed` are status reports | ≈ 40–49 of 77 by pattern count (one more created since); plausible |
| 117 status-report `failed` overall | not reproducible exactly; a loose pattern count gives ~93 |
| the draft's predicate list covers the misfires | ✗ matches 8 of 77; test tallies and known-flake reports dominate |
| new predicates can go in `internal/gate` without touching read time | ✗ `statusFailed` and `NotAFailure` are both read at query time |
| bug ids `20260819165849…`, `202609130448…` | prefixes match 3 and 1 records; full ids now given |
| raw/ 2.1 GB, 1.9 GB from August | ✓ 2.1 GB; August ≈ 2.0 GB |
| compaction brings raw/ to 300–500 MB | ✗ 1.74 GB is already zstd; ~1.7–1.8 GB after |
| "audit the seal path" | `SealRaw` + idle-seal already exist; root causes named above |
| pathless share 919/1,160 | ≈ 921/1,167 now |
| decision `2026092121252206f6f123f7bdbffa` | ✓ active; this spec honours it |
| 0.1.30 rescue routes, determinism since 0.1.28 | ✓ per CHANGELOG |
| 2026-09-26 re-review: code:line claims (query.go read-time gate, watch.go:276 `time.Now()`, append.go flock, `SealRaw` verify/lock/clobber gaps, main.go:391 bench leak, `manual/remember.jsonl` no `.partN`), backup CLI surface, store counts | ✓ all held (live drift: 82 vs 77 faileds, 24 vs 27 `export.spec.ts`, 444 vs 455 MB plain, 2,576 vs ~2,570 sessions); PR-A's tally drop gated on seen-before after review |

## Post-implementation review (2026-09-27)

A review of the implementation against a copy of the live store changed:

- **Seen-before is time-ordered.** The prune guard counted every other
  record, so repeat reports found each other and all were superseded, the
  first real report included. It now counts only records created earlier
  (id breaks ties), matches names on word boundaries, ignores non-names
  (`e.g`, short words, `NAME=value`), and counts only test-shaped names when
  a sentence has any.
- **Predicate narrowed** to what the live false positives showed: "unrelated"
  only as "unrelated to …" or next to a test/failure noun; "a real,
  documented failure" is not a label; quoted labels are mentions; a pass
  beside a negation is a finding; a stated cause (bisected to, caused by, "the
  failure is a … drift/setup issue") keeps the sentence; a failing count that
  names nothing and carries no label stores. On the live store the prune
  supersedes 37 records (the first version: 53, about 8 of them wrong).
- **Every raw writer re-checks the link after locking** (catch-up and
  `remember` as well as append), through one `openLiveLocked`.
- **Decay covers the recurrence route**: a decayed constraint cannot anchor a
  recurrence cluster. Re-confirmation needs two shared identifiers (or the
  only one). The latest end anchor decides. The stale listing covers every
  project when `--project` is omitted.
- One release (0.1.33) instead of three version bumps.
