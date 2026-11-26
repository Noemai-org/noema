"""Prints the principals of the journeys manifest as `id:role` words for bench_tokens.sh (A683; see test_journey_principals.py).

Usage: journey_principals.py MANIFEST [--expect N]. Reads the manifest only: it makes no token, reads no key and no environment variable, and prints nothing on a refusal. Exit 0 with one line of
space-separated `id:role` words in the manifest's order, each principal once; exit 2 on anything it cannot read in full. SYNTHETIC."""
import argparse
import json
import re
import sys
from pathlib import Path

ROLES = ("customer", "ops", "approver", "audit")
USR = re.compile(r"^usr_[A-Za-z0-9]{2,32}$")  # the form services/core/cmd/synthetic-idp accepts


def refuse(message: str) -> None:
    print(f"journey_principals: {message}", file=sys.stderr)
    sys.exit(2)


def principals(doc: object) -> list[tuple[str, str]]:
    if not isinstance(doc, dict) or not isinstance(doc.get("journeys"), list):
        refuse("the manifest has no journeys list")
    seen: dict[str, str] = {}
    for journey in doc["journeys"]:
        cases = journey.get("cases") if isinstance(journey, dict) else None
        if not isinstance(cases, list):
            refuse("a journey has no cases list")
        for case in cases:
            pid, roles = (case.get("principal_id"), case.get("roles")) if isinstance(case, dict) else (None, None)
            if not isinstance(pid, str) or not USR.match(pid):
                refuse("a case has a principal id the issuer would refuse")
            if not isinstance(roles, list) or len(roles) != 1 or roles[0] not in ROLES:
                refuse("a case does not carry exactly one role of the four")
            if seen.setdefault(pid, roles[0]) != roles[0]:
                refuse("a principal is listed with two different roles")
    if not seen:
        refuse("the manifest lists no principal")
    return list(seen.items())


def main() -> None:
    p = argparse.ArgumentParser(allow_abbrev=False)
    p.add_argument("manifest", type=Path)
    p.add_argument("--expect", type=str, default=None)
    a = p.parse_args()
    try:
        doc = json.loads(a.manifest.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        refuse("the manifest cannot be read as JSON")
    words = principals(doc)
    if a.expect is not None:
        if not a.expect.isdigit() or int(a.expect) != len(words):
            refuse(f"the manifest lists {len(words)} principals, not the {a.expect} expected")
    print(" ".join(f"{pid}:{role}" for pid, role in words))


if __name__ == "__main__":
    main()
