#!/usr/bin/env python3
"""Secret scan limited to the commits of one change (PLT-02; AGENTS section 10).

`gitleaks git` scans every ref by default, so a commit on any other branch (or a branch nobody uses) would fail
the scan of an unrelated pull request. This script scans only the commits reachable from HEAD and not from
BASE: the commits a pull request adds, or the commits a push to main adds. It prints the range it uses.

Usage: gitleaks_range.py <gitleaks> <base-sha> <head-sha>
With no usable base (empty, or all zeros: a new branch) it scans the commits reachable from the head only,
never other refs. Exit status is gitleaks's.
"""
import subprocess
import sys

ZEROS = "0" * 40


def scan_range(base: str, head: str) -> str:
    base = (base or "").strip()
    head = (head or "").strip()
    if not head:
        raise SystemExit("gitleaks_range: no head commit")
    if not base or set(base) == {"0"}:
        return head
    return f"{base}..{head}"


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    gitleaks, base, head = argv
    rng = scan_range(base, head)
    print(f"gitleaks range: {rng}")
    return subprocess.run([gitleaks, "git", ".", "--no-banner", "--redact", f"--log-opts={rng}"]).returncode


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
