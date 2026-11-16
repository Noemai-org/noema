#!/usr/bin/env python3
"""Workflows run with the least privilege their job needs (REQ-SEC-007; PLT-02).

The job token of a workflow can write to the repository, to issues, to pull requests and to packages unless the workflow narrows it. A workflow that does not need to write should not be
able to, so a bug or a hostile step in it (a test dependency, a benchmark adapter) cannot use the token. This check fails when:

  1. a workflow has no top-level `permissions:` mapping (an absent block takes the repository's default, which may be read-write), or the block is a scalar (`write-all`, `read-all`);
  2. the top-level `contents` permission is anything but `read` or `none`;
  3. any `permissions:` block (top level or job level) grants `write` to a permission that is not in ALLOW_WRITE for that workflow file, or holds a value other than `read`, `write`, `none`;
  4. a workflow uses the `pull_request_target` trigger (it runs with a write token on code from a fork) or names `write-all`, `read-all` or `id-token` outside a comment;
  5. an ALLOW_WRITE entry is no longer used (the list only shrinks: a removed grant removes its entry in the same change).

`permissions: {}` (no permission at all) is accepted. Comments are ignored. ALLOW_WRITE is a reviewed constant: a new write grant is a reviewed change of this file, with its reason.

Usage: check_workflow_permissions.py [workflows_dir]
"""
import re
import sys
from pathlib import Path

# (workflow file, permission) -> why the workflow needs to write it. Both file their own issues with the workflow token and read no secret.
ALLOW_WRITE = {
    ("bench-nightly.yml", "issues"): "files one regression issue per failing benchmark case, de-duplicated by case ID",
    ("main-merge-audit.yml", "issues"): "opens the governance issue for an unreviewed merge to main",
}
VALUES = {"read", "write", "none"}
PERMISSIONS = re.compile(r"^(?P<indent>[ ]*)permissions:[ ]*(?P<rest>.*)$")
ENTRY = re.compile(r"^(?P<indent>[ ]*)(?P<key>[A-Za-z0-9_-]+):[ ]*(?P<value>[^#\s]*)[ ]*(#.*)?$")


def code(line: str) -> str:
    """The line without a trailing comment (a `#` inside a quoted value is not special here: no permission value is quoted)."""
    return line.split("#", 1)[0].rstrip()


def blocks(lines: list[str]) -> list[tuple[int, int, str, dict[str, tuple[str, int]]]]:
    """Every `permissions:` key: (line number, indent, scalar text or "", {permission: (value, line number)})."""
    found = []
    for index, line in enumerate(lines):
        match = PERMISSIONS.match(code(line)) if not line.lstrip().startswith("#") else None
        if not match:
            continue
        indent = len(match.group("indent"))
        rest = match.group("rest").strip()
        entries: dict[str, tuple[str, int]] = {}
        if not rest:
            for follow in range(index + 1, len(lines)):
                text = code(lines[follow])
                if not text.strip():
                    continue
                if len(text) - len(text.lstrip(" ")) <= indent:
                    break
                item = ENTRY.match(text)
                if item:
                    entries[item.group("key")] = (item.group("value"), follow + 1)
        found.append((index + 1, indent, rest, entries))
    return found


def scan(name: str, text: str) -> tuple[list[str], set[tuple[str, str]]]:
    lines = text.splitlines()
    problems: list[str] = []
    used: set[tuple[str, str]] = set()
    found = blocks(lines)
    top = [b for b in found if b[1] == 0]
    if not top:
        problems.append(f"{name}: no top-level `permissions:` block (the repository default may be read-write)")
    for number, indent, scalar, entries in found:
        where = "top-level" if indent == 0 else "job-level"
        if scalar and scalar != "{}":
            problems.append(f"{name}:{number}: {where} `permissions: {scalar}` must be a mapping of read, write or none (or `{{}}`)")
        for permission, (value, entry_line) in entries.items():
            if value not in VALUES:
                problems.append(f"{name}:{entry_line}: `{permission}: {value}` is not read, write or none")
            elif value == "write":
                if (name, permission) in ALLOW_WRITE:
                    used.add((name, permission))
                else:
                    problems.append(f"{name}:{entry_line}: `{permission}: write` is not in ALLOW_WRITE for this workflow")
            if indent == 0 and permission == "contents" and value not in {"read", "none"}:
                problems.append(f"{name}:{entry_line}: top-level `contents` must be read or none")
    for number, line in enumerate(lines, 1):
        body = code(line)
        if re.search(r"(?<![\w-])pull_request_target(?![\w-])", body):
            problems.append(f"{name}:{number}: the `pull_request_target` trigger runs fork code with a write token")
        for word in ("write-all", "read-all", "id-token"):
            if re.search(rf"(?<![\w-]){word}(?![\w-])", body):
                problems.append(f"{name}:{number}: `{word}` is not allowed")
    return problems, used


def violations(directory: Path) -> list[str]:
    problems: list[str] = []
    used: set[tuple[str, str]] = set()
    files = sorted(directory.glob("*.y*ml"))
    for path in files:
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as exc:
            problems.append(f"{path.name}: cannot be read ({exc})")
            continue
        found, used_here = scan(path.name, text)
        problems += found
        used |= used_here
    present = {p.name for p in files}
    for entry in sorted(ALLOW_WRITE):
        if entry[0] in present and entry not in used:
            problems.append(f"{entry[0]}: ALLOW_WRITE lists `{entry[1]}: write`, which the workflow no longer asks for: remove the entry")
    return problems


def main() -> int:
    directory = Path(sys.argv[1] if len(sys.argv) > 1 else Path(__file__).resolve().parents[2] / ".github" / "workflows")
    if not directory.is_dir():
        print(f"workflow-permissions FAIL: {directory} is not a directory")
        return 1
    found = violations(directory)
    for item in found:
        print(f"workflow-permissions FAIL: {item}")
    if found:
        return 1
    print("workflow-permissions OK: every workflow narrows its token; the only write grants are the two reviewed ones")
    return 0


if __name__ == "__main__":
    sys.exit(main())
