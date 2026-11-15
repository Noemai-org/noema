#!/usr/bin/env python3
"""Leak scanner (SEC-03 stub; feeds SEC-07). REQ-SEC-003, POC-NFR-06, AGENT-13.

Finds what must never appear in logs, traces, API output, stored text, explanations, model request logs and the Record:
  * digit runs of 5 or more (REQ-SEC-003: the redactor masks them, so a scan of redacted output finds none);
    a run of exactly 10 digits is reported as an account number, a Luhn-valid run of 13 to 19 as a card number;
  * secrets: private key blocks, cloud access key ids, JWTs, bearer tokens, provider-style keys, and a value assigned to a
    name that says it is a secret (a file path or a redaction placeholder does not count);
  * seeded canaries (`--canary FILE`, one value per line): any occurrence is a leak, whatever its shape.

It NEVER prints a found value, only the file, line, kind, length and a fingerprint: an HMAC under a random key made for each run, so the
fingerprint matches two sightings of one value within a run and cannot be reversed by enumerating candidates. A report is safe to log.
Fractional seconds (up to nine digits) of a complete time stamp are not digit runs; a complete ISO date is exempt only as a whole token.
`--allow FILE` holds one regular expression per line; a line of input that matches one is skipped for the digit-run rule only (never
for secrets or canaries). A pattern that matches the empty string is refused. Review changes to that file.

A scan that looked at nothing must not say OK: zero files or zero lines scanned, an empty canary file, a missing or invalid allow file
are exit 2; a symlink and a file that is binary, compressed or not UTF-8 text are findings (they cannot be searched).

Known gaps (SEC-07 widens them): numbers split by dots or slashes, base64 or percent-encoded forms, and secrets whose names carry
no secret word. False positives (ports, epoch milliseconds, version strings) go in the allow file.

Usage: leakscan.py [--canary FILE] [--allow FILE] PATH [PATH ...]
Exit: 0 clean, 1 leak found, 2 usage error or a scan that looked at nothing.
"""
import argparse
import hashlib
import hmac
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path

MIN_RUN = 5
ACCOUNT_DIGITS = 10
CARD_DIGITS = range(13, 20)

# A number as the model redactor reads it: digits, with a single space or hyphen allowed between two digits (Codex review of #222).
DIGIT_RUN = re.compile(r"(?<![A-Za-z0-9])\d(?:[ -]?\d)*(?![A-Za-z0-9])")
# Exempt only complete tokens: a fraction of at most nine digits after hh:mm:ss, and an ISO date not followed by more digits joined by a space or
# hyphen (otherwise "0123-45-67-89" would lose its first ten characters, Codex review of #222).
FRACTION = re.compile(r"(?<![\d:])\d{2}:\d{2}:\d{2}[.,]\d{1,9}(?!\d)")
ISO_DATE = re.compile(r"(?<!\d)\d{4}-\d{2}-\d{2}(?!\d)(?![ -]\d)")
# A value that only stands in for a secret: asterisks or x's, or the word redacted or masked, alone or repeated.
PLACEHOLDER = re.compile(r"^(?:[*xX_=-]+|(?:redacted|masked)(?:[-_]?(?:redacted|masked))*|null|none|true|false)$", re.IGNORECASE)

