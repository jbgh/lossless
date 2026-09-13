# Design: backup to object storage

Date: 2026-09-12
Status: PROPOSED
Amends: `docs/deploy.md` (new section; out-of-scope line), `docs/roadmap.md` (operator row; "no S3" line)
API: unchanged. No new REST or MCP surface. Three CLI entry points (`backup init`, `backup`, `restore`), one env-driven ticker in `serve --watch`, one `doctor` line.

## Problem

The store is files on one machine. `raw/` is the tape and is meant to live
forever. `export/` is the claim HEAD. `index/` is derived. If the disk dies,
every harness on that machine loses its memory at once, and the only
documented recovery is that you copied `raw/` and `export/` yourself.

lossless keeps running locally. Nothing about `ask`, hooks, catch-up, or the
loopback default changes. This adds an opt-in, encrypted, incremental copy of
the store to an S3-compatible bucket you own, and a restore that brings it back
onto a fresh machine.

## Premises

1. Local stays the product. `ask` reads local files. A bucket is a copy,
   never a home. This is not "S3 as the raw store" and does not touch the
   remote-home push path.
2. Opt-in. `setup` does not enable it. Default install still uploads nothing.
   The bucket, credentials, and IAM are yours.
3. Bytes are encrypted before they leave the machine, with a key you hold.
   The bucket exposes no paths, project names, or session ids.
4. Incremental forever. Sealed tape parts upload once. A run after the first
   touches live parts, changed claims, and index snapshots.
5. Crash-safe. The manifest is written last. Until it lands, the previous
   generation is fully restorable.
6. Hooks never wait on the network. Backup takes a shared lock on a live part
   only long enough to copy it to a temp file.
7. No new modules. SigV4, HKDF, AES-GCM, and HMAC are all Go stdlib.
8. History is lossless's own. The bucket keeps the last N generations and the
   objects they reference. No bucket feature is assumed: Cloudflare R2 does
   not implement bucket versioning, and the layout must work the same on
   AWS, R2, B2, and MinIO.
9. Every backend gets the same bytes. Payloads are signed, the region is
   whatever the backend wants, and no operation needs LIST.

## Surface

### Commands

```
lossless backup init s3://<bucket>/<prefix> [--endpoint URL] [--region R] [--every 1h] [--keep 5]
lossless backup [--dry-run] [--verbose]
lossless restore [--force] [--at <generation>]
lossless restore --list
```

`backup init` writes `<home>/backup.env` (0600) and generates
`<home>/backup.key` (32 random bytes, hex, 0600). It refuses to overwrite an
existing key. It prints one line telling the operator to copy `backup.key`
somewhere that is not this machine. `--every` defaults to `1h` and writes
`LOSSLESS_BACKUP_EVERY`; `--every 0` writes `0`, which disables the
schedule. `--keep` writes `LOSSLESS_BACKUP_KEEP`. Configuring a bucket means
backups are scheduled unless you say otherwise.

`backup` runs one incremental backup and prints a summary: files scanned,
uploaded, deleted, bytes sent, generation id, elapsed. Non-zero exit on
failure. `--dry-run` stops after planning and prints the plan. A run that
finds nothing changed writes nothing to the bucket and prints `no change`.

`restore` pulls a generation and every file it names into the store. Default
is the latest. `--at` names a generation from `--list`. It refuses if
`GET /health` answers on the sidecar URL. It refuses if `raw/` or `export/`
is non-empty unless `--force`. With `--force` it unions raw and export and
replaces index snapshots. A local file at a different hash is left alone and
listed at the end, never overwritten.

`restore --list` prints kept generations: id, created, lossless version,
file count, total bytes. Read-only.

`serve --watch` runs backup on a due time derived from the last success when
`LOSSLESS_BACKUP_EVERY` is a positive duration. See Scheduling.

`doctor` prints one line: target, encrypted, generations kept, age of last
successful backup, next due, and the last error if the last attempt failed.
Warns when the age is more than twice the interval or there has never been
one. With `LOSSLESS_BACKUP_EVERY=0` it warns only when no backup has ever
succeeded. Prints `backup: not configured` when
`backup.env` is absent.

All three commands honour `--home` and `LOSSLESS_HOME` like every other
command.

### Config

`<home>/backup.env`, loaded by `serve`, `backup`, and `restore` themselves.
The launchd and systemd units do not change.

