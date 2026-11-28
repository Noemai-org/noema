#!/usr/bin/env python3
"""Decide whether a pull request's heavy CI jobs must run (A770, on A455: the runners are saturated, so a push that changes only documentation or status notes skips them).

Prints `run=true` or `run=false` (and writes it to $GITHUB_OUTPUT when set). It is safe by default: only a pull_request event ever skips, and only when EVERY changed file is a
`.md` file or sits under `status/`; a `.md` file under `contracts/`, `packs/`, `tools/`, `.github/` or `deploy/`, and the root AGENTS.md and CLAUDE.md, are treated as code, because a checker
may read them (a module README under `services/`, `benchmark/`, `synthetic/` or `ui/` is documentation). An empty or unreadable change list runs the jobs.

Usage: docs_only.py <event-name> <base-sha>   (the head is the checked-out HEAD; needs a checkout with fetch-depth 0)."""
import os
import posixpath
import subprocess
import sys
from collections.abc import Sequence

CODE_TREES = ("contracts/", "packs/", "tools/", ".github/", "deploy/")
CODE_ROOT_FILES = {"AGENTS.md", "CLAUDE.md"}


def _skippable(path: str) -> bool:
    if not path or path.startswith("/") or ".." in path.split("/"):
        return False
    norm = posixpath.normpath(path)
    if norm != path:
        return False
    if path.startswith("status/"):
        return True
    if not path.endswith(".md"):
        return False
    return not path.startswith(CODE_TREES) and path not in CODE_ROOT_FILES


def heavy_needed(files: Sequence[str]) -> bool:
    """True unless the list is non-empty and every file is documentation or a status note."""
    cleaned = [f.strip() for f in files]
    if not cleaned or any(not f for f in cleaned):
        return True
    return not all(_skippable(f) for f in cleaned)


def decide(event: str, files: Sequence[str]) -> bool:
    """True if the heavy jobs must run."""
    if event != "pull_request":
        return True
    return heavy_needed(files)


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    event, base = argv[1], argv[2]
    files: list[str] = []
    if event == "pull_request" and base:
        out = subprocess.run(["git", "diff", "--name-only", base, "HEAD"], capture_output=True, text=True)
        if out.returncode == 0:
            files = out.stdout.splitlines()
    run = "true" if decide(event, files) else "false"
    print(f"changed files: {len(files)}; heavy jobs: {run}")
    target = os.environ.get("GITHUB_OUTPUT")
    if target:
        with open(target, "a", encoding="utf-8") as handle:
            handle.write(f"run={run}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
