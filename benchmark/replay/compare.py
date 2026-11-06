"""The replay diff for BEN-05 (REQ-NFR-001, REQ-BEN-002, INV-21): did two runs of the same cases through the public API reach the same Trust decisions and verdicts?

A pure function of two run manifests, two sets of recorded case results and the suite manifest: no I/O, no clock, no threshold. It reports what the recorded
results carry and nothing more. The case result holds the Trust disposition, the dispatches with their grant state, the verdicts and the environment effects; it
does not hold the full Record (see LIMITATIONS). An errored case, a missing case or a story with no comparable case is NOT_COMPARABLE, never identical: two
runs that fail the same way prove nothing about a decision. Generated layer only (INV-22, REQ-AGT-004). SYNTHETIC."""
import json
from collections.abc import Mapping
from typing import Any

Json = dict[str, Any]

OBSERVED_ORDERED = ("disposition", "classification", "resolved_target", "clarification_asked", "escalation_opened", "audit_events")
OBSERVED_UNORDERED = ("dispatches", "verdicts", "claims", "disclosed_refs")
ENVIRONMENT_COUNTERS = ("oracle_accesses_by_runtime", "model_governance_changes", "monetary_calls")
COMPARED_FIELDS = (*OBSERVED_ORDERED, *OBSERVED_UNORDERED, "effects", *ENVIRONMENT_COUNTERS, "outcome_digest", "turns")
NOT_COMPARED = ("latency_ms", "cost_usd_micros", "run_id")
LIMITATIONS = (
    "The diff covers the fields of the recorded case result. The result holds the Trust disposition, the dispatches with their grant state, the verdicts, the claims, "
    "the audit event types and the environment effects. It does not hold the Trust decision payload or the Record events themselves.",
    "Two identical results are evidence that this build behaved the same on these cases, on this commit, with this seed. They are not a statement about other cases.",
)


class ReplayInputError(Exception):
    """An input that cannot be compared. The tool exits 2 and writes nothing."""


def _canonical(value: Any) -> str:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def _unordered(value: list[Any]) -> list[Any]:
    return sorted(value, key=_canonical)


def _view(result: Json) -> Json:
    observed, environment = result["observed"], result["environment"]
    view: Json = {name: observed[name] for name in OBSERVED_ORDERED}
    view.update({name: _unordered(observed[name]) for name in OBSERVED_UNORDERED})
    view["effects"] = _unordered(environment["effects"])
    view.update({name: environment[name] for name in ENVIRONMENT_COUNTERS})
    view["outcome_digest"] = result["outcome_digest"]
    view["turns"] = result["turns"]
    return view


def _require_same(label: str, first: Any, second: Any) -> None:
    if first != second:
        raise ReplayInputError(f"the two runs differ in {label} ({first!r} and {second!r}); a replay compares one suite, one seed and one commit")


def _check_runs(first: Json, second: Json) -> None:
    for manifest in (first, second):
        if manifest["status"] == "NOT_RUN":
            raise ReplayInputError(f"run {manifest['run_id']} is NOT_RUN ({manifest.get('not_run_reason')}); nothing ran, so there is nothing to compare")
        if manifest["commit_sha"] is None:
            raise ReplayInputError(f"run {manifest['run_id']} records no commit; evidence must name the code it ran")
    if first["run_id"] == second["run_id"]:
        raise ReplayInputError(f"both runs have the run_id {first['run_id']}; a replay needs two separate runs")
    _require_same("commit", first["commit_sha"], second["commit_sha"])
    _require_same("suite", first["suite"]["manifest_sha256"], second["suite"]["manifest_sha256"])
    _require_same("generator version", first["suite"]["generator"]["version"], second["suite"]["generator"]["version"])
    _require_same("seed", first["suite"]["generator"]["seed"], second["suite"]["generator"]["seed"])
    _require_same("pack version", first["pack_version"], second["pack_version"])
    _require_same("policy version", first["policy_version"], second["policy_version"])


def _check_results(run_id: str, results: dict[str, Json]) -> None:
    for case_id, result in results.items():
        if result["run_id"] != run_id or result["case_id"] != case_id:
            raise ReplayInputError(f"the result for {case_id} says run {result['run_id']} and case {result['case_id']}; it was expected to belong to run {run_id}")


def _compare_case(case_id: str, first: Json | None, second: Json | None) -> Json:
    if first is None or second is None:
        missing = "the first run" if first is None else "the second run"
        return {"case_id": case_id, "result": "NOT_COMPARABLE", "reason": f"no result in {missing}", "differences": []}
    errored = [(name, r) for name, r in (("first", first), ("second", second)) if r["status"] != "COMPLETED"]
    if errored:
        reason = "; ".join(f"{name} run ERROR {r['error']['code'] if r['error'] else 'unknown'}" for name, r in errored)
        return {"case_id": case_id, "result": "NOT_COMPARABLE", "reason": reason, "differences": []}
    a, b = _view(first), _view(second)
    differences = [{"field": f, "first": a[f], "second": b[f]} for f in COMPARED_FIELDS if a[f] != b[f]]
    return {"case_id": case_id, "result": "DIFFERENT" if differences else "IDENTICAL", "reason": None, "differences": differences}


