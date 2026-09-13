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
   The bucket, credentials, versioning, and IAM are yours.
3. Bytes are encrypted before they leave the machine, with a key you hold.
   The bucket exposes no paths, project names, or session ids.
4. Incremental forever. Sealed tape parts upload once. A run after the first
   touches live parts, changed claims, and index snapshots.
5. Crash-safe. The manifest is written last. Until it lands, the previous
   state is fully restorable.
6. Hooks never wait on the network. Backup takes a shared lock on a live part
   only long enough to copy it to a temp file.
7. No new modules. SigV4, HKDF, AES-GCM, and HMAC are all Go stdlib.
8. One restorable state in the bucket. Retention and point-in-time history
   are bucket versioning, on your side of the line like disk encryption.

## Surface

### Commands

```
lossless backup init s3://<bucket>/<prefix> [--endpoint URL] [--region R] [--every 1h]
lossless backup [--dry-run] [--verbose]
lossless restore [--force]
```

`backup init` writes `<home>/backup.env` (0600) and generates
`<home>/backup.key` (32 random bytes, hex, 0600). It refuses to overwrite an
existing key. It prints one line telling the operator to copy `backup.key`
somewhere that is not this machine. `--every` writes `LOSSLESS_BACKUP_EVERY`
into `backup.env`.

`backup` runs one incremental backup and prints a summary: files scanned,
uploaded, deleted, bytes sent, elapsed. Non-zero exit on failure.
`--dry-run` stops after planning and prints the plan.

`restore` pulls the latest manifest and every file it names into the store.
It refuses if `GET /health` answers on the sidecar URL. It refuses if `raw/` or
`export/` is non-empty unless `--force`. With `--force` it unions raw and
export and replaces index snapshots. A local file at a different hash is left
alone and listed at the end, never overwritten.

`serve --watch` runs backup on a ticker when `LOSSLESS_BACKUP_EVERY` parses
as a duration. First run fires one interval after start, not at start.

`doctor` prints one line: target, encrypted, age of last successful backup.
Warns when the age is more than twice the interval or there has never been
one. Without `LOSSLESS_BACKUP_EVERY` it warns only when no backup has ever
succeeded. Prints `backup: not configured` when `backup.env` is absent.

All three commands honour `--home` and `LOSSLESS_HOME` like every other
command.

### Config

`<home>/backup.env`, loaded by `serve`, `backup`, and `restore` themselves.
The launchd and systemd units do not change.

