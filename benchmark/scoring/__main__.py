"""Score recorded result files: python -m benchmark.scoring --cases DIR --oracles DIR --results PATH --pack FILE --manifest FILE --run-id ID --out DIR

Reads the generated layer only. It refuses any argument or environment value that mentions `benchmark-heldout`
(INV-22, REQ-AGT-004). Exit codes: 0 scored, complete, Tier 0 all zero; 1 scored, but the run is incomplete or Tier 0 is
not zero; 2 an input could not be read or is invalid (nothing is written).
"""
import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, StrictJsonError, load_strict, loads_strict, pretty_bytes
from benchmark.scoring.registry import registry_from_pack
from benchmark.scoring.report import ReportError, render_markdown
from benchmark.scoring.scorecard import score

Json = dict[str, Any]
FORBIDDEN = "benchmark-heldout"


class InputError(Exception):
    pass


def _documents(path: Path) -> list[tuple[str, Any]]:
    """(name, text) pairs: every *.json under a directory, or every line of a .jsonl-style file."""
    if path.is_dir():
        return [(p.relative_to(path).as_posix(), p.read_text(encoding="utf-8")) for p in sorted(path.rglob("*.json"))]
    if path.is_file():
        lines = [line for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
        return [(f"{path.name}:{i + 1}", line) for i, line in enumerate(lines)]
    raise InputError(f"{path} does not exist")


def _parse(name: str, text: str) -> Any:
    try:
        return loads_strict(text)
    except (ValueError, StrictJsonError) as exc:
        raise ValueError(f"{name}: {exc}") from exc


def _load_cases(path: Path, contracts: Contracts) -> tuple[list[Json], list[str]]:
    good: list[Json] = []
    invalid: list[str] = []
    for name, text in _documents(path):
        try:
            document = _parse(name, text)
        except ValueError:
            invalid.append(name)
            continue
        if contracts.errors("benchmark-case", document):
            invalid.append(name)
        else:
            good.append(document)
    return good, invalid


def _load_oracles(path: Path, contracts: Contracts) -> list[Json]:
    found: list[Json] = []
    for name, text in _documents(path):
        document = _parse(name, text)
        errors = contracts.errors("benchmark-oracle", document)
        if errors:
            raise InputError(f"oracle {name} is invalid: {errors[0]}")
        found.append(document)
    return found


def _error_result(document: Any, name: str, message: str) -> Json | None:
    if not isinstance(document, dict) or not isinstance(document.get("case_id"), str):
        return None
    version = document.get("case_version")
    run_id = document.get("run_id")
    return {
        "run_id": run_id if isinstance(run_id, str) and run_id else "run_unknown",
        "case_id": document["case_id"],
        "case_version": version if isinstance(version, int) and version >= 1 else 1,
        "status": "ERROR",
        "error": {"code": "RESULT_INVALID_SCHEMA", "message": f"{name}: {message}"[:300]},
        "observed": None,
        "environment": None,
        "path": "UNKNOWN",
        "latency_ms": 0,
        "cost_usd_micros": 0,
        "outcome_digest": None,
    }


def _load_results(path: Path, contracts: Contracts) -> list[Json]:
    found: list[Json] = []
    for name, text in _documents(path):
        document = _parse(name, text)
        errors = contracts.errors("benchmark-case-result", document)
        if not errors:
            found.append(document)
            continue
        replacement = _error_result(document, name, errors[0])
        if replacement is None:
            raise InputError(f"result {name} has no case_id and cannot be attributed: {errors[0]}")
        found.append(replacement)
    return found


def _refuse_heldout(argv: list[str]) -> list[str]:
    return [a for a in argv if FORBIDDEN in a] + [f"${k}" for k, v in os.environ.items() if FORBIDDEN in v]


def run(args: argparse.Namespace) -> int:
    contracts = Contracts()
    cases, invalid_cases = _load_cases(Path(args.cases), contracts)
    oracles = _load_oracles(Path(args.oracles), contracts)
    results = _load_results(Path(args.results), contracts)
    replay = _load_results(Path(args.replay_results), contracts) if args.replay_results else None
    registry = registry_from_pack(load_strict(Path(args.pack)))
    manifest = load_strict(Path(args.manifest))
    manifest_errors = contracts.errors("benchmark-suite-manifest", manifest)
    if manifest_errors:
        raise InputError(f"the manifest is invalid: {manifest_errors[0]}")
    structural: dict[str, int] = load_strict(Path(args.structural_evidence)) if args.structural_evidence else {}

    # A case that failed its schema is excluded from the run and named; the manifest still lists it.
    known = {c["case_id"] for c in cases}
    oracles = [o for o in oracles if o["case_id"] in known]
    card = score(cases, oracles, results, registry, run_id=args.run_id, structural_evidence=structural,
                 replay_results=replay, invalid_cases=invalid_cases)
    manifest_for_report = dict(manifest)
    if invalid_cases:
        # The manifest lists the invalid cases too; only the scored ones are in the scorecard.
        scored = [c for c in manifest["cases"] if c["case_id"] in known]
        manifest_for_report = {**manifest, "cases": scored, "layer_counts": card["layers"]}
    report = render_markdown(card, manifest_for_report)

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    (out / "scorecard.json").write_bytes(pretty_bytes(card))
    (out / "report.md").write_text(report, encoding="utf-8")
    bad = [g["gate"] for g in card["tier0"]["gates"] if g["count"]]
    print("Tier 0: " + ("all 11 gates at zero" if not bad else f"NOT at zero: {', '.join(bad)}"))
    print(f"Cases: {card['n_cases']}; complete: {card['complete']}; correct terminal handling: "
          f"{card['tier1']['correct_terminal_handling']['k']} of {card['tier1']['correct_terminal_handling']['n']}")
    print(f"Wrote {out / 'scorecard.json'} and {out / 'report.md'}")
    return 0 if card["complete"] and card["tier0"]["all_zero"] else 1


def main(argv: list[str] | None = None) -> int:
    argv = sys.argv[1:] if argv is None else argv
    refused = _refuse_heldout(argv)
    if refused:
        print(f"refused: {FORBIDDEN} is referenced by {refused}")
        return 2
    parser = argparse.ArgumentParser(prog="python -m benchmark.scoring", description=__doc__)
    for flag in ("--cases", "--oracles", "--results", "--pack", "--manifest", "--run-id", "--out"):
        parser.add_argument(flag, required=True)
    parser.add_argument("--replay-results")
    parser.add_argument("--structural-evidence", help="JSON file: gate name to a count from static scans and negative tests")
    args = parser.parse_args(argv)
    try:
        return run(args)
    except (InputError, ReportError, ValueError, KeyError, OSError, json.JSONDecodeError) as exc:
        print(f"input error: {exc}")
        return 2


if __name__ == "__main__":
    sys.exit(main())
