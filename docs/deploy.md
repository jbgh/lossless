# Deploy: one binary, any box

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
```

No token. Nothing listens off loopback. Nothing is uploaded. You can skip `setup` and only run `serve` if you will configure the client yourself.

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
- S3 as the raw store
- Hooks that POST straight to a remote home (they stay local and fail-open)
