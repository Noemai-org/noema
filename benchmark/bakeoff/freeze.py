"""The hashes frozen by benchmark/decisions/2026-10-27-model-selection.md, computed the one way the record states.

corpus hash        `cases_digest(generate(seed=1, per_cell=20))`: SHA-256 over the canonical bytes of the cases in id order.
scoring-code hash  SHA-256 over the lines `<sha256 of the file>  <path>\\n`, the files sorted by path (SCORING_FILES, below).
Both are recomputed by test_freeze.py against the record, so a change to the corpus or the scoring code after the freeze fails CI.
"""
import hashlib
from pathlib import Path

from benchmark.bakeoff.cases import cases_digest, generate

CORPUS_SEED = 1
CORPUS_PER_CELL = 20
SCORING_FILES = ("benchmark/bakeoff/gates.py", "benchmark/scoring/stats.py")


def corpus_hash() -> str:
    return cases_digest(generate(CORPUS_SEED, CORPUS_PER_CELL))


def scoring_code_hash(root: Path) -> str:
    lines = "".join(f"{hashlib.sha256((root / path).read_bytes()).hexdigest()}  {path}\n" for path in sorted(SCORING_FILES))
    return "sha256:" + hashlib.sha256(lines.encode()).hexdigest()
