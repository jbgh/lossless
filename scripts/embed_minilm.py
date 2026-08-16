#!/usr/bin/env python3
"""Optional local MiniLM for lossless.

stdin:  {"texts": ["..."]}
stdout: {"vectors": [[...]], "model": "all-MiniLM-L6-v2", "dim": 384}

Install once:  pip install sentence-transformers
Then:          export LOSSLESS_EMBED_CMD="python3 scripts/embed_minilm.py"
"""

from __future__ import annotations

import json
import sys

MODEL = "sentence-transformers/all-MiniLM-L6-v2"


def main() -> int:
    payload = json.load(sys.stdin)
    texts = payload.get("texts") or []
    from sentence_transformers import SentenceTransformer

    model = SentenceTransformer(MODEL)
    vecs = model.encode(list(texts), normalize_embeddings=True)
    out = {
        "vectors": [v.tolist() for v in vecs],
        "model": "all-MiniLM-L6-v2",
        "dim": int(len(vecs[0]) if len(vecs) else 384),
    }
    json.dump(out, sys.stdout)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
