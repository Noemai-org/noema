"""python -m benchmark.hallucination write [--seed N] --out FILE   |   verify FILE   (BEN-14)

`write` writes the corpus (sorted keys, two-space indent, one trailing newline). `verify` re-checks a corpus file with the reference checker: every bad item must break the
check it claims, every control must pass all four. Exit 0 ok, 1 an item is wrong, 2 unreadable input. Generated layer only; refuses any argument naming `benchmark-heldout`."""
import argparse
import json
import sys
from pathlib import Path

from benchmark.hallucination.corpus import DEFAULT_SEED, generate
from benchmark.hallucination.verify import violated_checks


def check_items(corpus: dict) -> list[str]:  # type: ignore[type-arg]
    problems = []
    for item in corpus["items"]:
        violated = violated_checks(item["facts"], item["rendered"])
        if item["expected"]["blocked"]:
            if item["expected"]["check"] not in violated:
                problems.append(f"{item['item_id']}: claims check {item['expected']['check']} but the text breaks {sorted(violated)}")
            if item["injected"] not in item["rendered"]["text"]:
                problems.append(f"{item['item_id']}: the injected span is not in the text")
        elif violated:
            problems.append(f"{item['item_id']}: a control breaks check(s) {sorted(violated)}")
    return problems


def dump(corpus: dict) -> bytes:  # type: ignore[type-arg]
    return (json.dumps(corpus, sort_keys=True, indent=2, ensure_ascii=False) + "\n").encode("utf-8")


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    if any("benchmark-heldout" in a for a in argv):
        print("refused: the held-out set is never read (INV-22, REQ-AGT-004)", file=sys.stderr)
        return 2
    parser = argparse.ArgumentParser(prog="benchmark.hallucination", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    w = sub.add_parser("write")
    w.add_argument("--seed", type=int, default=DEFAULT_SEED)
    w.add_argument("--out", required=True)
    v = sub.add_parser("verify")
    v.add_argument("file")
    args = parser.parse_args(argv)
    try:
        if args.command == "write":
            Path(args.out).write_bytes(dump(generate(args.seed)))
            print(f"wrote {args.out}")
            return 0
        corpus = json.loads(Path(args.file).read_text(encoding="utf-8"))
        problems = check_items(corpus)
    except (OSError, ValueError, KeyError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    for problem in problems:
        print(problem)
    print(f"{'ok' if not problems else str(len(problems)) + ' problem(s)'}: {len(corpus['items'])} items")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
