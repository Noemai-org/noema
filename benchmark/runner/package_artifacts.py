"""The runner's stamped artifacts for the G1 package (A444, rulings of 20 Nov; `benchmark/suites/g1_package.py` reads them).

`python -m benchmark.runner.package_artifacts --run DIR --sha SHA --run-id ID [--probes FILE]` writes, into the run directory:
- `mock-case-writes.json`: `{"stamp": {"sha", "run_id"}, "writes": {case id: integer}}`, the per-case write counts (aggregates only). A count is the sum of the `count` of every effect in the case result's
  `environment.effects`, which the oracle client read from the mock-case truth listener with the runner's own credential (`benchmark/oracle_client/effects.py`); this module reads the results the runner
  already wrote and no new source. It fails closed: a case whose result has no environment block, a malformed effect or a count that is not a whole number of at least zero, or that has two results, is
  **not counted** (no entry, never a zero), and the exit code is 1;
- `stamp.json`: `{"sha", "run_id"}`, the stamp of the run directory (it holds `correlations.json` and the run's other files);
wraps the runner's `reconstructions.json` as `{"stamp": {...}, "cases": {...}}` (it must exist: a full run always writes it), and, with `--probes`, adds the same stamp to `negative-probes.json` and appends the probes' own reads of the reconstruction route to `requests.jsonl` (the control read as a `reconstruction` request, the three refusal checks as `probe` requests; running it twice adds nothing) (a stamp already there for another dispatch or run is never overwritten).
Exit 0 every case counted, 1 a case uncounted (the rest is still written), 2 an input is unusable (nothing is written). It reads no oracle label and no expectation. SYNTHETIC."""
import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

Json = dict[str, Any]
SHA = re.compile(r"[0-9a-f]{40}")
RUN_ID = re.compile(r"[A-Za-z0-9._-]{1,64}")


class StampError(ValueError):
    pass


def _check(sha: str, run_id: str) -> None:
    if not SHA.fullmatch(sha):
        raise StampError("the dispatch SHA must be a full lowercase 40-hex SHA")
    if not RUN_ID.fullmatch(run_id):
        raise StampError("the run id may hold only letters, digits, dot, dash and underscore, 1 to 64 characters")


def stamp_doc(key: str, content: Any, sha: str, run_id: str) -> Json:
    _check(sha, run_id)
    return {"stamp": {"sha": sha, "run_id": run_id}, key: content}


def _effects_total(result: Json) -> int | None:
    env = result.get("environment")
    effects = env.get("effects") if isinstance(env, dict) else None
    if not isinstance(effects, list):
        return None
    total = 0
    for effect in effects:
        count = effect.get("count") if isinstance(effect, dict) else None
        if type(count) is not int or count < 0:
            return None
        total += count
    return total


def scan(run: Path) -> tuple[dict[str, int], list[str]]:
    """(counted case id to total writes, case ids that could not be counted), from `run/results/*.json`."""
    files = sorted((run / "results").glob("*.json")) if (run / "results").is_dir() else []
    if not files:
        raise ValueError(f"{run} has no results: an empty run is not a run with zero writes")
    by_case: dict[str, list[Json]] = {}
    for path in files:
        try:
            doc = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as exc:
            raise ValueError(f"{path.name} cannot be read: {exc.__class__.__name__}") from None
        if not isinstance(doc, dict) or not isinstance(doc.get("case_id"), str) or not doc["case_id"]:
            raise ValueError(f"{path.name} has no case id and cannot be attributed")
        by_case.setdefault(doc["case_id"], []).append(doc)
    counted: dict[str, int] = {}
    uncounted: list[str] = []
    for case_id in sorted(by_case):
        total = _effects_total(by_case[case_id][0]) if len(by_case[case_id]) == 1 else None
        if total is None:
            uncounted.append(case_id)
        else:
            counted[case_id] = total
    return counted, uncounted


def write_counts(run: Path) -> dict[str, int]:
    return scan(run)[0]


def stamp_probes(path: Path, sha: str, run_id: str) -> None:
    _check(sha, run_id)
    try:
        doc = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise ValueError(f"{path.name} cannot be read: {exc.__class__.__name__}") from None
    if not isinstance(doc, dict):
        raise ValueError(f"{path.name} is not a JSON object")
    stamp = {"sha": sha, "run_id": run_id}
    if "stamp" in doc and doc["stamp"] != stamp:
        raise StampError(f"{path.name} already carries a stamp for another dispatch or run; it is never overwritten")
    doc["stamp"] = stamp
    path.write_bytes(json.dumps(doc, indent=2, sort_keys=True).encode("utf-8") + b"\n")


