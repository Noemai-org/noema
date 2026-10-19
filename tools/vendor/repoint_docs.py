#!/usr/bin/env python3
"""Re-point every vendored copy of a docs file to one docs commit (GOV-22).

noema keeps copies of a few files whose originals live in Noemai-org/docs, so CI can use them without
cross-repository access. Each copy starts with an HTML comment naming the docs commit it was taken from.
This script rewrites all of them from one docs checkout, so the next person does not hunt for them.

    python3 tools/vendor/repoint_docs.py --docs <path to a docs checkout at the commit to point at>
    python3 tools/vendor/repoint_docs.py --docs <path> --check     # report differences, change nothing

The docs checkout must be clean and at the commit you want (normally origin/main). The copies are:

    AGENTS.md                           <- AGENTS.md
    CLAUDE.md                           <- CLAUDE.md
    contracts/state-registry.md         <- docs/engineering/state-registry.md
    contracts/perd-32-2-event-types.md  <- section 32.2 of the current PERD (see PERD_FILE below)

When the PERD version changes, edit PERD_FILE. Standard library only.
"""
import argparse
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
PERD_FILE = "docs/poc/perd/NOEMA_POC_PERD_v1.9.md"
EVENT_HEADING = "### 32.2 Event types"

WHOLE_FILES = [
    ("AGENTS.md", "AGENTS.md"),
    ("CLAUDE.md", "CLAUDE.md"),
    ("contracts/state-registry.md", "docs/engineering/state-registry.md"),
]
EXTRACT_TARGET = "contracts/perd-32-2-event-types.md"


def git(docs: Path, *args: str) -> str:
    return subprocess.check_output(["git", "-C", str(docs), *args], text=True).strip()


def whole_header(source: str, commit: str) -> str:
    return (f"<!-- Copy of Noemai-org/docs {source} at docs commit {commit}. The docs original wins on any "
            f"difference. Re-point every copy with: python3 tools/vendor/repoint_docs.py --docs <docs checkout> -->")


def event_table(perd_text: str) -> str:
    lines = perd_text.splitlines()
    start = next((i for i, line in enumerate(lines) if line.startswith(EVENT_HEADING)), None)
    if start is None:
        raise SystemExit(f"no heading '{EVENT_HEADING}' in {PERD_FILE}")
    table = []
    for line in lines[start + 1:]:
        if line.startswith("|"):
            table.append(line)
        elif table:
            break
    if not table:
        raise SystemExit(f"no table found under '{EVENT_HEADING}' in {PERD_FILE}")
    return "\n".join(table)


def expected(docs: Path, commit: str) -> dict[str, str]:
    out = {}
    for target, source in WHOLE_FILES:
        out[target] = whole_header(source, commit) + "\n\n" + (docs / source).read_text(encoding="utf-8")
    header = (f"<!-- Vendored extract of Noemai-org/docs {PERD_FILE} section 32.2 (Event types) at docs commit {commit}. "
              f"The PERD original is normative; this copy exists so CI can compare /contracts against it without "
              f"cross-repository access. Re-point every copy with: python3 tools/vendor/repoint_docs.py --docs <docs checkout> -->")
    out[EXTRACT_TARGET] = header + "\n\n" + EVENT_HEADING + "\n\n" + event_table((docs / PERD_FILE).read_text(encoding="utf-8")) + "\n"
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--docs", required=True, type=Path, help="a clean docs checkout at the commit to point at")
    parser.add_argument("--check", action="store_true", help="report differences and change nothing")
    args = parser.parse_args()
    docs = args.docs.resolve()
    if git(docs, "status", "--porcelain"):
        print(f"{docs} has uncommitted changes; point at a clean checkout of a real commit", file=sys.stderr)
        return 2
    commit = git(docs, "rev-parse", "HEAD")
    differing = []
    for target, text in expected(docs, commit).items():
        path = ROOT / target
        if not path.exists() or path.read_text(encoding="utf-8") != text:
            differing.append(target)
            if not args.check:
                path.write_text(text, encoding="utf-8")
    verb = "differs from" if args.check else "re-pointed to"
    for target in differing:
        print(f"{target}: {verb} docs {commit[:7]}")
    if not differing:
        print(f"all vendored copies already match docs {commit[:7]}")
    return 1 if (differing and args.check) else 0


if __name__ == "__main__":
    sys.exit(main())
