#!/usr/bin/env python3
"""The G1 leak scan with a planted canary (Security Reviewer's criterion 8 on noema#436; A154, A184).

A scan that finds nothing must not be read as clean: it may be blind. So this plants one canary where the scanner must find it, and only then trusts the scan of the artifacts:
  1. a fresh canary (`CANARY-` and 24 hex characters) is made and written to a canary file and to a planted file, both under a work directory that is never one of the scanned paths;
  2. the probe: leakscan over the planted files must FAIL, and must report EACH KIND that was planted: the canary, a ten-digit account number, a Luhn-valid card number, a secret-shaped
     value (`api_key=...`), a JWT-shaped value and a bearer token (Security Reviewer's condition on noema#591: a canary alone proves the scanner runs, not that its rules work). A kind that
     is not reported means a rule is broken, and the result is NOT RUN (exit 3), never a pass. The probe uses the same scanner, flags and allow file as the real scan;
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


def _luhn_complete(body: str) -> str:
    """`body` plus the check digit that makes it Luhn-valid."""
    total = 0
    for i, ch in enumerate(reversed(body)):
        d = int(ch) * 2 if i % 2 == 0 else int(ch)
        total += d - 9 if d > 9 else d
    return body + str((10 - total % 10) % 10)


def planted_values(canary: str) -> dict[str, str]:
    """One line per kind the real scan must be able to report, each with a fresh random value. Keys are leakscan's own kind names."""
    ten = str(secrets.randbelow(9 * 10**9) + 10**9)
    card = _luhn_complete("4" + "".join(secrets.choice("0123456789") for _ in range(14)))
    return {
        "canary": f"a planted line that carries {canary} so that the scanner must find it",
        "account_number": f"planted account {ten}",
        "card_number": f"planted card {card}",
        "named_secret_value": f"api_key={secrets.token_hex(10)}",
        "jwt": "planted token eyJ" + secrets.token_hex(6) + "." + secrets.token_hex(6) + "." + secrets.token_hex(6),
        "bearer_token": "Authorization: Bearer " + secrets.token_hex(12),
    }


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
    plants = planted_values(canary)
    for kind, line in plants.items():
        (planted / f"planted-{kind}.txt").write_text(line + "\n", encoding="utf-8")
    code, text = scan(scanner, canary_file, [planted], allow)
    missing = [kind for kind in plants if not any(f"planted-{kind}.txt" in row and (kind in row or (kind == "canary" and "canary" in row)) for row in text.splitlines())]
    if code != 1 or missing or canary in text:
        print(f"NOT RUN: the scanner did not report every planted kind (exit {code}; not reported: {', '.join(missing) or 'none'}); a scan that finds nothing, or whose rules are broken, cannot be read as clean")
        return 3
    print(f"probe: every planted kind was found ({', '.join(plants)}): the scanner can see")
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