| Var | Meaning |
|-----|---------|
| `LOSSLESS_BACKUP_URL` | `s3://bucket/prefix`. Prefix may be empty. |
| `LOSSLESS_BACKUP_ENDPOINT` | Optional. Custom endpoint (R2, B2, MinIO, GCS interop). Path-style addressing when set; virtual-host style for plain AWS. https required unless loopback. |
| `LOSSLESS_BACKUP_REGION` | Default `us-east-1`. A host ending in `r2.cloudflarestorage.com` signs with `auto` regardless. |
| `LOSSLESS_BACKUP_ACCESS_KEY` / `LOSSLESS_BACKUP_SECRET_KEY` | Credentials. Fallback: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`. |
| `LOSSLESS_BACKUP_EVERY` | Interval. `init` writes `1h`. `0` disables the schedule. |
| `LOSSLESS_BACKUP_KEEP` | Generations to keep. Default 5. Minimum 1. |

No profile files, no instance metadata, no SSO. Those credential chains are
the reason the SDK is heavy.

Other files in the home: `backup.key` (secret), `backup-state.json` (cache),
`backup.lock` (flock), `backup-tmp/` (swept each run).

### Cloudflare R2 specifics

Verified against R2's S3 API page and limits page on 2026-09-12.

- Endpoint `https://<account>.r2.cloudflarestorage.com`, or
  `https://<account>.<jurisdiction>.r2.cloudflarestorage.com` for a bucket
  created in a jurisdiction. Path-style works.
- Region must sign as `auto`. R2 aliases empty and `us-east-1` to `auto`,
  and the client sets `auto` itself when it sees an R2 host.
- Credentials are an R2 API token with Object Read & Write scoped to the
  bucket. Cloudflare shows the S3 Access Key ID and Secret Access Key once,
  on the token creation page. Same two env vars.
- PutObject, GetObject, HeadObject, DeleteObject, DeleteObjects, and
  ListObjectsV2 are implemented. Conditional PUT (`If-Match`,
  `If-None-Match`) is implemented.
- Bucket versioning is not implemented. Generations cover history.
- Writes to the same key are limited to one per second and answer 429
  above that. The only key written on every run is the pointer, once per run,
  and the retry backoff handles 429.
- Single PUT up to 5 GiB. Key length up to 1,024 bytes.
- Writes are strongly consistent.

The live test targets R2.

## What is copied

Only three roots are walked: `raw/`, `export/`, `index/`. Nothing else in the
home is ever read. That is the exclude rule.

| Path | How it is read | Changes |
|------|----------------|---------|
| `raw/**/*.jsonl.zst` | Direct. Immutable once sealed. | Uploads once. |
| `raw/**/*.jsonl` | Copy to `backup-tmp/` under `flock(LOCK_SH)`, then release. | Re-uploads as it grows until sealed. |
| `export/**/*.md` | Direct. Writer is tmp-and-rename, so reads are atomic. | On new or superseded claim. Prune deletes. |
| `index/claims.sqlite` | `VACUUM INTO '<backup-tmp>/claims.sqlite'` on a read connection. | Every run it changed. |
| `index/excerpts-*.sqlite` | Same. Zero-byte files skipped. | Current month changes; past months are static. |

Skipped: `*.lock`, `*.tmp`, `*-wal`, `*-shm`, zero-byte sqlite, symlinks.
`raw/manual/` is inside `raw/` and is included.

Snapshots upload under the original relative path (`index/claims.sqlite`) so
restore places them as-is. `VACUUM INTO` yields a consistent single file while
the WAL stays live and forces no checkpoint on the running daemon.

If a live part seals between walk and open, the reader takes the `.zst`
sibling, the same fallback `write.ReadRaw` uses.

## Change detection

`backup-state.json` records per relative path: size, mtime (ns), sha256,
object key. Matching size and mtime skips the read. It also records
`last_ok`, `last_attempt`, `last_error`, the last generation id this machine
wrote, and the kept manifests it last saw so a run does not re-fetch them.

The remote manifest is the source of truth. A missing or stale cache means
one full re-hash and an upload of only what differs from the manifest. The
cache is never authoritative.

## Bucket layout

```
<prefix>/manifest              pointer: copy of the latest generation, written last
<prefix>/m/<generation>        one full manifest per kept generation
<prefix>/o/<name>/<sha256>     one object per file version
```

- `generation` = UTC timestamp `20260912T201500Z` plus a 4-hex random
  suffix, so two machines restoring and re-backing-up never collide.
