"""noema-casegen: `python -m benchmark.generator generate|coverage|assumptions|reproduce [--seed N] [--out DIR] [--summary FILE]`.

`generate` and `coverage` exit 1 when the coverage report has a gap (PERD §48.1 "Coverage rule"): a suite with a gap is not built.
`reproduce` generates twice in two separate processes into `<out>/run1` and `<out>/run2` and exits 1 on any difference (BEN-04, G0-N N10); `--summary` writes the logs and digests, never a generated file.
"""
import argparse
import sys
from pathlib import Path

from benchmark.contracts import sha256_of

from .coverage import load_pack_capabilities, render, report
from .generate import DEFAULT_SEED, GENERATOR_NAME, GENERATOR_VERSION, generate, write
from .library import BY_CATEGORY
from .mutations import PROPOSED


def assumptions() -> str:
    """The archetypes whose expectation is INFERRED: the PERD allows more than one reading and the safer one was taken. For the
    benchmark owner to confirm (AGENT-02)."""
    rows = [a for arcs in BY_CATEGORY.values() for a in arcs if a.basis == "INFERRED"]
    lines = [f"{len(rows)} of {sum(len(v) for v in BY_CATEGORY.values())} archetypes carry an INFERRED expectation:"]
    lines += [f"  {a.key} ({a.cat}; {', '.join(a.covers)})" + (f": {a.note}" if a.note else "") for a in rows]
    lines.append(f"{len(PROPOSED)} mutations are PROPOSED expectations awaiting review: {', '.join(PROPOSED)}")
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="noema-casegen", description=__doc__)
    parser.add_argument("command", choices=("generate", "coverage", "assumptions", "reproduce"))
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    parser.add_argument("--out", type=Path, default=Path("benchmark/out/generated"), help="suite root for `generate`")
    parser.add_argument("--summary", type=Path, help="for `reproduce`: write the summary here")
    args = parser.parse_args(argv)
    if args.command == "reproduce":
        from .reproduce import render as render_summary, reproduce
        outcome = reproduce(args.seed, args.out)
        text = render_summary(outcome)
        sys.stdout.write(text)
        if args.summary is not None:
            with args.summary.open("w", encoding="utf-8", newline="\n") as handle:
                handle.write(text)
        return 0 if outcome.ok else 1
    if args.command == "assumptions":
        print(assumptions())
        return 0
    suite = generate(args.seed)
    result = report(suite.manifest, load_pack_capabilities(), suite.cases)
    print(render(result, suite.manifest))
    if args.command == "generate":
        if result.gaps:
            print("not written: the suite has coverage gaps")
            return 1
        write(suite, args.out)
        print(f"{GENERATOR_NAME} {GENERATOR_VERSION} seed {args.seed}: {len(suite.cases)} cases written to {args.out} (manifest {sha256_of(suite.manifest)})")
    return 1 if result.gaps else 0


if __name__ == "__main__":
    sys.exit(main())
