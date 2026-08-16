# Memory benchmark

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

Stress (`eval/stress_test.go`): 10k decoy claims + gold failed (p50/p95), 32 concurrent asks, 80-session catch-up.

Add a case by dropping a JSONL in `sessions/` and a JSON file in `cases/`. Gold is substrings and types, not claim ids, because extract assigns ids at ingest.