- `name` = hex(HMAC-SHA256(name_key, relpath))[:32]. Paths never appear in
  the bucket.
- `sha256` = hash of plaintext content. An object is shared by every
  generation that references it. It is deleted only when no kept generation
  references it.
- No LIST. Every operation is by known key.

### Manifest (plaintext, before encryption)

```json
{
  "version": 1,
  "generation": "20260912T201500Z-a1f3",
  "created_at": "2026-09-12T20:15:00Z",
  "lossless": "0.1.26",
  "client": "<install id>",
  "generations": ["20260912T201500Z-a1f3", "20260912T191400Z-77c0"],
  "dropping": [],
  "files": {
    "raw/jbgh__lossless/2026-09/2e49….part2.jsonl.zst": {
      "sha256": "…", "size": 442931, "object": "o/<name>/<sha256>"
    }
  }
}
```

Keys in `files` are relative to the home with forward slashes on every OS.
`generations` lists kept generations newest first, including this one.
`dropping` lists generations whose removal was in progress when the pointer
was written; the next run finishes them. Both `m/<generation>` and the
pointer hold the same document. Every manifest is encrypted with relpath
`manifest` as AAD. The pointer is overwritten in place; S3 PUT is atomic per
key.

## Encryption

`backup.key`: 32 random bytes, hex. HKDF-SHA256 (Go 1.24 `crypto/hkdf`)
derives two 32-byte keys with fixed info strings: `lossless-backup-content`
and `lossless-backup-name`.

Object format:

```
magic   "LSBK" + uint8 version (1)
prefix  8 random bytes
chunks  for i in 0..n-1:
          nonce = prefix || uint32be(i)
          ct    = AES-256-GCM(content_key, nonce, plaintext_chunk_i, aad)
          aad   = relpath || 0x00 || uint32be(i) || last_flag
```

- Plaintext chunk size 1 MiB. The last chunk may be shorter, including zero
  bytes for an empty file (one chunk, `last_flag` = 1).
- `last_flag` is 1 on the final chunk. A truncated object fails because the
  final chunk read has `last_flag` = 0. A reordered object fails the counter.
  A swapped object fails the relpath. A flipped byte fails the tag.
- Ciphertext length = 13 + n × 16 + plaintext length.

Upload path: encrypt to `backup-tmp/<name>.<sha256>` while hashing the
ciphertext in the same pass, then PUT that file with
`x-amz-content-sha256` set to the ciphertext hash and a known
`Content-Length`. Signed payloads work on every S3-compatible backend; R2's
docs do not promise unsigned ones. A retry re-reads the temp file and does
not re-encrypt. Peak temp disk is four in-flight objects, so at most a few
hundred MB.

Restore decrypts streaming to `<target>.restore-tmp`, verifies the plaintext
sha256 against the manifest, then renames.

No plaintext mode in v1. A later path-named plaintext layout can be added
without touching this one.

## Backup run

Single-flight under `flock` on `<home>/backup.lock`. Daemon and CLI never
overlap. A run that cannot take the lock exits with "backup already running".

1. Load `backup.env` and `backup.key`. Fail fast on missing or malformed
   config before any network call.
2. GET pointer. 404 → empty manifest (first run). Decrypt. Wrong key →
   "backup.key does not match this bucket".
3. Sweep `backup-tmp/`. Walk the three roots. Build the desired set
   `relpath → (sha256, size)` using the cache. Copy live parts and take
   sqlite snapshots into `backup-tmp/` here.
4. Plan.
   - upload = paths whose `(relpath, sha256)` the pointer manifest does not
     hold.
   - If upload is empty and no path was removed, stop: stamp `last_ok`,
     print `no change`, write nothing to the bucket.
   - drop = generations in `dropping`, plus the oldest kept generations
     beyond `KEEP − 1` once this run is added.
5. Encrypt and upload planned objects, 4 in flight. Retry 3× with backoff
   (1s, 4s, 16s) on network error, 429, 5xx. No retry on 400, 403,
   404-on-PUT; fail with the status and the S3 error code.
6. PUT `m/<generation>`, then PUT the pointer with `generations` = kept list
   and `dropping` = drop. Commit point is the pointer.
7. For each generation in drop: fetch its manifest (cache or `m/`), delete
   every object it references that no kept generation references, then
   delete `m/<generation>`. 4 in flight. Failures are logged, not fatal:
   the pointer still lists it under `dropping`, and the next run's step 4
   picks it up. A `m/<generation>` that is already 404 is treated as done.
