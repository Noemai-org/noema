#!/usr/bin/env python3
"""The G1 leak scan with a planted canary (Security Reviewer's criterion 8 on noema#436; A154, A184).

A scan that finds nothing must not be read as clean: it may be blind. So this plants one canary where the scanner must find it, and only then trusts the scan of the artifacts:
  1. a fresh canary (`CANARY-` and 24 hex characters) is made and written to a canary file and to a planted file, both under a work directory that is never one of the scanned paths;
  2. the probe: leakscan over the planted file must FAIL and name a canary. If it does not, the scanner is blind and the result is NOT RUN (exit 3), never a pass;
  3. the real scan: leakscan with the same canary file over every artifact path: zero findings, which also means no digit run of 5 or more anywhere (leakscan's own rule) and the canary
     nowhere in the artifacts. A scan that looked at nothing (leakscan exit 2) is NOT RUN as well.
It never prints a found value (leakscan prints only file, line, kind, length and a fingerprint) and never prints the canary. The criterion has no exceptions beyond the allow file, which lets a line through the digit-run rule only (never secrets or the canary); the workflow passes tools/ci/g1_leakscan.allow, which names three integer fields.

Usage: g1_leakscan.py [--scanner PATH] [--workdir DIR] [--allow FILE] PATH [PATH ...]
Exit: 0 clean (and the scanner proved itself), 1 a leak was found in the artifacts, 2 usage error, 3 NOT RUN (the scanner is blind or looked at nothing)."""
import argparse
import secrets
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


def scan(scanner: Path, canary_file: Path, paths: list[Path], allow: Path | None = None) -> tuple[int, str]:
    extra = ["--allow", str(allow)] if allow else []
    done = subprocess.run([sys.executable, "-I", str(scanner), "--canary", str(canary_file), *extra, *map(str, paths)], capture_output=True, text=True, check=False)
    return done.returncode, (done.stdout + done.stderr).strip()


def run(scanner: Path, workdir: Path, paths: list[Path], allow: Path | None = None) -> int:
    for path in paths:
        if workdir == path or workdir in path.parents or path in workdir.parents:
            print("refused: the work directory must not be, contain or lie inside a scanned path")
            return 2
    canary = "CANARY-" + secrets.token_hex(12)
    planted = workdir / "planted"
    planted.mkdir(parents=True, exist_ok=True)
    canary_file = workdir / "canary.txt"
    canary_file.write_text(canary + "\n", encoding="utf-8")
    (planted / "planted.txt").write_text(f"a planted line that carries {canary} so that the scanner must find it\n", encoding="utf-8")
    code, text = scan(scanner, canary_file, [planted], allow)
    if code != 1 or "canary" not in text or canary in text:
        print(f"NOT RUN: the scanner did not find the planted canary (exit {code}); a scan that finds nothing cannot be read as clean")
        return 3
    print("probe: the planted canary was found (the scanner can see)")
    code, text = scan(scanner, canary_file, paths, allow)
    if canary in text:
        print("refused: the scanner printed the canary")
        return 3
    print(text)
    if code == 0:
        return 0
    if code == 1:
        print("LEAK SCAN FAILED: findings in the artifacts (see the lines above: file, line, kind, length, fingerprint)")
        return 1
    print(f"NOT RUN: the scan itself failed or looked at nothing (exit {code})")
    return 3


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--scanner", type=Path, default=Path(__file__).resolve().parent / "leakscan.py")
    p.add_argument("--workdir", type=Path)
    p.add_argument("--allow", type=Path, help="leakscan's allow file: regular expressions of lines skipped for the digit-run rule only (never for secrets or canaries)")
    p.add_argument("paths", nargs="+", type=Path)
    a = p.parse_args(argv)
    paths = [x.resolve() for x in a.paths]
    missing = [str(x) for x in paths if not x.exists()]
    if missing:
        print(f"refused: not found: {', '.join(missing)}")
        return 2
    if a.workdir:
        a.workdir.mkdir(parents=True, exist_ok=True)
        return run(a.scanner, a.workdir.resolve(), paths, a.allow)
    work = Path(tempfile.mkdtemp(prefix="g1-leakscan-"))
    try:
        return run(a.scanner, work.resolve(), paths, a.allow)
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