| Var | Meaning |
|-----|---------|
| `LOSSLESS_BACKUP_URL` | `s3://bucket/prefix`. Prefix may be empty. |
| `LOSSLESS_BACKUP_ENDPOINT` | Optional. Custom endpoint (R2, B2, MinIO, GCS interop). Path-style addressing when set; virtual-host style for plain AWS. https required unless loopback. |
| `LOSSLESS_BACKUP_REGION` | Default `us-east-1`. R2 uses `auto`. |
| `LOSSLESS_BACKUP_ACCESS_KEY` / `LOSSLESS_BACKUP_SECRET_KEY` | Credentials. Fallback: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`. |
| `LOSSLESS_BACKUP_EVERY` | Optional duration. Enables the watcher ticker. |

No profile files, no instance metadata, no SSO. Those credential chains are
the reason the SDK is heavy.

Other files in the home: `backup.key` (secret), `backup-state.json` (cache),
`backup.lock` (flock), `backup-tmp/` (swept each run).

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
object key. Matching size and mtime skips the read. It also records the
object list of the last manifest this machine wrote and `last_ok`.

The remote manifest is the source of truth. A missing or stale cache means
one full re-hash and an upload of only what differs from the manifest. The
cache is never authoritative.

## Bucket layout

```
<prefix>/manifest              encrypted, written last
<prefix>/o/<name>/<sha256>     one object per file version
```

- `name` = hex(HMAC-SHA256(name_key, relpath))[:32]. Paths never appear in
  the bucket.
- `sha256` = hash of plaintext content. Two versions of a live part coexist
  until the next successful run deletes the older one.
- No LIST in v1. Every operation is by known key.

### Manifest (plaintext, before encryption)

```json
{
  "version": 1,
  "created_at": "2026-09-12T20:15:00Z",
  "lossless": "0.1.26",
  "client": "<install id>",
  "files": {
    "raw/jbgh__lossless/2026-09/2e49….part2.jsonl.zst": {
      "sha256": "…", "size": 442931, "object": "o/<name>/<sha256>"
    }
  }
}
```

Keys in `files` are relative to the home with forward slashes on every OS.
The manifest is encrypted with relpath `manifest` as AAD and stored at
`<prefix>/manifest`. It is overwritten in place. S3 PUT is atomic per key.

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
- Ciphertext length = 13 + n × 16 + plaintext length. Computable up front, so
  PUT streams with a known `Content-Length` and no second pass.
- SigV4 with `x-amz-content-sha256: UNSIGNED-PAYLOAD` over https. Integrity
  is the GCM tag on every chunk plus the plaintext sha256 check on restore.

Restore decrypts streaming to `<target>.restore-tmp`, verifies the plaintext
sha256 against the manifest, then renames.

No plaintext mode in v1. A later path-named plaintext layout can be added
without touching this one.

## Backup run

Single-flight under `flock` on `<home>/backup.lock`. Daemon and CLI never
overlap. A run that cannot take the lock exits with "backup already running".

1. Load `backup.env` and `backup.key`. Fail fast on missing or malformed
   config before any network call.
2. GET manifest. 404 → empty manifest (first run). Decrypt. Wrong key →
   "backup.key does not match this bucket".
3. Sweep `backup-tmp/`. Walk the three roots. Build the desired set
   `relpath → (sha256, size)` using the cache. Copy live parts and take
   sqlite snapshots into `backup-tmp/` here.
4. Plan.
   - upload = paths whose `(relpath, sha256)` the remote manifest does not
     hold.
   - delete = objects the remote manifest references that the desired set no
     longer does (pruned claims, superseded live parts, replaced snapshots),
     plus any objects in the cache's last-written list that the previous
     run failed to delete.
5. Upload, 4 in flight. Retry 3× with backoff (1s, 4s, 16s) on network
   error, 429, 5xx. No retry on 400, 403, 404-on-PUT; fail with the status
   and the S3 error code.
6. PUT manifest. Commit point.
7. Delete stale objects, 4 in flight. Failures are logged, not fatal. The
   list is kept in the cache and retried next run.
8. Write cache with `last_ok`. Sweep `backup-tmp/`. Print summary.

`--dry-run` runs steps 1–4 and prints the plan.

## Restore run

Takes the same lock.

1. Refuse if the sidecar `/health` answers. Refuse on non-empty `raw/` or
   `export/` without `--force`.
2. Load config and key. GET manifest. Decrypt.
3. For each file, 4 in flight: skip if the local file exists at the manifest
   hash (resume). Under `--force`, overwrite index snapshots; for raw and
   export, skip a local file at a different hash and list it. Otherwise GET,
   decrypt to `.restore-tmp`, verify sha256, rename.
4. Write `backup-state.json` from the manifest so the next backup on this
   machine does not re-hash.
5. Print summary: restored, skipped, left alone, bytes.

The restored `claims.sqlite` carries the action tape and cursors. Cursor rows
name session files from the old machine; those sessions are dead, so they are
inert. `store.Open` runs migrations as usual.

## Scheduling

`watch.Run` already owns the catch-up ticker and an hourly sweep ticker. A
backup ticker joins them when `LOSSLESS_BACKUP_EVERY` parses. The run goes in
its own goroutine. A tick while the previous run holds the lock is skipped and
logged. Errors go to `serve.log` with the time and version stamp. Catch-up and
`ask` never wait on it.

## Errors

| Case | Behaviour |
|------|-----------|
| No `backup.env` | `backup` and `restore` exit 2 with "run lossless backup init". `serve` does nothing. `doctor` says not configured. |
| Missing or malformed key | Exit 2 before any network call. |
| Bad URL, http endpoint off loopback | Exit 2. Same rule as remote home. |
| 400 / 403 from the bucket | Exit 1 with status and S3 error code. No retry. |
| Network, 429, 5xx | 3 retries per object, then exit 1. Manifest not written. Previous state restorable. |
| Crash mid-run | Next run sweeps `backup-tmp/`, retries deletes from the cache list, re-plans from the remote manifest. |
| Wrong key on restore | "backup.key does not match this bucket". Nothing written. |
| Disk full on restore | Tmp write fails. Nothing half-renamed. Exit 1. |
| Lock held | "backup already running". Exit 1. |

## Packages

```
internal/backup/s3/      SigV4 signer; client: Put, Get, Head, Delete; retry; URL rules
internal/backup/crypt/   HKDF keys; HMAC namer; chunked AES-GCM writer and reader
internal/backup/         config (load, init); state cache; lock; walk + snapshot; plan; Run; Restore
cmd/lossless/backup.go   runBackup (init | run), runRestore; help text
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
shape and `x-amz-content-sha256`, supports PUT, GET, HEAD, DELETE, and a
per-key failure injector.