def _stamped_reconstructions(run: Path, sha: str, run_id: str) -> Json | None:
    """The runner's flat `reconstructions.json` ({case: {"initial": body}}) as `{"stamp": {...}, "cases": {...}}`; None when it already carries exactly this stamp (nothing to write)."""
    _check(sha, run_id)
    path = run / "reconstructions.json"
    try:
        doc = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise ValueError(f"{path.name} cannot be read: {exc.__class__.__name__}") from None
    if not isinstance(doc, dict):
        raise ValueError(f"{path.name} is not a JSON object")
    stamp = {"sha": sha, "run_id": run_id}
    if "stamp" in doc:
        if set(doc) != {"stamp", "cases"} or doc["stamp"] != stamp:
            raise StampError(f"{path.name} is stamped for another dispatch or run, or is not a stamped wrapper; it is never overwritten")
        return None
    return {"stamp": stamp, "cases": doc}


def stamp_reconstructions(run: Path, sha: str, run_id: str) -> None:
    wrapped = _stamped_reconstructions(run, sha, run_id)
    if wrapped is not None:
        _write(run / "reconstructions.json", wrapped)


ROUTE = "/v1/reconstructions/{correlation_id}"
UNKNOWN_PROBE_CORRELATION = "corr_unknown_probe"


def _probe_status(value: Any) -> int | None:
    status = value.get("status") if isinstance(value, dict) and "status" in value else ValueError
    if status is ValueError or not (status is None or type(status) is int):
        raise ValueError("a probe row has no readable status (an integer, or null when no answer came)")
    return status


def _probe_request_lines(run: Path, probes: Path) -> list[Json] | None:
    """The probes' reads of the reconstruction route as request-record lines; None when the record already holds them (running it twice adds nothing). The control read (an allowed role, answered)
    is a `reconstruction` request of the case's own correlation, as the core logs it; the three refusal checks are `probe` requests (the unknown correlation is the fixed placeholder
    `corr_unknown_probe`: the real one is random and outside the run). A read that got no answer adds no line."""
    path = run / "requests.jsonl"
    if not path.is_file():
        raise ValueError(f"{path.name} does not exist: the runner did not write its own request record")
    try:
        existing = [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
        doc = json.loads(probes.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise ValueError(f"the request record or the probe results cannot be read: {exc.__class__.__name__}") from None
    if any(isinstance(x, dict) and x.get("kind") == "probe" for x in existing):
        return None
    rows = doc.get("cases") if isinstance(doc, dict) else None
    if not isinstance(rows, list):
        raise ValueError(f"{probes.name} has no list of cases")
    out: list[Json] = []
    for row in rows:
        if not isinstance(row, dict) or not isinstance(row.get("case_id"), str) or not row["case_id"] or not isinstance(row.get("correlation_id"), str) or not row["correlation_id"]:
            raise ValueError("a probe row has no case id or correlation id")
        for field, kind, correlation in (("control", "reconstruction", row["correlation_id"]), ("wrong_role", "probe", row["correlation_id"]),
                                         ("foreign_tenant", "probe", row["correlation_id"]), ("unknown_correlation", "probe", UNKNOWN_PROBE_CORRELATION)):
            status = _probe_status(row.get(field))
            if status is not None:
                out.append({"case_id": row["case_id"], "correlation_id": correlation, "method": "GET", "path": ROUTE, "status": status, "kind": kind})
    return out


def append_probe_requests(run: Path, probes: Path) -> None:
    lines = _probe_request_lines(run, probes)
    if lines:
        path = run / "requests.jsonl"
        path.write_bytes(path.read_bytes() + "".join(json.dumps(x, sort_keys=True) + "\n" for x in lines).encode("utf-8"))


def _write(path: Path, doc: Json) -> None:
    path.write_bytes(json.dumps(doc, indent=2, sort_keys=True).encode("utf-8") + b"\n")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.runner.package_artifacts", description=__doc__, allow_abbrev=False)
    p.add_argument("--run", type=Path, required=True)
    p.add_argument("--sha", required=True)
    p.add_argument("--run-id", required=True)
    p.add_argument("--probes", type=Path)
    args = p.parse_args(argv)
    try:
        _check(args.sha, args.run_id)
        counts, uncounted = scan(args.run)
        wrapped = _stamped_reconstructions(args.run, args.sha, args.run_id)  # read and checked before anything is written
        probe_lines = _probe_request_lines(args.run, args.probes) if args.probes is not None else None
        stamped = stamp_doc("writes", counts, args.sha, args.run_id)
        _write(args.run / "stamp.json", stamped["stamp"])
        if wrapped is not None:
            _write(args.run / "reconstructions.json", wrapped)
        _write(args.run / "mock-case-writes.json", stamped)
        if args.probes is not None:
            stamp_probes(args.probes, args.sha, args.run_id)
            if probe_lines:
                append_probe_requests(args.run, args.probes)
    except (ValueError, OSError) as exc:
        sys.stderr.write(f"package artifacts: {exc}\n")
        return 2
    if uncounted:
        sys.stdout.write(f"not counted (no environment block, a malformed effect or two results): {', '.join(uncounted)}\n")
        return 1
    sys.stdout.write(f"counted {len(counts)} cases; stamp {args.sha} {args.run_id}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
