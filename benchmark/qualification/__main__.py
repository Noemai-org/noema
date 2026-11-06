"""python -m benchmark.qualification record|check   (UND-13; REQ-INT-006)

`record` makes a qualification record from the scorecard of a qualification-subset run that includes pass^k (exactly five runs) and from the drift run record, and writes it (sorted keys, two-space
indent, one trailing newline). Exit 0 QUALIFIED, 1 NOT_QUALIFIED (the record is still written), 2 bad input.
`check` says whether a provider, model ID and pinned version has a qualification record. Exit 0 allowed, 1 refused, 2 unreadable input.
`subset` selects the qualification subset from a generated cases file; `baseline` pins the qualified version's answers; `compare` writes the run record of a new version against
the baseline. Exit 0 PASS, 1 DRIFT or PROVIDER_UNAVAILABLE (the record is still written), 2 bad input.
Generated layer only; refuses any argument naming `benchmark-heldout`."""
import argparse
import json
import sys
from pathlib import Path

from benchmark.contracts import load_strict, pretty_bytes
from benchmark.qualification.pins import KINDS
from benchmark.qualification.record import build_record, check_model, validate_record
from benchmark.qualification.run import build_baseline, compare, select_subset, validate_run_record

FORBIDDEN = "benchmark-heldout"


def _baseline_pass_k(path: str | None, drift: dict[str, object], args: argparse.Namespace) -> float | None:
    """The pass^k of the pinned baseline's own QUALIFIED record, which must be the record of the version the run record was compared against."""
    if path is None:
        return None
    baseline = load_strict(Path(path))
    problems = validate_record(baseline)
    if problems:
        raise ValueError(f"the baseline record is invalid: {'; '.join(problems)}")
    pinned = drift["baseline"]["pinned"]  # type: ignore[index]
    if (baseline["kind"], baseline["provider"], baseline["model_id"], baseline["version"]) != (pinned["kind"], pinned["provider"], pinned["model_id"], pinned["version"]):
        raise ValueError(f"the baseline record is for {baseline['provider']}/{baseline['model_id']}@{baseline['version']}, not the run's baseline {pinned['provider']}/{pinned['model_id']}@{pinned['version']}")
    if baseline["verdict"] != "QUALIFIED":
        raise ValueError("the baseline record is NOT_QUALIFIED: only a qualified version can be the baseline")
    rate = (baseline["pass_k"].get("rate") or {}).get("rate")
    if rate is None:
        raise ValueError("the baseline record has no pass^k rate")
    return float(rate)


def _record(args: argparse.Namespace) -> int:
    card = load_strict(Path(args.scorecard))
    drift = load_strict(Path(args.run))
    record = build_record(card, drift, kind=args.kind, provider=args.provider, model_id=args.model_id, version=args.version, decision_record=args.decision_record,
                          qualified_on=args.qualified_on, baseline_pass_k=_baseline_pass_k(args.baseline_record, drift, args), floor=args.floor, floor_source=args.floor_source)
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_bytes(pretty_bytes(record))
    print(f"{record['verdict']}: {args.provider}/{args.model_id}@{args.version} from run {record['run_id']}; wrote {out}")
    for reason in record["reasons"]:
        print(f"  {reason}")
    return 0 if record["verdict"] == "QUALIFIED" else 1


def _load_records(directory: Path) -> list[dict[str, object]]:
    if not directory.is_dir():
        raise OSError(f"{directory} is not a directory")
    records = []
    for path in sorted(directory.glob("*.json")):
        document = load_strict(path)
        if not isinstance(document, dict):
            raise ValueError(f"{path.name} is not a JSON object")
        problems = validate_record(document)
        if problems:
            print(f"warning: {path.name}: {'; '.join(problems)}")
        records.append(document)
    return records


def _check(args: argparse.Namespace) -> int:
    problems = check_model(_load_records(Path(args.records)), args.provider, args.model_id, args.version, args.kind)
    for problem in problems:
        print(f"refused: {problem}")
    if not problems:
        print(f"allowed: {args.provider}/{args.model_id}@{args.version} has a QUALIFIED record")
    return 1 if problems else 0


