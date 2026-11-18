#!/usr/bin/env python3
"""No checkout step leaves a credential on disk (PLT-05, REQ-SEC-005, REQ-SEC-007).

`actions/checkout` writes the job's token into `.git/config` unless the step says `persist-credentials: false`. Every later step of the job, and every script or test the job runs
(a benchmark, a model adapter, a build script), can then read that token from the workspace. A token that nothing needs should not be there. This check fails when any `actions/checkout`
step under `.github/` (workflows and composite actions) does not set `persist-credentials: false` in its own `with:` block. The value must be the literal `false`: an expression, `true`,
a quoted string or a comment that only mentions the key does not count. Unreadable files and unlistable directories fail.

A job that really needs git credentials (a push, a fetch of a private repository) is not allowed to rely on the persisted one: it passes the token to the step that needs it. There is no
exemption list: a new one would be a reviewed change of this file.

Usage: check_checkout_credentials.py [github_dir]
"""
import os
import re
import sys
from pathlib import Path

SUFFIXES = {".yml", ".yaml"}
USES = re.compile(r"^(?P<indent>\s*)(?P<dash>-\s+)?['\"]?uses['\"]?:\s*['\"]?actions/checkout@")
DASH = re.compile(r"^(?P<indent>\s*)-\s")
KEY = re.compile(r"^(?P<indent>\s*)(?P<q>['\"]?)(?P<key>[A-Za-z0-9_-]+)(?P=q):(?P<rest>.*)$")  # a key may be quoted: `"persist-credentials": false` is the same YAML
FALSE = re.compile(r"^\s*false\s*(#.*)?$")


def code_of(line: str) -> str:
    """The line without its YAML comment: a `#` starts one only outside a quoted string and at the start of the line or after a space or tab, so `{name: '# label', uses: ...}` keeps its
    `uses` (Codex P2 on noema#622)."""
    quote = ""
    for index, char in enumerate(line):
        if quote:
            if char == quote and not (quote == '"' and line[index - 1] == chr(92)):
                quote = ""
        elif char in "'\"":
            quote = char
        elif char == "#" and (index == 0 or line[index - 1] in " " + chr(9)):
            return line[:index]
    return line


def indent_of(line: str) -> int:
    return len(line) - len(line.lstrip(" "))


def meaningful(line: str) -> bool:
    stripped = line.strip()
    return bool(stripped) and not stripped.startswith("#")


def step_bounds(lines: list[str], uses_at: int) -> tuple[int, int, int]:
    """The first line, the line after the last, and the indent of the keys of the step that holds the `uses:` line."""
    match = USES.match(lines[uses_at])
    assert match is not None
    key_indent = len(match.group("indent")) + (len(match.group("dash")) if match.group("dash") else 0)
    start = uses_at
    if not match.group("dash"):
        start = uses_at - 1
        while start >= 0 and not (meaningful(lines[start]) and DASH.match(lines[start]) and len(DASH.match(lines[start]).group("indent")) + 2 <= key_indent):  # type: ignore[union-attr]
            start -= 1
        start = max(start, 0)
    dash_indent = len(DASH.match(lines[start]).group("indent")) if DASH.match(lines[start]) else key_indent  # type: ignore[union-attr]
    end = uses_at + 1
    while end < len(lines) and (not meaningful(lines[end]) or indent_of(lines[end]) > dash_indent):
        end += 1
    return start, end, key_indent


def sets_false(lines: list[str], start: int, end: int, key_indent: int) -> bool:
    """True when the step's own `with:` block carries `persist-credentials: false` (the key at the block's depth, the literal value)."""
    index = start
    while index < end:
        line = lines[index]
        match = KEY.match(line.replace("- ", "  ", 1) if index == start and DASH.match(line) else line)
        if match and match.group("key") == "with" and len(match.group("indent")) == key_indent and not match.group("rest").strip().split("#")[0].strip():
            block_indent = None
            for inner in lines[index + 1:end]:
                if not meaningful(inner):
                    continue
                depth = indent_of(inner)
                if depth <= key_indent:
                    break
                block_indent = depth if block_indent is None else block_indent
                item = KEY.match(inner)
                if item and len(item.group("indent")) == block_indent and item.group("key") == "persist-credentials" and FALSE.match(item.group("rest")):
                    return True
            return False
        index += 1
    return False


def violations(github_dir: Path) -> list[str]:
    found: list[str] = []

    def on_error(exc: OSError) -> None:
        found.append(f"{exc.filename}: directory cannot be listed ({exc.__class__.__name__})")

    files = sorted(Path(d) / n for d, _dirs, names in os.walk(github_dir, onerror=on_error) for n in names)
    for path in (p for p in files if p.suffix.lower() in SUFFIXES):
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except (OSError, UnicodeDecodeError) as exc:
            found.append(f"{path.relative_to(github_dir).as_posix()}: cannot be read ({exc})")
            continue
        for number, line in enumerate(lines):
            if meaningful(line) and "actions/checkout" in code_of(line) and not USES.match(line):
                # A checkout written as a flow mapping (`- {uses: actions/checkout@...}`), as `uses :` or in any other shape this reader does not parse could hide a persisted token: refuse it
                # instead of skipping it (Codex P2 on noema#622). Write the step in block style.
                found.append(f"{path.relative_to(github_dir).as_posix()}:{number + 1}: actions/checkout written in a form this check does not read (a flow mapping or an odd spelling): use block style")
                continue
            if not meaningful(line) or not USES.match(line):
                continue
            start, end, key_indent = step_bounds(lines, number)
            if not sets_false(lines, start, end, key_indent):
                found.append(f"{path.relative_to(github_dir).as_posix()}:{number + 1}: actions/checkout without `persist-credentials: false` in its own `with:` block")
    return found


def main() -> int:
    github_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[2] / ".github"
    if not github_dir.is_dir():
        print(f"checkout-credentials FAIL: {github_dir} is not a directory")
        return 1
    found = violations(github_dir)
    for item in found:
        print(f"checkout-credentials FAIL: {item}")
    if found:
        return 1
    print("checkout-credentials OK: every actions/checkout step sets persist-credentials: false")
    return 0


if __name__ == "__main__":
    sys.exit(main())
