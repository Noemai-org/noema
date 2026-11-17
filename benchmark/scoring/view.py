"""scorecard-view.v1: the projection of a scorecard v3 that a user interface may show (ruling A250, interim). Aggregates only: no case id, no list of case ids, no failure list, no structural-
evidence names, no manual-baseline placeholder. The lists a scorecard keeps are carried as COUNTS. It adds no number and no threshold (AGENT-16); every rate is the scorecard's own.

The projection is an allowlist, not a filter: a member of the scorecard that is not named here is not in the view, so a member added to the scorecard later cannot leak through. It is also
checked at both ends (Codex's P2s on noema#642): the whole source must be a valid scorecard v3 (a closed schema, so an injected nested member is refused, not copied), and the whole view must
be a valid `scorecard-view.v1` before it is returned (its map keys are enumerations or patterns that cannot spell a case id). Either failure is a ValueError, never a partial view."""
from typing import Any, Mapping

from benchmark.contracts import Contracts

Json = dict[str, Any]
CONTRACTS = Contracts()

INCOMPLETE = ("missing", "errored", "stale_version", "duplicate", "unknown", "invalid_case")
DIAGNOSTIC_RATES = ("exact_transaction_identification", "high_risk_ambiguous_not_guessed", "state_reconstruction", "policy_decisions", "writes_idempotent_and_verified",
                    "record_completeness", "explanation_claims_bound", "incorrect_action", "replay", "pass_k", "turns_to_resolution", "unnecessary_clarification", "auto_resolve_precision",
                    "definitions")


def _need(card: Mapping[str, Any], *path: str) -> Any:
    node: Any = card
    for part in path:
        if not isinstance(node, Mapping) or part not in node:
            raise ValueError(f"not a scorecard v3: {'/'.join(path)} is missing")
        node = node[part]
    return node


def _pass_k(block: Mapping[str, Any]) -> Json:
    out = {k: v for k, v in block.items() if k != "failing_cases"}
    out["failing_case_count"] = len(block.get("failing_cases") or [])
    return out


def scorecard_view(card: Mapping[str, Any]) -> Json:
    """The view of one scorecard v3. Raises ValueError for anything that is not a v3 scorecard (a rate without `exact_upper95`, a missing tier): nothing is guessed."""
    problems = CONTRACTS.errors("benchmark-scorecard.v3", card)
    if problems:
        raise ValueError(f"not a valid scorecard v3 ({len(problems)} problem(s)); first: {problems[0]}")
    view = _project(card)
    out = CONTRACTS.errors("scorecard-view.v1", view)
    if out:
        raise ValueError(f"the view is not a valid scorecard-view.v1 ({len(out)} problem(s)); first: {out[0]}")
    return view


def _project(card: Mapping[str, Any]) -> Json:
    diagnostics = _need(card, "tier1", "diagnostics")
    return {
        "run_id": _need(card, "run_id"), "complete": _need(card, "complete"),
        "incomplete_counts": {k: len(_need(card, "incomplete").get(k, [])) for k in INCOMPLETE},
        "n_cases": _need(card, "n_cases"), "layers": dict(_need(card, "layers")), "release_result": _need(card, "release_result"),
        "tier0": {"gates": [{"gate": g["gate"], "count": g["count"], "rate": g["rate"]} for g in _need(card, "tier0", "gates")],
                  "all_zero": _need(card, "tier0", "all_zero"), "severity_counts": dict(_need(card, "tier0", "severity_counts"))},
        "tier1": {"reportable": _need(card, "tier1", "reportable"), "correct_terminal_handling": _need(card, "tier1", "correct_terminal_handling"),
                  "by_disposition": dict(_need(card, "tier1", "by_disposition")),
                  "diagnostics": {k: (_pass_k(diagnostics[k]) if k == "pass_k" else diagnostics[k]) for k in DIAGNOSTIC_RATES}},
        "tier2": dict(_need(card, "tier2")),
        "tier3": {k: _need(card, "tier3", k) for k in ("cost_usd_micros_total", "cost_usd_micros_per_correct_outcome", "latency", "latency_by_path")},
        "by_family": dict(_need(card, "by_family")), "by_category": dict(_need(card, "by_category")),
        "failure_count": len(_need(card, "failures")),
    }
