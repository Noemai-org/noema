#!/usr/bin/env python3
"""The Tushe administrator credential appears in no workflow (SEC-02, REQ-SEC-002; OD-16).

The administrator token is held by the Founder only and is never given to CI. This check fails if any file under
`.github/` (workflows, composite actions, any YAML) names it: the secret `TUSHE_ADMIN_TOKEN` (or any `TUSHE_ADMIN*`
name), the Environment `tushe-admin`, or a spelling of either that differs only in case or in `-`, `_` or space.
Comments are NOT ignored: a comment that names the credential is how it gets wired in later. Unreadable files fail.

Usage: check_no_admin_in_workflows.py [github_dir]
"""
import re
import sys
from pathlib import Path

PATTERN = re.compile(r"tushe[\s_\-]*admin", re.IGNORECASE)
SUFFIXES = {".yml", ".yaml"}


def violations(github_dir: Path) -> list[str]:
    found = []
    for path in sorted(p for p in github_dir.rglob("*") if p.is_file() and p.suffix.lower() in SUFFIXES):
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as exc:
            found.append(f"{path.relative_to(github_dir)}: cannot be read ({exc})")
            continue
        for number, line in enumerate(text.splitlines(), 1):
            if PATTERN.search(line):
                found.append(f"{path.relative_to(github_dir)}:{number}: names the Tushe administrator credential")
    return found


def main() -> int:
    github_dir = Path(sys.argv[1] if len(sys.argv) > 1 else ".github")
    if not github_dir.is_dir():
        print(f"admin-in-workflows FAIL: {github_dir} does not exist")
        return 1
    found = violations(github_dir)
    for item in found:
        print("admin-in-workflows FAIL: " + item)
    if not found:
        print("admin-in-workflows OK")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
