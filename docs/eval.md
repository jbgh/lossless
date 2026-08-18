# Memory benchmark

[Docs](README.md) · [retrieval](retrieval.md)

This is our eval, not LoCoMo. Cases ingest simulated Grok/Claude session files,
then score whether `ask` packs the gold context. No LLM judge.

```
testdata/bench/sessions/*.jsonl   # harness-shaped transcripts
testdata/bench/cases/*.json       # write gold + ask gold
```

```bash
go test ./eval/ -run TestSimBenchmarkSuite -v
lossless bench --root testdata/bench
make bench                         # suite + stress + CLI scorecard
```

Each case scores:

- **Write:** extract produced the required types/substrings and did not leak secrets.
- **Ask:** each request’s pack contains the needles, excludes decoys, and fires the right warning.
- **Recall:** needles hit / needles required, averaged per case.

Stress (`eval/stress_test.go`): 10k decoy claims + gold failed (p50 < 500ms, p95 < 2s), 32 concurrent asks, 80-session catch-up.

Year corpus (`eval/year_sim_test.go`): weekday chatter for 365 days (~630 claims) plus five golds from Aug 2025 through Aug 2026. Asks check that a year-old jose decision, a Nov Redis failure, a Feb constraint, a May postgres pick, and an Aug warehouse timeout still pack, and that auth work does not leak invoices. Also catch-up of 120 generated sessions (~60/s) and a cross-project isolation check.

Multi-discipline year (`eval/year_disciplines_test.go`): ~260 weekday Grok/Claude/Codex JSONL sessions across mobile, kernel, frontend, backend, game, and infra (~290 extracted claims). Golds are planted in the tape, ingested via CatchUp, then backdated. Asks check year-later recall and no cross-discipline bleed. A newer failed on the same file must not drop an unrelated older decision (invalidation jaccard ignores path tokens).

Add a case by dropping a JSONL in `sessions/` and a JSON file in `cases/`. Gold is substrings and types, not claim ids, because extract assigns ids at ingest.

Algorithm notes the suite forced:

- Do not split sentences on `auth.ts` (file-extension dots).
- Do not classify “unless we already rejected that” as failed.
- `jsonwebtoken` and `jwt` are the same identifier.
- Newer decision on the same path wins over an older conflicting one.
- Failed/decision/constraint from early in a long session still extract.
- "error handling" is not a failure. Tool dumps are not claims.
- Hedging ("I don't think") and questions ("Should we use") are not constraints.
- Tried Redis, "fixed" it, it failed again: do not pack the dead "use Redis again" decision.
- Pathless JWT must not pack last week's warehouse timeout.
- Pathless "add rate limiting" hops through the limiter decision's file to the Redis failed.
- With an embedder, "add throttling" / "the cache idea we tried" finds the Redis failed that shares no tokens. Without one, that ask misses. Cosine still cannot beat a failed-on-path record.
- Thin ask after a rich JWT ask on the same session still packs jose (action tape).
- GET on a claim (dwell) then “what were we looking at” packs that claim.
- Same session, new path (auth → billing): warehouse only. Redis is not packed and does not warn.
