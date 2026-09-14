# Deploy: one binary, any box

[Docs](README.md) · [ask](ask.md) · [harnesses](harnesses.md)

lossless is a process with two surfaces:

| Surface | Where | What it is |
|---------|-------|------------|
| **REST** | `/v1/ask`, `/v1/remember`, `/v1/catch-up`, `/v1/append`, `/v1/records/:id` | The store |
| **MCP** | `/mcp` (HTTP) or `lossless mcp` (stdio client of that daemon) | How an agent calls the store |

Any agent harness that can call an authenticated HTTP MCP server or those REST endpoints can use lossless. Grok, Claude, Codex, Pi, and OpenCode get a one-command installer. Everything else is the same URL and the same token.

We do not provision AWS, GCP, Hetzner, Tailscale, systemd on a VPS, or TLS. Those are yours. The binary is OS-agnostic: run `lossless serve` wherever Go runs.

Default install is **this machine only**. Setup never reads `LOSSLESS_URL`. Pointing a laptop at a remote home is a documented, manual step.

---

## Local (the complete product)

```bash
lossless setup          # hooks + MCP + skill for known harnesses
lossless serve          # REST + /mcp on 127.0.0.1:7432
lossless update         # later: GitHub Releases → ~/.local/bin/lossless
```

No token. Nothing listens off loopback. Nothing is uploaded. You can skip `setup` and only run `serve` if you will configure the client yourself.

### Update channel

The first public cut is **0.1.0**. Binaries live on GitHub Releases for `jbgh/lossless`.

```bash
curl -fsSL https://github.com/jbgh/lossless/releases/latest/download/install.sh | sh
lossless setup
lossless update          # existing install
lossless update --check  # no write
```

`install.sh` and `lossless update` download `lossless-<os>-<arch>` plus `SHA256SUMS`, verify the digest, refuse non-https / off-host redirects, and rename over `~/.local/bin/lossless` so a dest symlink is replaced instead of followed. `update` then rewrites hooks and the user service to that path and restarts the daemon.

This is opt-in. `doctor` and `serve` do not call GitHub. `jbgh/lossless` is public, so no token is required. Set `GITHUB_TOKEN` (or `GH_TOKEN`) only if you point `LOSSLESS_UPDATE_REPO` at a private fork.

---

## The contract

### Auth

- Loopback (`127.0.0.1`, `localhost`) may be open. That is by design.
- A non-loopback `--listen` **requires** `--token` (or `LOSSLESS_TOKEN`). No token + public bind = refuse to start.
- Remote clients must use `https`. Loopback `http://127.0.0.1` is fine.
- Outbound clients do not follow redirects (a 302 cannot bounce a bearer to another host).
- Send the token as `Authorization: Bearer <token>`.

Generate one if you need it: `lossless token`. Put it in the environment of the process that serves and the process that calls. We do not write it into harness config files; those reference `${LOSSLESS_TOKEN}`.

### REST

```
POST /v1/ask          Authorization: Bearer <token>
POST /v1/remember
POST /v1/catch-up     # local sidecar: copy a session file we can read
POST /v1/append       # remote home: receive already-redacted JSONL
GET  /v1/records/:id
GET  /health
```

`ask` / `remember` bodies match the MCP tools. See [ask.md](ask.md).

`POST /v1/append` is how a sidecar ships new bytes to a home on another machine. The home never sees `/Users/you/.grok/sessions/...`.

```
POST /v1/append
Authorization: Bearer <token>
Content-Type: application/x-ndjson
X-Project: acme/api
X-Harness: grok
X-Session: 01a002a6-…
X-Client: <install id>
X-Prev-Offset: 4096

<body: complete JSONL lines only>
```

```
200 { "accepted_through": 8192, "extracted": 2 }
409 { "accepted_through": 4096 }   # retry from 4096
```

Idempotent: same `X-Prev-Offset` + same bytes → no-op, 200.

### MCP

HTTP (any client that speaks MCP-over-HTTP):

```
POST https://home.example/mcp
Authorization: Bearer <token>
```

stdio (any client that can spawn a command):

```
command: lossless
args:    ["mcp"]
env:
  LOSSLESS_URL:   https://home.example
  LOSSLESS_TOKEN: <token>
```

`lossless mcp` is an HTTP client of the daemon. It does not hold the store.

---

## Point a harness at it

**Known harnesses** (optional helper — does not move data):

```bash
export LOSSLESS_URL=https://home.example
export LOSSLESS_TOKEN=...
lossless install-mcp --url "$LOSSLESS_URL"
```

That writes the five configs we know (`~/.grok/config.toml`, `~/.claude.json`, `~/.codex/config.toml`, `~/.pi/agent/mcp.json`, `~/.config/opencode/opencode.json`). Restart the harness so MCP attaches.

**Any other harness:** add an MCP server named `lossless` with one of the two shapes above, or call REST directly. The skill and home rule are markdown; copy `internal/harness/skill.md` and `internal/harness/rule.md` into whatever always-on / skill directory that harness loads.

Hooks (write path) still talk to a **local** sidecar. Compact cannot wait on a transatlantic `append`. Local `lossless serve --watch` stays, even when `ask` goes to a remote home. If `LOSSLESS_URL` is remote, the sidecar pushes new raw in the background.

---

## Run a home on another machine (manual)

Whatever box you pick — a second laptop, a VM, a container, a machine behind Tailscale — do the same three things. We do not have an AWS script, a GCP script, or a Hetzner script.

1. **Process.** Same binary.

   ```bash
   export LOSSLESS_TOKEN=$(lossless token)
   lossless serve --listen 0.0.0.0:7432 --token "$LOSSLESS_TOKEN" --watch
   ```

   Keep it up with whatever you already use: systemd, launchd, a container restart policy, a process supervisor. `lossless setup` only writes a *user* unit on this login (macOS launchd / Linux systemd --user). It does not touch a cloud VM.