8. Write cache with `last_ok`, this generation id, and the kept manifests.
   Sweep `backup-tmp/`. Print summary.

`--dry-run` runs steps 1–4 and prints the plan, including which generation
would be dropped.

An object uploaded by a run that crashed before step 6 is referenced by no
manifest. The next run re-PUTs it under the same key, which makes it
referenced again. The only leak is a version that changed again before the
next run; a later `--prune` with LIST can sweep those.

## Restore run

Takes the same lock.

1. Refuse if the sidecar `/health` answers. Refuse on non-empty `raw/` or
   `export/` without `--force`.
2. Load config and key. GET pointer. Decrypt. With `--at`, GET
   `m/<generation>` instead; refuse a generation that is in `dropping`.
3. For each file, 4 in flight: skip if the local file exists at the manifest
   hash (resume). Under `--force`, overwrite index snapshots; for raw and
   export, skip a local file at a different hash and list it. Otherwise GET,
   decrypt to `.restore-tmp`, verify sha256, rename.
4. Write `backup-state.json` from the manifest so the next backup on this
   machine does not re-hash.
5. Print summary: generation, restored, skipped, left alone, bytes.

`--list` does steps 2 (pointer only) and prints the `generations` list with
each manifest's created time, lossless version, file count, and total size.
It fetches each kept manifest for the counts; they are small.

The restored `claims.sqlite` carries the action tape and cursors. Cursor rows
name session files from the old machine; those sessions are dead, so they are
inert. `store.Open` runs migrations as usual.

## Scheduling

The schedule lives in the daemon. No cron, no launchd timer, nothing new for
`setup` to write. The service units already run `serve --watch`.

`watch.Run` already owns the catch-up ticker and an hourly sweep ticker. When
`LOSSLESS_BACKUP_EVERY` is a positive duration, a one-minute backup ticker
joins them and asks one question each minute: is a run due?

```
due = last_ok + EVERY            from backup-state.json
if no last_ok: due = start + 2m   first ever run on this machine
if due < start + 2m: due = start + 2m
```

The two-minute floor keeps a fresh boot or an `update` restart from uploading
before the harnesses have settled. A daemon that restarts more often than the
interval still backs up, because due comes from the persisted last success,
not from process start. A laptop that sleeps past its due time backs up
within a minute of waking.

After a success, `due = now + EVERY`. After a failure, `due = now + min(15m,
EVERY)` and `last_error` is stamped, so a transient outage costs minutes, not
an interval. A run with no changes counts as a success.

The run goes in its own goroutine. A tick while the previous run holds the
lock is skipped and logged. Errors go to `serve.log` with the time and
version stamp. Catch-up and `ask` never wait on it.

History window is `KEEP` changed runs, not `KEEP` ticks: idle runs write
nothing. With hourly runs and active sessions that is roughly the last five
working hours; with daily runs, five working days. `--keep` is the knob.

## Errors

| Case | Behaviour |
|------|-----------|
| No `backup.env` | `backup` and `restore` exit 2 with "run lossless backup init". `serve` does nothing. `doctor` says not configured. |
| Missing or malformed key | Exit 2 before any network call. |
| Bad URL, http endpoint off loopback | Exit 2. Same rule as remote home. |
| 400 / 403 from the bucket | Exit 1 with status and S3 error code. No retry. |
| Network, 429, 5xx | 3 retries per object, then exit 1. Pointer not written. Previous generation restorable. |
| Crash mid-run | Next run sweeps `backup-tmp/`, finishes `dropping`, re-plans from the pointer. |
| Wrong key on restore | "backup.key does not match this bucket". Nothing written. |
| `--at` names an unknown or dropping generation | Exit 1 listing the kept ones. |
| Disk full on restore | Tmp write fails. Nothing half-renamed. Exit 1. |
| Lock held | "backup already running". Exit 1. |
| Scheduled run fails | Logged, `last_error` stamped, retried in `min(15m, EVERY)`. `doctor` shows the error until a success. |

## Packages

```
internal/backup/s3/      SigV4 signer; client: Put, Get, Head, Delete; retry; URL rules; R2 region rule
internal/backup/crypt/   HKDF keys; HMAC namer; chunked AES-GCM writer and reader
internal/backup/         config (load, init); state cache; lock; walk + snapshot; plan; generations; Run; Restore; List
cmd/lossless/backup.go   runBackup (init | run), runRestore (run | --list); help text
cmd/lossless/main.go     switch entries; doctor line
internal/watch/watch.go  backup ticker
```

