#!/usr/bin/env python3
"""Architecture lint harness (PLT-03; SoT §51).

  run.py [--root DIR]     lint a tree (default: the repository); exit 1 on any violation
  run.py --canaries       prove every rule can fail: each canary tree must produce violations
                          of exactly its own rule, and the clean repository tree must pass

Go rules run in tools/lint/golint (go/parser and go/ast). Python rules run in pyrules.py.
"""
import argparse
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPO = HERE.parent.parent
sys.path.insert(0, str(HERE))
import pyrules  # noqa: E402


def lint(root: Path) -> list[tuple[str, str, int, str]]:
    result = subprocess.run(["go", "run", ".", "-root", str(root)], cwd=HERE / "golint", capture_output=True, text=True)
    if result.returncode != 0:
        raise SystemExit(f"golint failed to run:\n{result.stderr}")
    found = [(v["rule"], v["file"], v["line"], v["msg"]) for v in json.loads(result.stdout)]
    for rule in pyrules.ALL:
        found += rule(root)
    return sorted(found)


def report(found) -> None:
    for rule, file, line, msg in found:
        print(f"lint FAIL [{rule}] {file}:{line}: {msg}")


def canaries() -> int:
    clean = lint(REPO)
    print(f"[{'PASS' if not clean else 'FAIL'}] clean tree: {len(clean)} violations")
    report(clean)
    bad = 1 if clean else 0
    for tree in sorted((HERE / "canaries").iterdir()):
        if not tree.is_dir():
            continue
        found = lint(tree)
        rules = {v[0] for v in found}
        ok = rules == {tree.name.split("__")[0]}
        bad += 0 if ok else 1
        print(f"[{'PASS' if ok else 'FAIL'}] canary '{tree.name}': rules fired {sorted(rules) or 'none'}")
        if found:
            print(f"    e.g. {found[0][1]}:{found[0][2]}: {found[0][3]}")
    return 1 if bad else 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=REPO)
    parser.add_argument("--canaries", action="store_true")
    args = parser.parse_args()
    if args.canaries:
        return canaries()
    found = lint(args.root)
    report(found)
    if not found:
        print("lint OK")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