# Secret shapes. The key-id and key prefixes are built from parts so that this file is not itself a secret finding.
SECRET_SHAPES = [
    ("private_key_block", re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY(?: BLOCK)?-----")),
    ("github_token", re.compile(r"\bgh[pousr]_[A-Za-z0-9]{36,}|\bgithub_pat_[A-Za-z0-9_]{22,}")),
    ("slack_token", re.compile(r"\bxox[abprs]-[A-Za-z0-9-]{10,}")),
    ("basic_auth", re.compile(r"(?i)\bauthorization\W{0,3}basic\s+[A-Za-z0-9+/=]{8,}")),
    ("cloud_access_key_id", re.compile(r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b")),
    ("jwt", re.compile(r"\bey" + r"J[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}")),
    ("bearer_token", re.compile(r"(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}")),
    ("provider_key", re.compile(r"\bsk-[A-Za-z0-9_-]{20,}|\bsk_(?:live|test)_[A-Za-z0-9]{16,}|\bAIza[0-9A-Za-z_-]{35}")),
]
# A name that says it is a secret (the word may sit anywhere in the key, as in the model redactor), then the WHOLE assigned value: quoted
# (spaces and punctuation allowed, escaped JSON quotes allowed) or bare up to a delimiter. Paths and placeholders are filtered afterwards.
NAMED_SECRET = re.compile(
    r"""(?i)[A-Za-z0-9_.-]*(?:secret|token|passw(?:or)?d|pwd|api[_-]?key|private[_-]?key|hmac[_-]?key|credential|authorization)[A-Za-z0-9_.-]*"""
    r"""\\?["']?\s*[:=]\s*(?:\\?(?P<q>["'])(?P<quoted>[^"'\\]*)|(?P<bare>[^\s"'\\,;&}]+))"""
)
MIN_SECRET_VALUE = 8
_SALT = os.urandom(32)


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
    return hmac.new(_SALT, value.encode("utf-8", "replace"), hashlib.sha256).hexdigest()[:8]


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
    scrubbed = ISO_DATE.sub(lambda m: " " * len(m.group(0)), FRACTION.sub(lambda m: " " * len(m.group(0)), line))
    for m in DIGIT_RUN.finditer(scrubbed):
        run = re.sub(r"\D", "", m.group(0))
        if len(run) < MIN_RUN:
            continue
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
        value = m.group("quoted") if m.group("q") else m.group("bare")
        # A path to a mounted secret and a redaction placeholder are not secret values. A value with no digit is not exempt: a password or a
        # token need not contain one (Codex review of #222).
        if value is None or len(value) < MIN_SECRET_VALUE or value.startswith(("/", "file:")) or PLACEHOLDER.match(value.strip("[]<>(){}")):
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


def _files_of(root: Path, found: list[Finding]) -> list[Path]:
    """Every file under root. os.walk with an error callback, not Path.rglob: rglob silently skips a directory it cannot list, and a scan
    that did not look everywhere must not say OK (Codex review of #222). A directory that cannot be listed becomes a finding, and so
    does a symlink (os.walk does not follow one, so what it points to would go unscanned)."""
    if root.is_symlink():
        found.append(Finding(str(root), 0, "symlink", 0, "-"))
        return []
    if not root.is_dir():
        return [root]

    def on_error(exc: OSError) -> None:
        found.append(Finding(str(exc.filename or root), 0, f"unreadable_directory ({exc.__class__.__name__})", 0, "-"))

    files: list[Path] = []
    for d, dirs, names in os.walk(root, onerror=on_error):
        for n in list(dirs):
            if (Path(d) / n).is_symlink():
                found.append(Finding(str(Path(d) / n), 0, "symlink", 0, "-"))
                dirs.remove(n)
        for n in names:
            p = Path(d) / n
            if p.is_symlink():
                found.append(Finding(str(p), 0, "symlink", 0, "-"))
            else:
                files.append(p)
    return sorted(files)


def _decode(data: bytes) -> str | None:
    """Text, or None when the bytes are not plain UTF-8 text a line scan can read (NUL bytes cover UTF-16 and most binary; gzip has a magic)."""
    if b"\x00" in data or data[:2] == b"\x1f\x8b":
        return None
    try:
        return data.decode("utf-8")
    except UnicodeDecodeError:
        return None


def scan_paths(paths, canaries=(), allow=()):
    """Returns (findings, files scanned, lines scanned)."""
    found: list[Finding] = []
    n_files = n_lines = 0
    for root in paths:
        for f in _files_of(Path(root), found):
            try:
                data = f.read_bytes()
            except OSError as exc:
                found.append(Finding(str(f), 0, f"unreadable ({exc.__class__.__name__})", 0, "-"))
                continue
            text = _decode(data)
            if text is None:
                found.append(Finding(str(f), 0, "binary_or_undecodable", len(data), "-"))
                continue
            n_files += 1
            n_lines += len(text.splitlines())
            found.extend(scan_text(text, str(f), canaries, allow))
    return found, n_files, n_lines


def _read_list(path: str, what: str) -> list[str] | None:
    try:
        return _lines_of(Path(path))
    except (OSError, UnicodeDecodeError) as exc:
        print(f"leakscan FAIL: cannot read the {what} file ({exc.__class__.__name__})")
        return None


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("paths", nargs="+")
    ap.add_argument("--canary", help="file with one seeded canary value per line")
    ap.add_argument("--allow", help="file with one regular expression per line (digit-run rule only)")
    args = ap.parse_args(argv)
    for p in args.paths:
        if not Path(p).exists() and not Path(p).is_symlink():
            print(f"leakscan FAIL: {p} does not exist")
            return 2
    canaries: tuple[str, ...] = ()
    if args.canary:
        lines = _read_list(args.canary, "canary")
        if lines is None:
            return 2
        canaries = tuple(ln for ln in lines if ln.strip())
        if not canaries:
            print("leakscan FAIL: the canary file holds no value, so no canary was looked for")
            return 2
    allow: tuple[re.Pattern[str], ...] = ()
    if args.allow:
        lines = _read_list(args.allow, "allow")
        if lines is None:
            return 2
        compiled = []
        for ln in (x for x in lines if x.strip()):
            try:
                rx = re.compile(ln)
            except re.error:
                print("leakscan FAIL: an allow pattern is not a valid regular expression")
                return 2
            if rx.search(""):
                print("leakscan FAIL: an allow pattern matches the empty string, so it would skip every line")
                return 2
            compiled.append(rx)
        allow = tuple(compiled)
    found, n_files, n_lines = scan_paths(args.paths, canaries, allow)
    for item in found:
        print("leakscan FAIL: " + str(item))
    if found:
        return 1
    if n_files == 0 or n_lines == 0:
        print(f"leakscan FAIL: scanned {n_files} files, {n_lines} lines: a scan that looked at nothing is not a pass")
        return 2
    print(f"leakscan OK: scanned {n_files} files, {n_lines} lines")
    return 0


if __name__ == "__main__":
    sys.exit(main())
