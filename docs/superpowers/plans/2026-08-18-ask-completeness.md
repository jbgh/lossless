# Ask completeness (0.2a) — final plan

Three independent reviews (constraints, code, completeness) plus a parent synthesis. This is the plan to implement. It is not 0.2b.

**Goal:** When someone calls `ask`, the pack includes this session's latest claims, and "I'll ask lossless…" is not stored as a decision.

**Architecture:** Store-first `maybeCatchUp`. Omitted `session_id` catch-up stored sessions for this workspace that are behind. Set-but-unknown `session_id` is exact locate only. Reuse `write.CatchUp`. Gate `i'll ask`. No harness-home walk. No Stop inject. No Claude `additionalContext`. No `active/` file.

**Stack:** Go 1.24. `internal/retrieve`, `internal/harness`, `internal/gate`, `internal/write`, `internal/store`.

## Global constraints

- No LLM on retrieve. No hosted embeddings. Age never disqualifies.
- Hooks fail-open. Do not use Stop to nag or auto-inject packs.
- Claims are `owner/repo`. `WFailedOverlap` 4.0 / `WShippedOverlap` 2.5 unchanged.
- Reset-on-shrink stays. Pathless two-hop still packs the grounded failed.
- `looksLikeOwnPayload` on any role; nearby is assistant-only. Do not touch parse.
- Do not ingest go-test / fixture sessions into a live home.
- Product copy says lossless, never we. Do not rewrite README in this slice.
- MCP does **not** auto-fill `session_id`. Store-first is the response.
- This slice does not implement 0.2b.

## Files

| File | Role |
|------|------|
| `internal/gate/gate.go` | Add `i'll ask` / `i will ask` to `planning` |
| `internal/gate/gate_test.go` | SkipProse / Planning / I'll-stick-with lock |
| `internal/retrieve/catchup.go` | Widen `maybeCatchUp` as below |
| `internal/retrieve/catchup_test.go` | New tests A–D plus the extra cases listed |
| `internal/retrieve/ask_test.go` | Existing `TestAskCatchesUpSessionTape` must stay green |
| `docs/algorithm.md` | Step 1 still says "if `session_id` is set". Update after the code change. |
| `CHANGELOG.md` | 0.1.2 only after the slice is done |

Do not touch `internal/retrieve/weights.go`. Do not add SessionStart hooks. Do not add newest-mtime to `LocateGrok`. Do not change `compile.locateSession` (owned raw only).

`docs/pipeline.md` Caller B and compact mitigation 3, and `docs/stack.md` retrieve line, are already honest. Leave them.

---

### Task 1: Gate `i'll ask`

**Files:** `internal/gate/gate.go`, `internal/gate/gate_test.go`

Planning already drops `i'll check` / `i'll look` / `i'll inspect`. Extract classifies "I'll ask lossless…" as a decision because `decisionRE` matches `decided` and `Planning` is false. Both write (`extract_classify.go`) and retrieve (`extractNoise` in `query.go`) already consult `Planning`.

- [ ] Failing tests in `TestSkipProse`:

```go
if !SkipProse("I'll ask lossless what's already decided, then look through the repo.") {
    t.Fatal("i'll ask planning")
}
if !SkipProse("I’ll ask lossless what's already decided.") { // curly apostrophe
    t.Fatal("curly i'll ask")
}
if SkipProse("Use the ask tool before implementing in src/app.ts.") {
    t.Fatal("real ask mention")
}
if SkipProse("I'll stick with postgres instead of mysql.") {
    t.Fatal("i'll stick with is a decision")
}
```

- [ ] `go test ./internal/gate/ -count=1` — first two fail.

- [ ] Append to `planning` only:

```go
"i'll ask", "i will ask",
```

`Fold` already maps curly apostrophes. Do **not** add an `i'll ` prefix. "I'll ask the user" will also drop. That is planning, not a standing decision.

- [ ] `go test ./internal/gate/ ./internal/write/ ./internal/retrieve/ -count=1`

- [ ] After this build is the live daemon: `lossless inspect --project jbgh/lossless --prune`. Those two live rows must supersede:
  - `202608180521305f51b534e1b1aac2`
  - `202608181727498344ab7c3a6117ab`

---

### Task 2: Widen `maybeCatchUp` (store-first)

**Files:** `internal/retrieve/catchup.go`, new `internal/retrieve/catchup_test.go`

Today (`internal/retrieve/catchup.go:11-18`) returns when `req.SessionID == ""`. MCP marks the field optional. Watcher usually already inserted the session. That is the hole.

`prepare()` already calls `maybeCatchUp` first (`ask.go:81`), **before** `normalize`. Derive project yourself (`projectkey.FromWorkspace` / `Normalize`). Do not wait for `query.ProjectKey`.

Order:

