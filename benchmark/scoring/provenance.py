"""Which results a scoring run read (UND-13, REQ-BEN-009; Codex P1 on noema#199), written as `provenance.json` beside `scorecard.json`.

The scorecard's pass^k rests on five result sets, but the scorecard names only the primary run. A qualification that is shown four more scorecards cannot tell whether they
are the runs behind that pass^k. The provenance carries each run's id and the digest of the result set the scorer read, so the qualification can check the match.
The format is `contracts/schemas/benchmark-run-provenance.v1.json`. Generated layer only (INV-22)."""
from collections.abc import Mapping, Sequence
from typing import Any

from benchmark.contracts import sha256_of
from benchmark.scoring.diagnostics import understanding_dependent

Json = dict[str, Any]
SCHEMA = "benchmark-run-provenance.v1"


def results_digest(results: Sequence[Mapping[str, Any]]) -> str:
    """The digest of one result set, taken over canonical JSON with the results ordered by case_id, so file order never changes it."""
    return sha256_of(sorted((dict(r) for r in results), key=lambda r: str(r.get("case_id"))))


def run_ids(results: Sequence[Mapping[str, Any]]) -> list[str]:
    return sorted({str(r.get("run_id")) for r in results})


def context_digest(cases: Sequence[Mapping[str, Any]], oracles: Sequence[Mapping[str, Any]], pack: Any) -> str:
    """What the results were scored against: the cases, the oracles (each ordered by case_id) and the pack (its capability registry is where a prohibited capability is decided).
    Repeat results rescored against another pack, case set or oracle set get another digest (Codex P1 on noema#199)."""
    def ordered(items: Sequence[Mapping[str, Any]]) -> list[Json]:
        return sorted((dict(i) for i in items), key=lambda i: str(i.get("case_id")))
    return sha256_of({"cases": ordered(cases), "oracles": ordered(oracles), "pack": pack})


def understanding_cases_digest(cases: Sequence[Mapping[str, Any]]) -> str | None:
    """The digest of the understanding-dependent cases the scorer read, in case-id order: the set pass^k is measured on. It is computed exactly as the qualification subset's
    `cases_sha256` is, so a qualification can require the two to be equal (Codex P2 on noema#199: scores on another, easier set must not qualify a model). None when there is none."""
    copies = [dict(c) for c in cases]
    chosen = sorted((c for c in copies if understanding_dependent(c)), key=lambda c: str(c["case_id"]))
    return sha256_of(chosen) if chosen else None


def provenance(card: Mapping[str, Any], results: Sequence[Mapping[str, Any]], repeat_runs: Sequence[Sequence[Mapping[str, Any]]], context_sha256: str, pinned: Mapping[str, str] | None = None, pass_k_cases_sha256: str | None = None) -> Json:
    """`pinned` is the identity of the model whose results were scored, as declared to the scoring run (or None): a qualification refuses pass^k evidence declared for another model."""
    runs: list[Json] | None = None
    if repeat_runs:
        runs = [{"run_id": card["run_id"], "results_sha256": results_digest(results)}]
        for one in repeat_runs:
            ids = run_ids(one)
            runs.append({"run_id": ids[0] if len(ids) == 1 else "|".join(ids), "results_sha256": results_digest(one)})
    return {"schema": SCHEMA, "run_id": card["run_id"], "results_sha256": results_digest(results), "scorecard_sha256": sha256_of(card), "context_sha256": context_sha256, "pass_k_cases_sha256": pass_k_cases_sha256, "pinned": None if pinned is None else dict(pinned), "pass_k_runs": runs}