def compare(suite_manifest: Json, first_manifest: Json, first_results: dict[str, Json], second_manifest: Json, second_results: dict[str, Json],
            stories: tuple[str, ...], *, open_exclusions: Mapping[str, Json] | None = None, all_cases: bool = False) -> Json:
    if all_cases and stories:
        raise ReplayInputError("--all compares every case of the suite; it cannot be combined with --story")
    if not all_cases and not stories:
        raise ReplayInputError("name the user stories to compare (stories is empty)")
    _check_runs(first_manifest, second_manifest)
    _check_results(first_manifest["run_id"], first_results)
    _check_results(second_manifest["run_id"], second_results)
    listed = suite_manifest["cases"]
    unknown = [] if all_cases else [s for s in stories if not any(s in entry["covers"] for entry in listed)]
    if unknown:
        raise ReplayInputError(f"the suite has no case covering {', '.join(unknown)}")
    named = dict(open_exclusions or {})
    absent = sorted(case_id for case_id in named if not any(entry["case_id"] == case_id for entry in listed))
    if absent:
        raise ReplayInputError(f"the open-exclusions registry names {', '.join(absent)}, which the suite does not contain")
    covering = list(listed) if all_cases else [entry for entry in listed if any(s in entry["covers"] for s in stories)]

    def row_for(entry: Json) -> Json:
        row = _compare_case(entry["case_id"], first_results.get(entry["case_id"]), second_results.get(entry["case_id"]))
        row["covers"] = sorted(s for s in stories if s in entry["covers"])
        row["mutation"] = entry["mutation"]["name"] if entry["mutation"] else None
        return row

    # A named open exclusion is left out of the tally only while it cannot be compared. Every named case is probed: one that can run is compared and
    # counted (it cannot hide a difference, and it closes itself), one that cannot stays an open exclusion that records what was observed (Codex, noema#102).
    rows: list[Json] = []
    exclusions: list[Json] = []
    for entry in covering:
        row = row_for(entry)
        if entry["case_id"] in named and row["result"] == "NOT_COMPARABLE":
            exclusions.append({"case_id": entry["case_id"], "status": "OPEN", "mutation": row["mutation"],
                               "faults": sorted(c for c in entry["covers"] if c.startswith("FAULT:") and c != "FAULT:NONE"), "covers": row["covers"],
                               "observed": {"result": row["result"], "reason": row["reason"]}, **named[entry["case_id"]]})
        else:
            rows.append(row)
    tally = {"IDENTICAL": 0, "DIFFERENT": 0, "NOT_COMPARABLE": 0}
    for row in rows:
        tally[row["result"]] += 1
    counts = {"selected": len(rows), "compared": tally["IDENTICAL"] + tally["DIFFERENT"], "identical": tally["IDENTICAL"], "different": tally["DIFFERENT"],
              "not_comparable": tally["NOT_COMPARABLE"]}
    covered = {s: sum(1 for r in rows if s in r["covers"] and r["result"] != "NOT_COMPARABLE") for s in stories}
    if counts["different"]:
        verdict = "DIFFERENT"
    elif counts["not_comparable"] or not all(covered.values()):
        verdict = "NOT_COMPARABLE"
    else:
        verdict = "IDENTICAL"
    generator = first_manifest["suite"]["generator"]
    return {
        "verdict": verdict, "stories": list(stories), "selection": "ALL_CASES_OF_THE_SUITE" if all_cases and not exclusions else ("NAMED_OPEN_EXCLUSIONS_REMOVED" if exclusions else "ALL_CASES_COVERING_THE_STORIES"),
        "open_exclusions": exclusions, "case_coverage": {"covered": len(rows), "of": len(covering)}, "full_coverage": not exclusions, "stories_covered": covered, "counts": counts,
        "first": {"run_id": first_manifest["run_id"], "status": first_manifest["status"]},
        "second": {"run_id": second_manifest["run_id"], "status": second_manifest["status"]},
        "seed": generator["seed"], "generator": {"name": generator["name"], "version": generator["version"]},
        "commit_sha": first_manifest["commit_sha"], "suite_manifest_sha256": first_manifest["suite"]["manifest_sha256"],
        "pack_version": first_manifest["pack_version"], "policy_version": first_manifest["policy_version"],
        "compared_fields": list(COMPARED_FIELDS), "not_compared": list(NOT_COMPARED), "limitations": list(LIMITATIONS), "cases": rows,
    }
