"""python -m benchmark.qualification record|check   (UND-13; REQ-INT-006)

`record` makes a qualification record from the scorecard of a qualification-subset run that includes pass^k (exactly five runs) and writes it (sorted keys, two-space
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
from benchmark.qualification.record import KINDS, build_record, check_model, validate_record
from benchmark.qualification.run import build_baseline, compare, select_subset, validate_run_record

FORBIDDEN = "benchmark-heldout"


def _record(args: argparse.Namespace) -> int:
    card = load_strict(Path(args.scorecard))
    record = build_record(card, kind=args.kind, provider=args.provider, model_id=args.model_id, version=args.version, decision_record=args.decision_record,
                          qualified_on=args.qualified_on)
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
    for flag in ("--scorecard", "--provider", "--model-id", "--version", "--decision-record", "--qualified-on", "--out"):
        rec.add_argument(flag, required=True)
    rec.add_argument("--kind", required=True, choices=KINDS)
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
