"""python -m benchmark.x5 --manifest FILE --facade-url URL [--control-url URL] --bench-token-file FILE --out DIR

Runs the X5 dry run and writes `runs.json` (the scorer's input) and `dry-run.json` (a label that says no model was called). Exit status: 0 clean, 1 bad manifest or facade, 4 the oracle log
disagreed with what was sent, 5 a live run was asked for (ruling A118). The bench token is read from a file, never from an argument or the environment."""
import argparse
import json
import sys
from pathlib import Path

from benchmark.x5 import manifest
from benchmark.x5.driver import Scripted
from benchmark.x5.facade_client import FacadeError
from benchmark.x5.runner import run

LABEL = "DRY RUN: a scripted driver, no live model, no provider call. It says nothing about any model, any arm or Noema."


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="benchmark.x5", description=__doc__)
    p.add_argument("--manifest", type=Path, required=True)
    p.add_argument("--facade-url", required=True, help="the facade's data side")
    p.add_argument("--control-url", help="the facade's control side (a separate listener); defaults to --facade-url")
    p.add_argument("--bench-token-file", type=Path, required=True)
    p.add_argument("--out", type=Path, required=True)
    p.add_argument("--live", action="store_true", help="refused: " + manifest.LIVE_REASON)
    args = p.parse_args(argv)
    if args.live:
        print(manifest.LIVE_REASON, file=sys.stderr)
        return 5
    try:
        m = manifest.load(args.manifest)
    except manifest.LiveRunRefused as err:
        print(err, file=sys.stderr)
        return 5
    except manifest.ManifestError as err:
        print(f"manifest refused: {err}", file=sys.stderr)
        return 1
    try:
        token = args.bench_token_file.read_text().strip()
        result = run(m, facade_url=args.facade_url, bench_token=token, driver=Scripted(m.dry_script), control_url=args.control_url)
    except (OSError, FacadeError) as err:
        print(f"dry run failed: {err}", file=sys.stderr)
        return 1
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "runs.json").write_text(json.dumps(result.scorer_input, indent=2, sort_keys=True) + "\n")
    label = {"mode": "dry_run", "live_model_calls": 0, "label": LABEL, "runs": len(result.scorer_input["runs"]), "problems": result.problems}
    (args.out / "dry-run.json").write_text(json.dumps(label, indent=2, sort_keys=True) + "\n")
    for problem in result.problems:
        print(problem, file=sys.stderr)
    return 4 if result.problems else 0


if __name__ == "__main__":
    sys.exit(main())
