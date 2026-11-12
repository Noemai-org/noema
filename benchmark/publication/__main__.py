"""python -m benchmark.publication check PUBLICATION.json --scorecards DIR   |   claim --scorecard FILE --id ID --ref POINTER --text TEXT   (BEN-31)

`check` applies the publication rules (benchmark/publication/README.md) to a publication and its evidence bundle (a directory of scorecards, one per run ID). Exit 0: no problem.
1: problems (all are listed). 2: an input is unreadable. `claim` prints a claim whose numbers are copied from a scorecard, so nobody types one.
Generated layer only; refuses any argument or environment value that names `benchmark-heldout` (INV-22, REQ-AGT-004)."""
import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any

from benchmark.contracts import load_strict
from benchmark.publication.check import build_claim, check

Json = dict[str, Any]
FORBIDDEN = "benchmark-heldout"


def _refused(argv: list[str]) -> list[str]:
    return [a for a in argv if FORBIDDEN in a] + [f"${k}" for k, v in os.environ.items() if FORBIDDEN in v]


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    refused = _refused(argv)
    if refused:
        print(f"refused: {', '.join(refused)} names the held-out set, which this tool never reads (INV-22, REQ-AGT-004)", file=sys.stderr)
        return 2
    parser = argparse.ArgumentParser(prog="benchmark.publication", description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    c = sub.add_parser("check")
    c.add_argument("publication")
    c.add_argument("--scorecards", required=True, help="the evidence bundle: a directory of scorecard JSON files")
    k = sub.add_parser("claim")
    k.add_argument("--scorecard", required=True)
    k.add_argument("--id", required=True)
    k.add_argument("--ref", required=True, help="a JSON pointer to a rate in the scorecard, for example /tier1/correct_terminal_handling")
    k.add_argument("--text", required=True)
    args = parser.parse_args(argv)
    try:
        if args.command == "claim":
            print(json.dumps(build_claim(load_strict(Path(args.scorecard)), args.id, args.ref, args.text), indent=2, sort_keys=True, ensure_ascii=False))
            return 0
        publication = load_strict(Path(args.publication))
        cards: dict[str, Json] = {}
        for path in sorted(Path(args.scorecards).glob("*.json")):
            card = load_strict(path)
            if isinstance(card, dict) and "run_id" in card and "tier0" in card:
                cards[card["run_id"]] = card
    except (OSError, ValueError, KeyError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    problems = check(publication, cards)
    for problem in problems:
        print(problem)
    claims = len(publication.get("claims") or [])
    print(f"{'no problem' if not problems else str(len(problems)) + ' problem(s)'}: {claims} claims, {len(cards)} scorecards in the bundle")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