- **Signer.** Published AWS SigV4 test vectors with the fixed date and the
  example keys; signature compared byte for byte.
- **Crypt.** Round trip. One flipped byte, swapped chunks, truncated tail,
  wrong relpath each fail. Empty file, exactly 1 MiB, 1 MiB + 1.
- **End to end.** Temp store via `store.Open`, seeded with sealed parts, one
  live part, claims. Run 1 uploads all. Run 2 uploads nothing. Append to the
  live part: one upload, one delete. Prune a claim: one delete. Restore into
  a fresh root, `store.Open`, `ask` returns the same records.
- **Restore guards.** Refuses non-empty without `--force`. With `--force`,
  leaves a differing local file and lists it.
- **Commit point.** Fake fails the manifest PUT; the previous manifest still
  restores.
- **Snapshot.** A write left in the WAL is present in the `VACUUM INTO` copy.
- **Watcher.** A slow backup does not block the tick; the next tick skips.
- **Doctor.** Not configured, fresh, stale.
- **Live.** One test against a real bucket, skipped unless
  `LOSSLESS_BACKUP_LIVE_TEST` names it. Run by hand before shipping.

## Docs

- `docs/deploy.md`: new section "Back up to object storage" (init, key
  custody, what is copied, what is not, restore, versioning is yours). The
  out-of-scope line "S3 as the raw store" becomes: `ask` reads local files;
  a bucket is a copy, never a home.
- `docs/roadmap.md`: "no S3" line gets the same clarification; operator table
  gains a backup row.
- `README.md`: "Not a cloud" stays. One line under local-by-default about
  opt-in encrypted backup to a bucket you own.
- `CHANGELOG.md`: 0.1.26 entry.
- `lossless remember` the standing decisions from this spec so the next
  session's `ask` packs them.

## Out of scope

- Multi-machine sync or merge. One writer per bucket prefix.
- Retention, point-in-time history, `--prune` with LIST. Bucket versioning
  covers history.
- Plaintext (`--no-encrypt`) layout.
- Multipart upload. Largest object is a monthly excerpts sqlite (~100 MB);
  single PUT is good to 5 GB.
- Project filter.
- `reindex` from `export/` and `raw/`. Restore carries index snapshots, so it
  does not need one. That gap stays a separate roadmap item.
- Credential chains: profiles, instance metadata, SSO.
- Provisioning the bucket, IAM, or versioning.