def _write(path: str, document: object) -> Path:
    out = Path(path)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_bytes(pretty_bytes(document))
    return out


def _pin(args: argparse.Namespace) -> dict[str, str]:
    return {"kind": args.kind, "provider": args.provider, "model_id": args.model_id, "version": args.version}


def _subset(args: argparse.Namespace) -> int:
    loaded = load_strict(Path(args.cases))
    cases = loaded["cases"] if isinstance(loaded, dict) else loaded
    subset = select_subset(cases, subset_id=args.subset_id)
    print(f"subset {subset['subset_id']}: {subset['n_cases']} GENERATED understanding-dependent cases; wrote {_write(args.out, subset)}")
    return 0


def _baseline(args: argparse.Namespace) -> int:
    baseline = build_baseline(load_strict(Path(args.subset)), _pin(args), load_strict(Path(args.answers)), commit_sha=args.commit_sha)
    print(f"baseline for {args.provider}/{args.model_id}@{args.version} on {baseline['subset_id']}; wrote {_write(args.out, baseline)}")
    return 0


def _compare(args: argparse.Namespace) -> int:
    record = compare(load_strict(Path(args.subset)), load_strict(Path(args.baseline)), _pin(args), load_strict(Path(args.answers)), commit_sha=args.commit_sha, recorded_on=args.recorded_on)
    problems = validate_run_record(record)
    if problems:
        raise ValueError("; ".join(problems))
    drifted = [c for c in record["cases"] if c["status"] != "MATCH"]
    print(f"{record['result']}: {args.provider}/{args.model_id}@{args.version} against the baseline of {record['baseline']['pinned']['version']}; wrote {_write(args.out, record)}")
    for case in drifted:
        slots = ", ".join(d["slot"] for d in case["differing_slots"]) or ("a different version answered" if case["version_mismatch"] else "no answer")
        print(f"  {case['case_id']}: {case['status']} ({slots})")
    return 0 if record["result"] == "PASS" else 1


def main(argv: list[str] | None = None) -> int:
    argv = sys.argv[1:] if argv is None else argv
    named = [a for a in argv if FORBIDDEN in a]
    if named:
        print(f"refused: {FORBIDDEN} is referenced by {named}")
        return 2
    parser = argparse.ArgumentParser(prog="python -m benchmark.qualification", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    rec = sub.add_parser("record")
    for flag in ("--scorecard", "--run", "--provider", "--model-id", "--version", "--decision-record", "--qualified-on", "--out"):
        rec.add_argument(flag, required=True)
    rec.add_argument("--kind", required=True, choices=KINDS)
    rec.add_argument("--baseline-record", help="the qualification record of the pinned baseline version; its pass^k is the bar")
    rec.add_argument("--floor", type=float, help="a Founder-set pass^k floor (pack data); needs --floor-source")
    rec.add_argument("--floor-source", help="the decision record that set the floor")
    chk = sub.add_parser("check")
    for flag in ("--records", "--provider", "--model-id", "--version"):
        chk.add_argument(flag, required=True)
    chk.add_argument("--kind", choices=KINDS)
    sub_cmd = sub.add_parser("subset")
    for flag in ("--cases", "--subset-id", "--out"):
        sub_cmd.add_argument(flag, required=True)
    for name, flags in (("baseline", ("--subset", "--answers", "--commit-sha", "--out")), ("compare", ("--subset", "--baseline", "--answers", "--commit-sha", "--recorded-on", "--out"))):
        cmd = sub.add_parser(name)
        for flag in (*flags, "--provider", "--model-id", "--version"):
            cmd.add_argument(flag, required=True)
        cmd.add_argument("--kind", required=True, choices=KINDS)
    args = parser.parse_args(argv)
    commands = {"record": _record, "check": _check, "subset": _subset, "baseline": _baseline, "compare": _compare}
    try:
        return commands[args.command](args)
    except (ValueError, KeyError, OSError, json.JSONDecodeError) as exc:
        print(f"input error: {exc}")
        return 2


if __name__ == "__main__":
    sys.exit(main())