`internal/backup` imports `store` (a new `store.Snapshot(src, dst)` helper opens a
read connection with the existing pragmas and runs `VACUUM INTO`) and
`write` (for `CheckRemoteURL` and `ReadRaw` fallback). `watch` imports
`backup`. No cycle: `write` and `store` do not import `backup`.

No existing behaviour changes when `backup.env` is absent.

## Tests

All outbound clients in this repo are tested against `httptest`. Backup uses
an in-process fake S3: objects in a map, checks the `Authorization` header
shape and that `x-amz-content-sha256` matches the body, supports PUT, GET,
HEAD, DELETE, a per-key failure injector, and an optional one-write-per-second
rule per key that answers 429 like R2.

- **Signer.** Published AWS SigV4 test vectors with the fixed date and the
  example keys; signature compared byte for byte. An R2 host signs with
  region `auto` whatever the config says.
- **Crypt.** Round trip. One flipped byte, swapped chunks, truncated tail,
  wrong relpath each fail. Empty file, exactly 1 MiB, 1 MiB + 1. Ciphertext
  hash from the encrypt pass equals a hash of the temp file.
- **End to end.** Temp store via `store.Open`, seeded with sealed parts, one
  live part, claims. Run 1 uploads all and writes generation 1. Run 2 writes
  nothing and prints `no change`. Append to the live part: one upload, a new
  generation, no delete yet. Prune a claim: a new generation. Restore latest
  into a fresh root, `store.Open`, `ask` returns the same records.
- **Generations.** With `KEEP=2`, a third changed run drops generation 1
  and deletes only the objects no kept generation references; the shared
  sealed parts stay. `restore --at` generation 2 restores the older live
  part content. `restore --list` shows two.
- **Dropping.** Fake fails the object deletes in step 7; the pointer lists
  the generation under `dropping`; the next run finishes it.
- **Restore guards.** Refuses non-empty without `--force`. With `--force`,
  leaves a differing local file and lists it. `--at` an unknown generation
  exits 1.
- **Commit point.** Fake fails the pointer PUT; the previous generation
  still restores; the next run re-PUTs the orphan objects.
- **429.** Fake enforces one write per second on the pointer key; a retried
  run succeeds.
- **Snapshot.** A write left in the WAL is present in the `VACUUM INTO` copy.
- **Watcher.** A slow backup does not block the tick; the next tick skips.
  A stale `last_ok` runs two minutes after start; a fresh one waits for its
  due time; a failed run is retried after the short delay; `EVERY=0` never
  runs.
- **Doctor.** Not configured, fresh, stale, last attempt failed.
- **Live.** One test against a real R2 bucket, skipped unless
  `LOSSLESS_BACKUP_LIVE_TEST` names it. Run by hand before shipping.

## Docs

- `docs/deploy.md`: new section "Back up to object storage" (init, key
  custody, what is copied, what is not, generations and `--keep`, restore
  and `--at`) with a Cloudflare R2 subsection (endpoint, jurisdiction, API
  token scope, region `auto`, no versioning so generations are the history).
  The out-of-scope line "S3 as the raw store" becomes: `ask` reads local
  files; a bucket is a copy, never a home.
- `docs/roadmap.md`: "no S3" line gets the same clarification; operator table
  gains a backup row.
- `README.md`: "Not a cloud" stays. One line under local-by-default about
  opt-in encrypted backup to a bucket you own.
- `CHANGELOG.md`: 0.1.26 entry.
- `lossless remember` the standing decisions from this spec so the next
  session's `ask` packs them.

## Out of scope

- Multi-machine sync or merge. One writer per bucket prefix.
- `--prune` with LIST for orphans left by a crash. Generations already
  bound normal growth.
- Plaintext (`--no-encrypt`) layout.
- Multipart upload. Largest object is a monthly excerpts sqlite (~100 MB);
  single PUT is good to 5 GiB on AWS and R2.
- Project filter.
- `reindex` from `export/` and `raw/`. Restore carries index snapshots, so it
  does not need one. That gap stays a separate roadmap item.
- Credential chains: profiles, instance metadata, SSO.
- Provisioning the bucket, IAM, or lifecycle rules.
