#!/usr/bin/env python3
"""Workflows run with the least privilege their job needs (REQ-SEC-007; PLT-02).

The job token of a workflow can write to the repository, to issues, to pull requests and to packages unless the workflow narrows it. A workflow that does not need to write should not be
able to, so a bug or a hostile step in it (a test dependency, a benchmark adapter) cannot use the token. The workflow files are PARSED (A283, OD-P-95 D1; the loader is described in
check_checkout_credentials.py: PyYAML's SafeLoader that refuses a repeated key and reads booleans as YAML 1.2), so no layout hides a grant. This check fails when:

  1. a workflow has no top-level `permissions` mapping (an absent block takes the repository's default, which may be read-write), or the block is a scalar (`write-all`, `read-all`);
  2. the top-level `contents` permission is anything but `read` or `none`;
  3. any `permissions` block (top level or job level) grants `write` to a permission that is not in ALLOW_WRITE for that workflow file, or holds a value other than `read`, `write`, `none`,
     or names `id-token` (any value);
  4. a workflow uses the `pull_request_target` trigger (it runs with a write token on code from a fork);
  5. an ALLOW_WRITE entry is no longer used (the list only shrinks: a removed grant removes its entry in the same change);
  6. a workflow file cannot be read or parsed, or repeats a key.

`permissions: {}` (no permission at all) is accepted. ALLOW_WRITE is a reviewed constant: a new write grant is a reviewed change of this file, with its reason.

Usage: check_workflow_permissions.py [workflows_dir]
"""
import re
import sys
from pathlib import Path
from typing import Any

try:
    import yaml
except ImportError:  # fail closed: a check that cannot read the files must not pass them
    yaml = None  # type: ignore[assignment]

MAX_DEPTH = 64
BOOL = re.compile(r"^(?:true|True|TRUE|false|False|FALSE)$")


def make_loader() -> Any:
    """yaml.SafeLoader that refuses a repeated key and reads booleans as YAML 1.2 does. Never the unsafe `yaml.load` loaders."""
    assert yaml is not None

    class StrictLoader(yaml.SafeLoader):  # type: ignore[name-defined, misc]
        pass

    StrictLoader.yaml_implicit_resolvers = {k: [r for r in v if r[0] != "tag:yaml.org,2002:bool"] for k, v in yaml.SafeLoader.yaml_implicit_resolvers.items()}
    StrictLoader.add_implicit_resolver("tag:yaml.org,2002:bool", BOOL, list("tTfF"))

    def construct_mapping(loader: Any, node: Any, deep: bool = False) -> Any:
        seen: set[Any] = set()
        for key_node, _value in node.value:
            if key_node.tag == "tag:yaml.org,2002:merge":
                continue
            key = loader.construct_object(key_node, deep=True)
            try:
                repeated = key in seen
            except TypeError:
                raise yaml.constructor.ConstructorError(None, None, "a mapping key that cannot be compared", key_node.start_mark) from None
            if repeated:
                raise yaml.constructor.ConstructorError(None, None, f"the key {key!r} is repeated in one mapping", key_node.start_mark)
            seen.add(key)
        return yaml.SafeLoader.construct_mapping(loader, node, deep)

    StrictLoader.add_constructor("tag:yaml.org,2002:map", construct_mapping)
    return StrictLoader



# (workflow file, permission) -> why the workflow needs to write it. Both file their own issues with the workflow token and read no secret.
ALLOW_WRITE = {
    ("bench-nightly.yml", "issues"): "files one regression issue per failing benchmark case, de-duplicated by case ID",
    ("main-merge-audit.yml", "issues"): "opens the governance issue for an unreviewed merge to main",
}
VALUES = {"read", "write", "none"}


def documents(text: str) -> list[Any]:
    return list(yaml.load_all(text, Loader=make_loader()))  # SafeLoader subclass: no object construction beyond plain data


def keyed(node: dict[Any, Any], name: str) -> list[Any]:
    """The values of the keys of a mapping that read as `name` in any capitalisation (GitHub reads keys case-insensitively in places; a stricter read costs nothing)."""
    return [v for k, v in node.items() if isinstance(k, str) and k.lower() == name]


def check_block(where: str, block: Any, top: bool, name: str, problems: list[str], used: set[tuple[str, str]]) -> None:
    if not isinstance(block, dict):
        problems.append(f"{name}: {where} `permissions: {block!r}` must be a mapping of read, write or none (or `{{}}`)")
        return
    for permission, value in block.items():
        label = f"`{permission}: {value}`"
        if not isinstance(permission, str):
            problems.append(f"{name}: {where} a permission name that is not a string")
        elif permission.lower() == "id-token":
            problems.append(f"{name}: {where} {label}: `id-token` is not allowed")
        elif value not in VALUES:
            problems.append(f"{name}: {where} {label} is not read, write or none")
        elif value == "write":
            if (name, permission) in ALLOW_WRITE:
                used.add((name, permission))
            else:
                problems.append(f"{name}: {where} `{permission}: write` is not in ALLOW_WRITE for this workflow")
        if top and isinstance(permission, str) and permission.lower() == "contents" and value in VALUES and value not in {"read", "none"}:
            problems.append(f"{name}: {where} `contents` must be read or none")


def triggers(doc: dict[Any, Any]) -> list[str]:
    found: list[str] = []
    for value in keyed(doc, "on"):
        if isinstance(value, str):
            found.append(value)
        elif isinstance(value, list):
            found += [v for v in value if isinstance(v, str)]
        elif isinstance(value, dict):
            found += [k for k in value if isinstance(k, str)]
    return found


def scan(name: str, text: str) -> tuple[list[str], set[tuple[str, str]]]:
    problems: list[str] = []
    used: set[tuple[str, str]] = set()
    try:
        docs = documents(text)
    except yaml.YAMLError as exc:
        mark = getattr(exc, "problem_mark", None)
        return [f"{name}: cannot be parsed ({getattr(exc, 'problem', 'invalid YAML')}{', line ' + str(mark.line + 1) if mark else ''})"], used
    if len(docs) != 1 or not isinstance(docs[0], dict):
        return [f"{name}: a workflow must be exactly one YAML mapping"], used
    doc = docs[0]
    top = keyed(doc, "permissions")
    if len(top) != 1:
        problems.append(f"{name}: no top-level `permissions` mapping (the repository default may be read-write)")
    for block in top:
        check_block("top-level", block, True, name, problems, used)
    jobs = keyed(doc, "jobs")
    for container in jobs:
        for job_id, job in (container.items() if isinstance(container, dict) else []):
            if isinstance(job, dict):
                for block in keyed(job, "permissions"):
                    check_block(f"job `{job_id}`", block, False, name, problems, used)
    if any(t.lower() == "pull_request_target" for t in triggers(doc)):
        problems.append(f"{name}: the `pull_request_target` trigger runs fork code with a write token")
    return problems, used


def violations(directory: Path) -> list[str]:
    if yaml is None:
        return ["PyYAML is not installed: the check cannot read the workflow files (the safeguards job installs tools/ci/requirements.lock)"]
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
    found = violations(directory)
    for item in found:
        print(f"workflow-permissions FAIL: {item}")
    if found:
        return 1
    print("workflow-permissions OK: every workflow narrows its token; the only write grants are the two reviewed ones")
    return 0


if __name__ == "__main__":
    sys.exit(main())