1. `Store == nil` → return.
2. If `req.SessionID != ""` and `SessionByID` has a JSONL → today's path: stat, cursor, `write.CatchUp` with **stored** `SessionID`, `Harness`, workspace/project fallbacks. `Source: "turn"`.
3. Else if `session_id` is set, not stored, and `workspace_root` is set → exact locate, then `os.Stat` (Grok returns a path even when the file is missing):

```text
LocateGrok(ws, sid)              // then Stat
LocateClaude("", sid, ws)        // then Stat
LocateCodex("", sid, "")         // empty cwd — no newest-cwd fallback
LocatePi("", sid, "")            // empty cwd — no newest-cwd fallback
```

`LocateCodex` / `LocatePi` **with cwd set** fall through to newest-mtime for that cwd and stamp the requested sid on the wrong file. Never pass cwd to those two.

`CatchUp` only if the file exists. Pass locate `SessionID` (never `""`) and the harness that hit. Empty harness becomes `"other"`.

4. Else if `session_id` is empty → `ListSessions()` (there is no `SessionsByWorkspace`). If `workspace_root != ""`, keep rows whose `filepath.Clean(Workspace) == filepath.Clean(workspace_root)`. Do **not** OR-match project: that would catch-up every worktree of `jbgh/lossless`. If `workspace_root` is empty, match `projectkey.Normalize(req.Project)` or `FromWorkspace`. For each with a JSONL whose cursor is behind, `CatchUp` with the **stored** session id, harness, and workspace.

5. Do **not** walk `~/.grok/sessions`. Do not call `LocateGrok(ws, "")`. Do not point `compile.locateSession` at a live harness file.

`CatchUp` already refuses fixtures, resets on shrink, waits for a complete JSONL line, no-ops at EOF. Keep `_, _ = write.CatchUp(...)`. Ask still returns.

The "no 17 MB import" ban is for **omitted** `session_id` (step 4). Step 3 may first-ingest that one file. With `serve --watch` that is "sid before first Tick" or daemon down. Do not "fix" it with a home walk.

#### Tests

Use `t.TempDir()` store (`tmpStore` already matches `goTestDir`, so `refuseTestIngest` allows ingest). Set `GROK_HOME` to a **temp** tree in any locate test. Do not use session ids that match `claim.FixtureSession` (`sess1`, `grok-auth`, …). Set `Project` on both `UpsertSession` and `Ask` (a temp `workspace_root` hashes to `path-<hash>`, not `acme/api`). `LOSSLESS_SIDECAR` is irrelevant here (`CatchUp` is in-process); `off` is only hygiene.

- [ ] **A.** Two stored sessions, same cleaned workspace; only one file behind. `Ask` with **empty** `session_id` + that `workspace_root` + `Project`. Extracts from the behind file only. Claim `SessionID` is the directory id, not `chat_history`.

- [ ] **B.** `session_id` set, row not in store, file exists under temp `GROK_HOME`. `Ask` catch-up that file. Pack sees the new decision.

- [ ] **C.** Cursor at EOF. `Ask` does not double-extract. Existing `TestAskCatchesUpSessionTape` still passes (sid set + stored → step 2 only, no Locate).

- [ ] **D.** Last line without `\n` is not ingested. Cursor not advanced.

- [ ] **E.** `session_id` set, not stored, **no** `workspace_root`: no-op, no home walk, no `chat_history` session.

- [ ] **F.** Already-stored decision "I'll ask lossless what's already decided…" is dropped by `extractNoise` after Task 1.

Keep `compile_test.go` green.

---

### Task 3: Docs that this slice makes true

- [ ] `docs/algorithm.md` step 1: omitted `session_id` catch-up stored workspace sessions that are behind; set-but-unknown `session_id` is exact locate only. Do not change `docs/ask.md` "newest session" into a harness-home walk (`hydrate.go` uses `"default"` for the action tape, not newest-mtime).

- [ ] `CHANGELOG.md` 0.1.2 after tests are green. Not 0.2.0 (that name is the awareness slice).

Do not rewrite README.

---

### Task 4: Verify

- [ ] `go test ./...` (year + eval).
- [ ] `lossless inspect --project jbgh/lossless --prune` — the two I'll-ask ids above are gone from recent.
- [ ] `lossless doctor` — last ask listed. Daemon on this build.
- [ ] Live MCP-shaped ask: `workspace_root` + `goal` + `paths`, **no** `session_id`. Catch-up any stored session for this workspace that is behind. Pack is whatever the store already earned; empty is valid if nothing is behind.

---

## Out of this plan

- Claude SessionStart / UserPromptSubmit `additionalContext` (0.2b, and only after README "Not auto-injection" is rewritten)
- `~/.lossless/active/<project>.md` (0.2b, pull, can split from Claude inject)
- OpenCode watcher, Codex empty-rollout, Claude watcher unknown-cwd (0.4)
- MCP auto-fill `session_id` (killed)
- Newest-mtime compile on the live harness file (killed)
- Vectors, archive, scrub, `weights.go`
- First-run doctor `ask`

Those stay on [docs/roadmap.md](../../roadmap.md).
