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

## Judged sets: the gate for a ranking change

The suite above checks that planted records are found. It cannot tell a good pack from a padded one: it scored 19/19 on builds where a third of what `ask` returned was judged useless. A change to the score, the pack, the candidate routes or the warnings is measured on real asks instead, and does not ship unless the number moves.

- **Asks:** real `ask` calls taken from session transcripts, with their goal, question and paths. Two sets exist: 60 from October 2026 and 40 from November 2026. The second was drawn after the ranker it tested was written.
- **Store:** a copy of the live index, rolled back to the time of each ask (records created later are hidden, records superseded later are restored), so nothing from the future can be returned.
- **Arms:** the build under test, the current default, and a plain baseline (raw BM25 plus ten per shared file, top five, no pipeline). A change that cannot beat the plain baseline is not an improvement.
- **Labels:** every record any arm returns is labelled for its ask by two judges that see only the goal and the record, shuffled, with no arm named: 2 = wanted before acting, 1 = same area, 0 = not useful. The label is the mean. A few dozen already-labelled pairs are mixed into each round to measure drift between rounds.
- **Score:** the sum of labels per ask (10 is the most), the share labelled under 0.5, and a bootstrap interval on the per-ask difference between arms. Records nobody labelled are counted and reported, never assumed.

The asks, labels and scripts hold text from private projects, so they live beside the store (`~/.lossless/eval/`), not in this repository.

Add a case by dropping a JSONL in `sessions/` and a JSON file in `cases/`. Gold is substrings and types, not claim ids, because extract assigns ids at ingest.

Algorithm notes the suite forced:

- Do not split sentences on `auth.ts` (file-extension dots).
- Do not classify “unless we already rejected that” as failed.
- `jsonwebtoken` and `jwt` are the same identifier.
- Newer decision on the same path wins over an older conflicting one.
- Failed/decision/constraint from early in a long session still extract.
- "error handling" is not a failure. Tool dumps are not claims.
- Hedging ("I don't think") and questions ("Should we use") are not constraints.
- Tried Redis, "fixed" it, it failed again: both failures pack. (v1 also dropped the dead "use Redis again" decision; v2 shows the history.)
- Pathless JWT must not pack last week's warehouse timeout.
- Pathless "add rate limiting" hops through the limiter decision's file to the Redis failed.
- With an embedder, "add throttling" / "the cache idea we tried" finds the Redis failed that shares no tokens. Without one, that ask misses. Cosine still cannot beat a failed-on-path record.
- Thin ask after a rich JWT ask on the same session still packs jose (action tape).
- GET on a claim (dwell) then “what were we looking at” packs that claim.
- Same session, new path (auth → billing): warehouse only. Redis is not packed and does not warn.
