#!/usr/bin/env python3
"""Workflows run with the least privilege their job needs (REQ-SEC-007; PLT-02).

The job token of a workflow can write to the repository, to issues, to pull requests and to packages unless the workflow narrows it. A workflow that does not need to write should not be
able to, so a bug or a hostile step in it (a test dependency, a benchmark adapter) cannot use the token. This check fails when:

  1. a workflow has no top-level `permissions:` mapping (an absent block takes the repository's default, which may be read-write), or the block is a scalar (`write-all`, `read-all`) or empty;
  2. the top-level `contents` permission is anything but `read` or `none`;
  3. any `permissions:` block (top level or job level) grants `write` to a permission that is not in ALLOW_WRITE for that workflow file, or holds a value other than `read`, `write`, `none`;
  4. a workflow uses the `pull_request_target` trigger (it runs with a write token on code from a fork) or a permission block names `id-token`;
  5. an ALLOW_WRITE entry is no longer used (the list only shrinks: a removed grant removes its entry in the same change).

The files are read as PARSED YAML (tools/ci/yamlload.py: PyYAML safe_load, OD-P-95, ruling A283), so no spelling hides a block: a quoted key, a flow mapping, a space before the colon, a
quoted `#`, a YAML escape in a key and a JSON-style job are the same document as the plain form. A file that is not plain YAML (an anchor, an alias, a repeated key, a second document, an
unreadable file) fails. `permissions: {}` (no permission at all) is accepted. ALLOW_WRITE is a reviewed constant: a new write grant is a reviewed change of this file, with its reason.

Usage: check_workflow_permissions.py [workflows_dir]
"""
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parent))
import yamlload  # noqa: E402

# (workflow file, permission) -> why the workflow needs to write it. Both file their own issues with the workflow token and read no secret.
ALLOW_WRITE = {
    ("bench-nightly.yml", "issues"): "files one regression issue per failing benchmark case, de-duplicated by case ID",
    ("main-merge-audit.yml", "issues"): "opens the governance issue for an unreviewed merge to main",
}
VALUES = {"read", "write", "none"}
ABSENT = object()


def trigger_names(document: dict) -> list[str]:
    """The events a workflow runs on. YAML 1.1 reads the bare key `on` as the boolean true, so both spellings are looked up."""
    value = document.get("on", document.get(True, ABSENT))
    if value is ABSENT:
        return []
    if isinstance(value, str):
        return [value]
    if isinstance(value, list):
        return [v for v in value if isinstance(v, str)]
    if isinstance(value, dict):
        return [k for k in value if isinstance(k, str)]
    return []


def check_block(name: str, where: str, block: Any, top: bool, problems: list[str], used: set[tuple[str, str]]) -> None:
    if not isinstance(block, dict):
        shown = block if isinstance(block, str) else type(block).__name__
        problems.append(f"{name}: {where} `permissions` must be a mapping of read, write or none (or `{{}}`), not `{shown}`")
        return
    for permission, value in block.items():
        if not isinstance(permission, str):
            problems.append(f"{name}: {where} a permission name that is not a string")
            continue
        if permission.strip().lower() == "id-token":
            problems.append(f"{name}: {where} `{permission}` is not allowed")
        if not isinstance(value, str) or value not in VALUES:
            problems.append(f"{name}: {where} `{permission}` is not read, write or none")
        elif value == "write":
            if (name, permission) in ALLOW_WRITE:
                used.add((name, permission))
            else:
                problems.append(f"{name}: {where} `{permission}: write` is not in ALLOW_WRITE for this workflow")
        if top and permission == "contents" and value not in {"read", "none"}:
            problems.append(f"{name}: top-level `contents` must be read or none")


def scan(name: str, text: str) -> tuple[list[str], set[tuple[str, str]]]:
    problems: list[str] = []
    used: set[tuple[str, str]] = set()
    try:
        document = yamlload.load(text, name)
    except yamlload.YamlRefused as exc:
        return [str(exc)], used
    if not isinstance(document, dict):
        return [f"{name}: the workflow is not a mapping"], used
    if "permissions" not in document:
        problems.append(f"{name}: no top-level `permissions:` block (the repository default may be read-write)")
    else:
        check_block(name, "top-level", document["permissions"], True, problems, used)
    jobs = document.get("jobs")
    if isinstance(jobs, dict):
        for job_id, job in jobs.items():
            if isinstance(job, dict) and "permissions" in job:
                check_block(name, f"job `{job_id}`", job["permissions"], False, problems, used)
    if "pull_request_target" in trigger_names(document):
        problems.append(f"{name}: the `pull_request_target` trigger runs fork code with a write token")
    return problems, used


def violations(directory: Path) -> list[str]:
    problems: list[str] = []
    used: set[tuple[str, str]] = set()
    files = sorted(directory.glob("*.y*ml"))
    for path in files:
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as exc:
            problems.append(f"{path.name}: cannot be read ({exc.__class__.__name__})")
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
    if not yamlload.available():
        print("workflow-permissions FAIL: PyYAML is not installed (pip install --require-hashes -r tools/ci/requirements.lock)")
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
