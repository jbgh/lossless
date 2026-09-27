# `supersede` and warning decay

[Docs](README.md) · [algorithm](algorithm.md) · [retrieval](retrieval.md) · [ask](ask.md)

A constraint is written down during an incident and then keeps warning. The
incident closes, a `state` record says so, and the constraint is still there
blocking every deploy-shaped ask. This page is the two ways out: retire it
explicitly, or let it decay.

## `lossless supersede <id> [reason]`

```console
lossless supersede 20260913044825a1b2c3d4e5f6a7b8 prod node key renewed 2026-09-14
```

Sets `status=superseded` on the record. The row, its text, and its export all
survive — `get_record <id>` still returns it. **Delete never**: supersede is the
only thing this command does. It works offline against the local store; it is
not a daemon call. On a remote home (`LOSSLESS_URL` set) it refuses, because it
edits a local index.

### Where the reason lives

`records` has no reason column, and this adds none. The reason is stored as a
**linked `decision` record**:

- `text` is `Superseded <id>: <reason>`, so a plain text search for the id
  finds the pair, and the reason reads as prose to anything scanning claims
- `supersedes` is the retired record's id, so the link is machine-readable
- `why` is `retires <id>`

`runSupersede` prints the new decision record's id. Reason is optional; without
it there is no extra record and the command is a pure status flip.

The reason record carries the retired record's `paths`, so it surfaces on the
same asks the constraint used to. It is written like any `remember`: redacted,
appended to the manual tape, and given an excerpt. A reason that looks like a
secret is refused before anything changes. Superseding a record that is already
superseded changes nothing and writes no second reason.

## Warning decay

Decay is the blast-radius guard for the constraints nobody gets round to
retiring by hand. It changes **warnings only**. A decayed constraint still
packs into `context[]`, with its visible date, so the history stays readable —
it just stops interrupting.

It is applied in the ask's warning path (`internal/retrieve/ask.go`), after
`emit`. The pack, the weights, and the read-time `statusFailed` gate are
untouched.

Anchors mean different things and decay differently:

| Anchor | Example | Decays |
|--------|---------|--------|
| End anchor | `Migrate off Python 3.8 until 2026-03-01` | once the date has passed |
| Start anchor + incident vocabulary | `Since 2026-09-13 the prod key is EXPIRED` | after the window (14 days default) |
| Start anchor on a standing rule | `Since 2026-01, CI requires Go 1.23` | never |
| No anchor | `Never log Authorization headers` | never |

Incident vocabulary: *expired, expires, down, broken, blocked, outage, not
deployed*. It is what separates an incident snapshot from a standing rule — the
same "since" words.

The two rules are independent. A text with both anchors still decays on the
start anchor when its end date has not passed: "Since 2026-09-13 api-prod's
node key is EXPIRED … NOT deployed … (eu expires 2026-10-06)" is an incident
on api-prod, and the trailing date about a different box must not shield it.
A future `until` date is the constraint's own end, so it keeps the warning past
the window ("blocked since 2026-09-13, do not merge until 2026-12-01" warns
until December); an `expires` date does not, since it states when some thing
expires. When a text names several end dates, the latest one decides: "the staging key
expires 2026-09-20, the prod key expires 2027-01-05" keeps warning until both
have passed.

Decay is measured from the later of the anchor and the record's `created_at`, so
a constraint imported today about an incident from last year earns its own 14
days rather than decaying on arrival.

### Tuning

`Engine.StaleWindow` in Go, or `LOSSLESS_STALE_WINDOW` in the environment. The
value is a Go duration, so `336h` for 14 days — `14d` is not a Go duration. A
zero, negative, or unparseable value falls back to the default: a typo in an
env var must not silently un-retire every incident constraint.

## Re-confirmation

A newer active constraint re-enables a decayed one, by deterministic match
only:

- the same `claim_hash`, or
- at least two of the decayed record's rare code-shaped identifiers, the
  identifiers recurrence detection clusters on (a record with fewer than two
  re-confirms only by `claim_hash`)

Both reuse `recIdents` and the recurrence store scan, so the two features
cannot drift apart. One shared identifier is not enough: a later rule that
only names the same host would revive an unrelated incident. The newer
constraint warns on its own merits anyway. A newer constraint that has itself
decayed re-confirms nothing, so a chain of restatements of one incident decays
link by link. A shared ISO date fragment is not a match: two unrelated
constraints filed the same day would otherwise look like a re-confirmation.

Anything fuzzier — auto-supersede on contradiction — is deliberately out of
scope.

## `inspect --prune`

`lossless inspect --prune` lists stale candidates: decayed, still active, and
not re-confirmed. It is a listing, not a sweep. Nothing is superseded or
deleted by the listing; each line is a prompt to run `lossless supersede`.

```
stale constraints  1 decayed, not superseded — retire with: lossless supersede <id> [reason]
  stale  20260913044825a1b2c3d4e5f6a7b8  incident-window  15d  Since 2026-09-13T04:29:03Z api-prod's node key is EXPIRED…
```

A superseded record leaves the listing. Its row and text stay either way.

## Recurrence warnings

A recurrence warning needs a constraint in its cluster. A decayed constraint
does not count as that constraint, so decay cannot be undone by the
recurrence route citing the same record under another heading. It still
counts as a member of the cluster.

## Why a `state` record does not close this

An incident like this one is typically closed by a `state` record ("#3837
CLOSED, key renewed"). That record carries no paths, and the two
read-time conflict rules both need more than that: `dropOlderConflicts` requires
a same-type pair sharing a path cluster, and `dropInvalidatedByNewerFailed`
retires a decision or constraint only for a newer **failed**. Nothing at read
time links a `state` row to the constraint it closes, and the read-time gate is
out of scope for this change. Hence an explicit primitive plus decay.
