"""python -m benchmark.qualification record|check   (UND-13; REQ-INT-006)

`record` makes a qualification record from the scorecard of a qualification-subset run that includes pass^k (exactly five runs) and writes it (sorted keys, two-space
indent, one trailing newline). Exit 0 QUALIFIED, 1 NOT_QUALIFIED (the record is still written), 2 bad input.
`check` says whether a provider, model ID and pinned version has a qualification record. Exit 0 allowed, 1 refused, 2 unreadable input.
Generated layer only; refuses any argument naming `benchmark-heldout`."""
import argparse
import json
import sys
from pathlib import Path

from benchmark.contracts import load_strict, pretty_bytes
from benchmark.qualification.record import KINDS, build_record, check_model, validate_record

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
    args = parser.parse_args(argv)
    try:
        return _record(args) if args.command == "record" else _check(args)
    except (ValueError, KeyError, OSError, json.JSONDecodeError) as exc:
        print(f"input error: {exc}")
        return 2


if __name__ == "__main__":
    sys.exit(main())
