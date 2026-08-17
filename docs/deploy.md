# Deploy: one brain, many harnesses

Two first-class ways to run it. Same binary, same APIs, same hooks.

| Mode | Who it is for | Where bytes live |
|------|----------------|------------------|
| **Local** | One machine, nothing leaves disk | Home + sidecar in one process on `127.0.0.1` |
| **Home** | Many laptops / worker VPS, one brain | Canonical store on a box you run; sidecars push increments and `ask` it |

Neither is a fallback of the other. Pick at install. You can start local and point sidecars at a home later without rewriting data (`raw/` + `export/` are the same layout).

**Still not a vendor SaaS.** You run the home (a VPS you control, or Tailscale, or localhost). The corpus does not go to our cloud or Mem0. One bearer token = one brain = one person (or one team that accepts a shared past). Multi-tenant ACL is out of scope.

---

## Shape

```
  laptop A   Grok, Claude  ──hooks──► sidecar (this machine)
  laptop B   Codex         ──hooks──► sidecar
  VPS worker OpenCode      ──hooks──► sidecar
                                      │
                                      │  incremental append (async, reliable)
                                      ▼
                                 HOME (your VPS)
                                 raw/ + export/ + ask
                                      ▲
                                      │  POST /v1/ask
                                 any sidecar / skill
```

| Process | Where | Job |
|---------|-------|-----|
| **Home** | The long-lived box | Canonical `raw/` + `export/`. Derives claims, serves `ask`. |
| **Sidecar** | Every machine that runs a harness | Hooks, spool, incremental push. May proxy `ask`. |

Same Go binary. `--home-mode` vs default sidecar. Local-only: one process does both.

---

## Why the hook never talks to the VPS directly

PreCompact has **< 1s**. A new session can have **megabytes** not yet shipped. A worker in `us-east` talking to a home in `eu` cannot make that the critical path.

```
hook (fail-open, <200ms)
  → append new harness bytes to local spool
  → return

sidecar (background)
  → POST incremental chunks to home
  → home appends raw, derives claims
  → ack offset; sidecar drops spool prefix
```

If the home is down, spool grows on the laptop. Compact still succeeds. Memory is **eventually** on the home, not instantly. That is the honest contract.

`ask` is a small JSON round trip. Skills talk to **home** (`LOSSLESS_URL`). If home is unreachable, sidecar can answer from whatever it has already shipped *and* still has in spool (best-effort, v1.1). v1: ask fails soft and the agent continues.

---

## Write protocol (home)

Hooks do not send filesystem paths from another OS. The home never sees `/Users/you/.grok/sessions/...`.

```
POST /v1/append
Authorization: Bearer <token>
Content-Type: application/x-ndjson
X-Project: acme/api
X-Harness: grok
X-Session: 01a002a6-…
X-Client: <install id>
X-Prev-Offset: 4096          # bytes already acked for this session+client

<body: complete JSONL lines only>
```

```
200 { "accepted_through": 8192, "extracted": 2 }
409 { "accepted_through": 4096 }   # client was behind; retry from 4096
```

Idempotent: same `X-Prev-Offset` + same bytes → no-op, 200.

Home writes `raw/<project>/<yyyy-mm>/<session>.jsonl` (and `.partN` if a seal already exists). Then the same derive as local catch-up (redact already applied on the sidecar **and** again on home; defense in depth).

Sidecar tracks `accepted_through` per `(session_id)`. Harness file cursors stay local (how much of the *harness* file we have spooled). Two numbers, two jobs.

---

## Read protocol

Unchanged: `POST /v1/ask` on the **home**. Every laptop and worker uses the same URL. That is how “I compacted on the Mac this morning and the Codex box tonight knows we rejected Redis” works.

Skill / MCP: `LOSSLESS_URL` + `LOSSLESS_TOKEN`. No per-harness memory URL.

---

## Identity

| Id | Meaning |
|----|---------|
| Bearer token | Which brain. One token, one `raw/` tree. |
| `project_key` | `owner/repo`. Shared across machines. |
| `session_id` | Harness session. Globally unique. Never reused across machines. |
| `client_id` | This install. For acks and debugging. Not a security boundary. |

Two Codex workers on the same repo = two session ids, one project. Claims collide only via `claim_hash` (same fact twice → supersede). That is how many writers stay safe without a distributed lock.

---

## Security

- Home binds `127.0.0.1` unless `--listen` is set. Public listen **requires** a token. No token + `0.0.0.0` = refuse to start.
- Remote `LOSSLESS_URL` / sidecar URL must be `https`. Loopback `http://127.0.0.1` is fine. Outbound clients do not follow redirects (so a 302 cannot bounce a bearer token to another host).
- TLS via Tailscale Serve or Caddy. The binary does not terminate Let’s Encrypt in v1.
- Token is a high-entropy bearer, stored in env / `0600` `service.env`. Rotating it is a restart. `lossless setup` writes that file so the user service can start the daemon on login. The systemd unit uses `EnvironmentFile` and never inlines the token. Harness MCP configs only reference `${LOSSLESS_TOKEN}`. Existing `~/.claude.json` / `config.toml` modes are preserved so setup cannot make a `0600` file world-readable.
- Sidecar redacts **before** the bytes leave the machine. Home redacts again.
- Store dirs are `0700`. `export/`, `raw/`, `spool/`, and sqlite files are `0600`. Catch-up refuses symlinks and anything that is not a `.jsonl`. Claim IDs must be a single path-safe token.
- Disk on the VPS: your LUKS / provider volume encryption. App-level at-rest encryption is not v1.

This is “in the cloud” as **your** process on **your** VM. It is not us hosting your transcripts.

---

## Local mode (first-class)

```
# no LOSSLESS_URL, or http://127.0.0.1:7432
# no token
# one process: hooks, raw/, ask
lossless serve
```

Catch-up writes `raw/` on this disk. `ask` reads this disk. Nothing listens off loopback. Nothing is uploaded. Tests run this way.

If you later want a shared brain:

```bash
# on the VPS (once)
export LOSSLESS_TOKEN=$(lossless token --write)
# TLS in front (Caddy / Tailscale Serve). Then:
lossless serve --home-mode

# on the laptop that already has local memory
export LOSSLESS_URL=https://home.example
export LOSSLESS_TOKEN=...
lossless migrate
lossless doctor
```

`migrate` uploads `raw/` tapes through `POST /v1/append` (resume-safe), rewrites MCP to the home, and updates `service.env`. Local `lossless serve` stays for hooks. No scp of sqlite. No format change.

If you never want that: stay on local forever. That is a complete product.

---

## What we build, in order

1. Local process that is already home+sidecar (catch-up as now).
2. Split: `POST /v1/append` + spool + `LOSSLESS_URL` for ask.
3. `lossless setup` still talks to the **local** sidecar only for hooks. MCP on a remote home uses `LOSSLESS_URL` + `LOSSLESS_TOKEN` (never written into a committed config).
4. Run home on a VPS, point every laptop’s sidecar at it.

Do not skip (1). Do not make Grok’s PreCompact call the VPS in step (1).

---

## Out of scope until someone is paying for a team brain

- Per-user ACL inside one home
- Invite links, orgs, billing
- S3 as the raw store
- Clients without a sidecar (pure HTTP from the hook)
