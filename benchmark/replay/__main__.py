"""python -m benchmark.replay --suite DIR --first RUN_DIR --second RUN_DIR [--story US-01 ...] --out DIR   (BEN-05)

Diffs the Trust decisions and verdicts of two runs of the same cases. Each RUN_DIR is a runner output (`run-manifest.json` and `results/`). Reads the generated
layer only and refuses any argument or environment value that names `benchmark-heldout` (INV-22, REQ-AGT-004).

Exit codes: 0 IDENTICAL; 1 DIFFERENT or NOT_COMPARABLE (the report is written); 2 an input is unusable or the two runs cannot be compared (nothing is written);
3 a run is NOT_RUN, no access (nothing is written, nothing is faked)."""
import argparse
import os
import sys
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, StrictJsonError, load_strict, pretty_bytes
from benchmark.replay.compare import ReplayInputError, compare
from benchmark.replay.report import render_markdown

Json = dict[str, Any]
FORBIDDEN = "benchmark-heldout"
DEFAULT_STORIES = ("US-01", "US-06")
REGISTRY_PATH = Path(__file__).parent / "open_exclusions.json"


def _load(path: Path, schema: str, contracts: Contracts) -> Json:
    try:
        document = load_strict(path)
    except (OSError, ValueError, StrictJsonError) as exc:
        raise ReplayInputError(f"{path}: {exc}") from exc
    problems = contracts.errors(schema, document)
    if problems:
        raise ReplayInputError(f"{path} breaks its contract ({schema}): {problems[0]}")
    return document  # type: ignore[no-any-return]


def _load_run(run_dir: Path, contracts: Contracts) -> tuple[Json, dict[str, Json]]:
    manifest = _load(run_dir / "run-manifest.json", "benchmark-run-manifest", contracts)
    results_dir = run_dir / "results"
    if manifest["status"] != "NOT_RUN" and not results_dir.is_dir():
        raise ReplayInputError(f"{results_dir} does not exist")
    results: dict[str, Json] = {}
    for path in sorted(results_dir.glob("*.json")) if results_dir.is_dir() else []:
        result = _load(path, "benchmark-case-result.v2", contracts)
        results[result["case_id"]] = result
    return manifest, results


def _registry(path: Path) -> dict[str, Json]:
    try:
        document = load_strict(path)
    except (OSError, ValueError, StrictJsonError) as exc:
        raise ReplayInputError(f"{path}: {exc}") from exc
    entries = document.get("exclusions") if isinstance(document, dict) else None
    if not isinstance(entries, dict) or not entries:
        raise ReplayInputError(f"{path} has no 'exclusions' object naming at least one case")
    for case_id, entry in entries.items():
        if not isinstance(entry, dict) or not all(isinstance(entry.get(k), str) and entry[k] for k in ("reason", "held_by", "opened")):
            raise ReplayInputError(f"{path}: the exclusion {case_id} needs a reason, held_by and opened")
    return dict(entries)


def _refused(argv: list[str]) -> list[str]:
    return [a for a in argv if FORBIDDEN in a] + [f"${k}" for k, v in os.environ.items() if FORBIDDEN in v]


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    parser = argparse.ArgumentParser(prog="benchmark.replay", description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--suite", required=True, help="a suite root written by `python -m benchmark.generator generate`")
    parser.add_argument("--first", required=True, help="the first run's output directory")
    parser.add_argument("--second", required=True, help="the second run's output directory")
    parser.add_argument("--story", action="append", help="a user story to compare, repeatable (default: US-01 and US-06)")
    parser.add_argument("--all", action="store_true", help="compare every case of the suite, not the cases of two stories (BEN-18); cannot be combined with --story")
    parser.add_argument("--exclude-mutated", action="store_true",
                        help="leave out only the cases named in the open-exclusions registry; no other case can be dropped, and the report lists each one")
    parser.add_argument("--open-exclusions", default=str(REGISTRY_PATH), help="the registry of named open exclusions (default: the reviewed file beside this module)")
    parser.add_argument("--out", required=True)
    args = parser.parse_args(argv)
    named = _refused(argv)
    if named:
        print(f"refused: {', '.join(named)} names the held-out set, which this tool never reads (INV-22, REQ-AGT-004)", file=sys.stderr)
        return 2
    contracts = Contracts()
    try:
        suite = _load(Path(args.suite) / "manifest.json", "benchmark-suite-manifest", contracts)
        first_manifest, first_results = _load_run(Path(args.first), contracts)
        second_manifest, second_results = _load_run(Path(args.second), contracts)
        not_run = [m for m in (first_manifest, second_manifest) if m["status"] == "NOT_RUN"]
        if not_run:
            for m in not_run:
                print(f"not run, no access: run {m['run_id']}: {m['not_run_reason']}")
            return 3
        report = compare(suite, first_manifest, first_results, second_manifest, second_results, tuple(args.story or (() if args.all else DEFAULT_STORIES)),
                         open_exclusions=_registry(Path(args.open_exclusions)) if args.exclude_mutated else None, all_cases=args.all)
    except ReplayInputError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    (out / "replay.json").write_bytes(pretty_bytes(report))
    (out / "replay.md").write_text(render_markdown(report), encoding="utf-8")
    counts = report["counts"]
    print(f"{report['verdict']}: {counts['identical']} identical, {counts['different']} different, {counts['not_comparable']} not comparable, of {counts['selected']} selected "
          f"(runs {report['first']['run_id']} and {report['second']['run_id']}, seed {report['seed']}, commit {report['commit_sha']})")
    return 0 if report["verdict"] == "IDENTICAL" else 1


if __name__ == "__main__":
    sys.exit(main())
