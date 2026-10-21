"""Decision-model bake-off: python -m benchmark.bakeoff cases | run | report. Generated layer only (INV-22, REQ-AGT-004).

  cases   --seed N --per-cell M --out FILE            write the deterministic synthetic case set
  run     --cases FILE --provider P --run-id ID --runner-region R --out FILE
                                                      run one path (LLM or DECIDE_OPENAI_DECISIONS); NOT_RUN without access
  report  --cases FILE --runs LLM_RUN DECIDE_RUN --out DIR
                                                      comparison.json, report.md and one reliability diagram per task

Exit codes: 0 done; 2 an input is unreadable or invalid (nothing is written). A path that is NOT_RUN is a result, not an error.
Refuses any argument or environment value that names `benchmark-heldout`.
"""
import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any, Mapping

from benchmark.bakeoff.cases import GENERATOR_NAME, TASKS, generate
from benchmark.bakeoff.report import compare, reliability_svg, render_markdown
from benchmark.bakeoff.run import get_decider, run_provider
from benchmark.contracts import Contracts, load_strict, pretty_bytes

Json = dict[str, Any]
FORBIDDEN = "benchmark-heldout"


class InputError(Exception):
    pass


def _load(path: str, schema: str, contracts: Contracts) -> Json:
    try:
        document = load_strict(Path(path))
    except (OSError, ValueError) as exc:
        raise InputError(f"{path}: {exc}") from exc
    errors = contracts.errors(schema, document)
    if errors:
        raise InputError(f"{path} is not a valid {schema}: {errors[0]}")
    return dict(document)


def _load_cases(path: str, contracts: Contracts) -> list[Json]:
    try:
        cases = load_strict(Path(path))
    except (OSError, ValueError) as exc:
        raise InputError(f"{path}: {exc}") from exc
    if not isinstance(cases, list) or not cases:
        raise InputError(f"{path} must hold a non-empty list of cases")
    for case in cases:
        errors = contracts.errors("benchmark-decision-case", case)
        if errors:
            raise InputError(f"{path}: case {case.get('case_id') if isinstance(case, dict) else '?'} is invalid: {errors[0]}")
    return list(cases)


def run(argv: list[str], environ: Mapping[str, str]) -> int:
    parser = argparse.ArgumentParser(prog="python -m benchmark.bakeoff", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    c = sub.add_parser("cases")
    c.add_argument("--seed", type=int, required=True)
    c.add_argument("--per-cell", type=int, default=20)
    c.add_argument("--out", required=True)
    r = sub.add_parser("run")
    r.add_argument("--cases", required=True)
    r.add_argument("--provider", required=True)
    r.add_argument("--run-id", required=True)
    r.add_argument("--runner-region", required=True)
    r.add_argument("--out", required=True)
    p = sub.add_parser("report")
    p.add_argument("--cases", required=True)
    p.add_argument("--runs", nargs=2, required=True)
    p.add_argument("--out", required=True)
    args = parser.parse_args(argv)
    contracts = Contracts()

    if args.command == "cases":
        cases = generate(args.seed, args.per_cell)
        out = Path(args.out)
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_bytes(pretty_bytes(cases))
        print(f"wrote {len(cases)} cases from {GENERATOR_NAME} seed {args.seed} to {out}")
        return 0

    cases = _load_cases(args.cases, contracts)
    if args.command == "run":
        decider = get_decider(args.provider, environ)
        result = run_provider(cases, decider, run_id=args.run_id, runner_region=args.runner_region)
        errors = contracts.errors("benchmark-decision-run", result)
        if errors:
            raise InputError(f"the run is not a valid benchmark-decision-run: {errors[0]}")
        Path(args.out).parent.mkdir(parents=True, exist_ok=True)
        Path(args.out).write_bytes(pretty_bytes(result))
        if result["status"] == "NOT_RUN":
            print(f"{args.provider}: NOT RUN ({result['not_run_reason']})")
        else:
            print(f"{args.provider}: ran {len(result['results'])} calls")
        return 0

    runs = [_load(path, "benchmark-decision-run", contracts) for path in args.runs]
    try:
        comparison = compare(cases, runs)
    except ValueError as exc:
        raise InputError(str(exc)) from exc
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    (out / "comparison.json").write_bytes(pretty_bytes(comparison))
    (out / "report.md").write_text(render_markdown(comparison), encoding="utf-8")
    for task in TASKS:
        (out / f"reliability_{task}.svg").write_text(reliability_svg(comparison, task), encoding="utf-8")
    print(f"wrote {out / 'report.md'}, comparison.json and {len(TASKS)} reliability diagrams")
    for task in TASKS:
        print(f"{task}: {comparison['tasks'][task]['recommendation']}")
    return 0


def main(argv: list[str] | None = None, environ: Mapping[str, str] | None = None) -> int:
    argv = sys.argv[1:] if argv is None else argv
    environ = os.environ if environ is None else environ
    refused = [a for a in argv if FORBIDDEN in a] + [f"${k}" for k, v in environ.items() if FORBIDDEN in v]
    if refused:
        print(f"refused: {FORBIDDEN} is referenced by {refused}")
        return 2
    try:
        return run(argv, environ)
    except (InputError, json.JSONDecodeError, OSError, ValueError) as exc:
        print(f"input error: {exc}")
        return 2


if __name__ == "__main__":
    sys.exit(main())
