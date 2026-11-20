"""The runner's stamped artifacts for the G1 package (A444, rulings of 20 Nov; `benchmark/suites/g1_package.py` reads them).

`python -m benchmark.runner.package_artifacts --run DIR --sha SHA --run-id ID [--probes FILE]` writes, into the run directory:
- `mock-case-writes.json`: `{"stamp": {"sha", "run_id"}, "writes": {case id: integer}}`, the per-case write counts (aggregates only). A count is the sum of the `count` of every effect in the case result's
  `environment.effects`, which the oracle client read from the mock-case truth listener with the runner's own credential (`benchmark/oracle_client/effects.py`); this module reads the results the runner
  already wrote and no new source. It fails closed: a case whose result has no environment block, a malformed effect or a count that is not a whole number of at least zero, or that has two results, is
  **not counted** (no entry, never a zero), and the exit code is 1;
- `stamp.json`: `{"sha", "run_id"}`, the stamp of the run directory (it holds `correlations.json` and the run's other files);
wraps the runner's `reconstructions.json` as `{"stamp": {...}, "cases": {...}}` (it must exist: a full run always writes it), and, with `--probes`, adds the same stamp to `negative-probes.json` (a stamp already there for another dispatch or run is never overwritten).
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


def _write(path: Path, doc: Json) -> None:
    path.write_bytes(json.dumps(doc, indent=2, sort_keys=True).encode("utf-8") + b"\n")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.runner.package_artifacts", description=__doc__)
    p.add_argument("--run", type=Path, required=True)
    p.add_argument("--sha", required=True)
    p.add_argument("--run-id", required=True)
    p.add_argument("--probes", type=Path)
    args = p.parse_args(argv)
    try:
        _check(args.sha, args.run_id)
        counts, uncounted = scan(args.run)
        wrapped = _stamped_reconstructions(args.run, args.sha, args.run_id)  # read and checked before anything is written
        stamped = stamp_doc("writes", counts, args.sha, args.run_id)
        _write(args.run / "stamp.json", stamped["stamp"])
        if wrapped is not None:
            _write(args.run / "reconstructions.json", wrapped)
        _write(args.run / "mock-case-writes.json", stamped)
        if args.probes is not None:
            stamp_probes(args.probes, args.sha, args.run_id)
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