2. **Expose it.** Put TLS in front if it is not loopback: Caddy, nginx, Tailscale Serve, a cloud load balancer. The binary does not terminate Let's Encrypt.

3. **Point clients.** `LOSSLESS_URL=https://…` and `LOSSLESS_TOKEN=…` on every machine that should `ask` that home. Then `install-mcp` or edit the harness yourself.

### Move existing tapes (optional)

lossless does not upload your store for you. If you want history on the new box, copy the data directory:

```bash
rsync -a ~/.lossless/raw/ user@home:~/.lossless/raw/
rsync -a ~/.lossless/export/ user@home:~/.lossless/export/
```

Or start empty. New catch-up on the sidecars will `append` going forward.

Local `raw/` layout is the same on every OS. The home never needs the original harness session paths.

---

## Back up to object storage (optional)

lossless keeps running on local files. This copies the store to an
S3-compatible bucket you own, encrypted with a key you hold, on a schedule.
It is not a home: `ask` never reads the bucket, and nothing here changes the
loopback default. Default install still uploads nothing.

```bash
export LOSSLESS_BACKUP_ACCESS_KEY=…   # or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
export LOSSLESS_BACKUP_SECRET_KEY=…
lossless backup init s3://my-bucket/lossless --endpoint https://<account>.r2.cloudflarestorage.com
lossless backup            # first copy now; serve --watch repeats it hourly
lossless doctor            # "backup   ok   s3://… encrypted keep=5 last ok 3m ago, due in 57m"
```

`backup init` writes `~/.lossless/backup.env` and `~/.lossless/backup.key`
(both `0600`). **Copy `backup.key` somewhere that is not this machine.**
Restore is impossible without it. Credentials present in the environment at
init time are written into `backup.env`; otherwise add them there.

### What is copied

| Path | How |
|------|-----|
| `raw/**/*.jsonl.zst` | Sealed tape parts. Uploaded once, never re-read. |
| `raw/**/*.jsonl` | Live parts. Copied under a shared lock, re-uploaded as they grow. |
| `export/**/*.md` | Claims. |
| `index/*.sqlite` | `VACUUM INTO` snapshots. Restore needs no rebuild. |

Nothing else in the home is read. `spool/`, `active/`, `serve.log`,
`service.env`, and the backup files themselves stay on this machine.

Every object is chunked AES-256-GCM under a per-object key derived from
`backup.key`. Object names are an HMAC of the path. The bucket shows no
project names, session ids, or paths. Someone with bucket read access learns
nothing; someone with write access can delete or roll back, and a rollback
shows in `restore --list`.

### Generations

Each run that changed anything writes a full manifest and moves the
pointer. The last five generations are kept (`--keep`), and an object is
deleted only when no kept generation references it. A run with no changes
writes nothing. Bucket versioning is not assumed; Cloudflare R2 does not
implement it.

```bash
lossless restore --list
lossless restore --at 20260914T151500Z-a1f3
```

### Schedule

`serve --watch` runs a backup when one is due: last success plus the
interval, or two minutes after the daemon starts if that is already past.
A daemon that restarts daily with a daily interval still backs up. A failed
run retries in fifteen minutes. `--every 0` at init turns the schedule off;
`lossless backup` is always available by hand. `doctor` warns when the last
success is older than twice the interval, or when the last attempt failed.

### Restore

On a new machine: install lossless, run `lossless setup`, copy `backup.env`
and `backup.key` into `~/.lossless`, stop the daemon, then:

```bash
lossless restore           # latest generation into an empty store
lossless setup             # or just start serve again
```

`restore` refuses a running daemon and a non-empty store. `--force` unions
into a non-empty store: index snapshots are replaced, raw and export files
that differ locally are left alone and listed. The restored machine becomes
the bucket's writer; if the old machine comes back it refuses to back up
until you run `lossless backup --take-over` there on purpose.

### Cloudflare R2

- Endpoint `https://<account>.r2.cloudflarestorage.com`, or
  `https://<account>.<jurisdiction>.r2.cloudflarestorage.com` for a bucket
  created in a jurisdiction.
- Create an R2 API token with **Object Read & Write** scoped to the bucket.
  Cloudflare shows the S3 Access Key ID and Secret Access Key once, on the
  token page. Those are the two credentials above.
- Region is `auto`; the client sets it for any R2 endpoint.
- R2 has no bucket versioning. Generations are the history.

Any other S3-compatible store works the same way with its endpoint. AWS
needs no `--endpoint`.

---

## Security (the parts we own)

- Public listen requires a bearer. Loopback does not.
- Remote URL must be `https`. No redirect following.
- Sidecar redacts before bytes leave the machine. Home redacts again.
- Store dirs `0700`; `export/`, `raw/`, `spool/`, sqlite `0600`.
- Catch-up refuses symlinks and non-`.jsonl`. Claim IDs are a single path-safe token.
- Disk encryption, firewall, and TLS certs are yours.

This is your process on your machine. It is not us hosting your transcripts.

---

## Identity

| Id | Meaning |
|----|---------|
| Bearer token | Which brain. One token, one `raw/` tree. |
| `project_key` | `owner/repo`. Shared across machines. |
| `session_id` | Harness session. Globally unique. |
| `client_id` | This install. For append acks. Not a security boundary. |

---

## Out of scope

- Provisioning a cloud, a VPS image, or a TLS certificate
- An automatic `migrate` that rewrites your machine and ships the store
- Per-user ACL, invite links, orgs, billing
- S3 as the store `ask` reads from. A bucket is a copy (`lossless backup`), never a home.
- Hooks that POST straight to a remote home (they stay local and fail-open)
