#!/usr/bin/env python3
"""Leak scanner (SEC-03 stub; feeds SEC-07). REQ-SEC-003, POC-NFR-06, AGENT-13.

Finds what must never appear in logs, traces, API output, stored text, explanations, model request logs and the Record:
  * digit runs of 5 or more (REQ-SEC-003: the redactor masks them, so a scan of redacted output finds none);
    a run of exactly 10 digits is reported as an account number, a Luhn-valid run of 13 to 19 as a card number;
  * secrets: private key blocks, cloud access key ids, JWTs, bearer tokens, provider-style keys, and a value assigned to a
    name that says it is a secret (a file path or a redaction placeholder does not count);
  * seeded canaries (`--canary FILE`, one value per line): any occurrence is a leak, whatever its shape.

It NEVER prints a found value, only the file, line, kind, length and a short fingerprint, so a report is itself safe to log.
Fractional seconds in a time stamp are not digit runs. `--allow FILE` holds one regular expression per line; a line of input
that matches one is skipped for the digit-run rule only (never for secrets or canaries). Review changes to that file.

Usage: leakscan.py [--canary FILE] [--allow FILE] PATH [PATH ...]    (exit 0: clean, 1: leak found, 2: usage error)
"""
import argparse
import hashlib
import re
import sys
from dataclasses import dataclass
from pathlib import Path

MIN_RUN = 5
ACCOUNT_DIGITS = 10
CARD_DIGITS = range(13, 20)

DIGIT_RUN = re.compile(r"(?<![A-Za-z0-9])(\d{5,})(?![A-Za-z0-9])")
FRACTION = re.compile(r"\d{2}:\d{2}:\d{2}[.,]\d+")

# Secret shapes. The key-id and key prefixes are built from parts so that this file is not itself a secret finding.
SECRET_SHAPES = [
    ("private_key_block", re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----")),
    ("cloud_access_key_id", re.compile(r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b")),
    ("jwt", re.compile(r"\bey" + r"J[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}")),
    ("bearer_token", re.compile(r"(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}")),
    ("provider_key", re.compile(r"\bsk-[A-Za-z0-9_-]{20,}|\bAIza[0-9A-Za-z_-]{35}")),
]
NAMED_SECRET = re.compile(
    r"""(?i)(secret|token|passw(?:or)?d|api[_-]?key|private[_-]?key|hmac[_-]?key|credential)["']?\s*[:=]\s*["']?([A-Za-z0-9+_=-][A-Za-z0-9+/_=-]{15,})"""
)


@dataclass(frozen=True)
class Finding:
    path: str
    line: int
    kind: str
    length: int
    fingerprint: str

    def __str__(self) -> str:
        return f"{self.path}:{self.line}: {self.kind} (length {self.length}, fingerprint {self.fingerprint})"


def _fp(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8", "replace")).hexdigest()[:8]


def _luhn(digits: str) -> bool:
    total = 0
    for i, ch in enumerate(reversed(digits)):
        n = int(ch)
        if i % 2 == 1:
            n *= 2
            if n > 9:
                n -= 9
        total += n
    return total % 10 == 0


def _digit_findings(line: str, number: int, path: str):
    scrubbed = FRACTION.sub(lambda m: " " * len(m.group(0)), line)
    for m in DIGIT_RUN.finditer(scrubbed):
        run = m.group(1)
        if len(run) in CARD_DIGITS and _luhn(run):
            kind = "card_number"
        elif len(run) == ACCOUNT_DIGITS:
            kind = "account_number"
        else:
            kind = "digit_run"
        yield Finding(path, number, kind, len(run), _fp(run))


def _secret_findings(line: str, number: int, path: str):
    for kind, pattern in SECRET_SHAPES:
        for m in pattern.finditer(line):
            yield Finding(path, number, kind, len(m.group(0)), _fp(m.group(0)))
    for m in NAMED_SECRET.finditer(line):
        value = m.group(2)
        # A path to a mounted secret, a URL, or a value with no digit or no letter is not a secret value itself.
        if value.startswith(("/", "file:")) or not (any(c.isdigit() for c in value) and any(c.isalpha() for c in value)):
            continue
        yield Finding(path, number, "named_secret_value", len(value), _fp(value))


def scan_text(text: str, path: str, canaries=(), allow=()) -> list[Finding]:
    """Scan one text. `allow` is a sequence of compiled regular expressions (digit-run rule only)."""
    found: list[Finding] = []
    for number, line in enumerate(text.splitlines(), 1):
        for canary in canaries:
            if canary and canary in line:
                found.append(Finding(path, number, "canary", len(canary), _fp(canary)))
        found.extend(_secret_findings(line, number, path))
        if not any(a.search(line) for a in allow):
            found.extend(_digit_findings(line, number, path))
    return found


def _lines_of(path: Path) -> list[str]:
    return [ln.rstrip("\n") for ln in path.read_text(encoding="utf-8").splitlines()]


def scan_paths(paths, canaries=(), allow=()) -> list[Finding]:
    found: list[Finding] = []
    for root in paths:
        root = Path(root)
        files = sorted(p for p in root.rglob("*") if p.is_file()) if root.is_dir() else [root]
        for f in files:
            try:
                text = f.read_bytes().decode("utf-8", errors="replace")
            except OSError as exc:
                found.append(Finding(str(f), 0, f"unreadable ({exc.__class__.__name__})", 0, "-"))
                continue
            found.extend(scan_text(text, str(f), canaries, allow))
    return found


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("paths", nargs="+")
    ap.add_argument("--canary", help="file with one seeded canary value per line")
    ap.add_argument("--allow", help="file with one regular expression per line (digit-run rule only)")
    args = ap.parse_args(argv)
    for p in args.paths:
        if not Path(p).exists():
            print(f"leakscan FAIL: {p} does not exist")
            return 2
    canaries = tuple(ln for ln in _lines_of(Path(args.canary)) if ln.strip()) if args.canary else ()
    allow = tuple(re.compile(ln) for ln in _lines_of(Path(args.allow)) if ln.strip()) if args.allow else ()
    found = scan_paths(args.paths, canaries, allow)
    for item in found:
        print("leakscan FAIL: " + str(item))
    if not found:
        print("leakscan OK")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
