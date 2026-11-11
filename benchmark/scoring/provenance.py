"""Which results a scoring run read (UND-13, REQ-BEN-009; Codex P1 on noema#199), written as `provenance.json` beside `scorecard.json`.

The scorecard's pass^k rests on five result sets, but the scorecard names only the primary run. A qualification that is shown four more scorecards cannot tell whether they
are the runs behind that pass^k. The provenance carries each run's id and the digest of the result set the scorer read, so the qualification can check the match.
The format is `contracts/schemas/benchmark-run-provenance.v1.json`. Generated layer only (INV-22)."""
from collections.abc import Mapping, Sequence
from typing import Any

from benchmark.contracts import sha256_of

Json = dict[str, Any]
SCHEMA = "benchmark-run-provenance.v1"


def results_digest(results: Sequence[Mapping[str, Any]]) -> str:
    """The digest of one result set, taken over canonical JSON with the results ordered by case_id, so file order never changes it."""
    return sha256_of(sorted((dict(r) for r in results), key=lambda r: str(r.get("case_id"))))


def run_ids(results: Sequence[Mapping[str, Any]]) -> list[str]:
    return sorted({str(r.get("run_id")) for r in results})


def provenance(card: Mapping[str, Any], results: Sequence[Mapping[str, Any]], repeat_runs: Sequence[Sequence[Mapping[str, Any]]]) -> Json:
    runs: list[Json] | None = None
    if repeat_runs:
        runs = [{"run_id": card["run_id"], "results_sha256": results_digest(results)}]
        for one in repeat_runs:
            ids = run_ids(one)
            runs.append({"run_id": ids[0] if len(ids) == 1 else "|".join(ids), "results_sha256": results_digest(one)})
    return {"schema": SCHEMA, "run_id": card["run_id"], "results_sha256": results_digest(results), "scorecard_sha256": sha256_of(card), "pass_k_runs": runs}
