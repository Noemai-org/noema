#!/usr/bin/env python3
"""No checkout step leaves a credential on disk (PLT-05, REQ-SEC-005, REQ-SEC-007).

`actions/checkout` writes the job's token into `.git/config` unless the step says `persist-credentials: false`. Every later step of the job, and every script or test the job runs
(a benchmark, a model adapter, a build script), can then read that token from the workspace. A token that nothing needs should not be there. This check fails when any `actions/checkout`
step under `.github/` (workflows and composite actions) does not set `persist-credentials: false` in its own `with:` block.

The files are read as PARSED YAML (tools/ci/yamlload.py: PyYAML safe_load, OD-P-95, ruling A283), so no spelling hides a step: a quoted key, a flow mapping, a JSON-style step, a space before
the colon and an escaped key are the same document as the plain form. A file that is not plain YAML (an anchor, an alias, a repeated key, a second document, an unreadable file) fails.
The value must be the boolean `false` (the string "false" is the same input to the action and is accepted); an expression, `true`, any other value or a missing key does not count. The input
name is matched without regard to case, as the runner does, and a step that sets it twice must set it to false both times. An `INPUT_PERSIST-CREDENTIALS` environment variable on the step is
refused: it would override the input.

A job that really needs git credentials (a push, a fetch of a private repository) is not allowed to rely on the persisted one: it passes the token to the step that needs it. There is no
exemption list: a new one would be a reviewed change of this file.

Usage: check_checkout_credentials.py [github_dir]
"""
import os
import sys
from pathlib import Path
from typing import Any, Iterator

sys.path.insert(0, str(Path(__file__).resolve().parent))
import yamlload  # noqa: E402

SUFFIXES = {".yml", ".yaml"}
INPUT = "persist-credentials"


def checkout_steps(node: Any, path: str = "") -> Iterator[tuple[str, dict]]:
    """Every mapping of the document whose `uses` is actions/checkout, with where it is. A step is found wherever it is written (a job, a composite action, a reusable workflow)."""
    if isinstance(node, dict):
        uses = node.get("uses")
        if isinstance(uses, str) and uses.split("@", 1)[0].strip().lower() == "actions/checkout":
            yield path or "(root)", node
        for key, value in node.items():
            yield from checkout_steps(value, f"{path}.{key}" if path else str(key))
    elif isinstance(node, list):
        for index, value in enumerate(node):
            yield from checkout_steps(value, f"{path}[{index}]")


def is_false(value: Any) -> bool:
    return value is False or (isinstance(value, str) and value in {"false", "False", "FALSE"})


def step_problem(step: dict) -> str | None:
    """Why this checkout step leaves a credential, or None when it does not."""
    with_block = step.get("with")
    if not isinstance(with_block, dict):
        return "actions/checkout without a `with:` block that sets `persist-credentials: false`"
    values = [v for k, v in with_block.items() if isinstance(k, str) and k.lower() == INPUT]
    if not values:
        return "actions/checkout without `persist-credentials: false` in its own `with:` block"
    if not all(is_false(v) for v in values):
        return "actions/checkout with `persist-credentials` set to something other than false"
    env = step.get("env")
    if isinstance(env, dict) and any(str(k).strip().lower().replace("-", "_") == "input_persist_credentials" for k in env):
        return "actions/checkout with an INPUT_PERSIST-CREDENTIALS environment variable, which overrides the input"
    return None


def violations(github_dir: Path) -> list[str]:
    found: list[str] = []

    def on_error(exc: OSError) -> None:
        found.append(f"{exc.filename}: directory cannot be listed ({exc.__class__.__name__})")

    files = sorted(Path(d) / n for d, _dirs, names in os.walk(github_dir, onerror=on_error) for n in names)
    for path in (p for p in files if p.suffix.lower() in SUFFIXES):
        label = path.relative_to(github_dir).as_posix()
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError) as exc:
            found.append(f"{label}: cannot be read ({exc.__class__.__name__})")
            continue
        try:
            document = yamlload.load(text, label)
        except yamlload.YamlRefused as exc:
            found.append(str(exc))
            continue
        for where, step in checkout_steps(document):
            problem = step_problem(step)
            if problem:
                name = step.get("name")
                found.append(f"{label}: {where}{f' ({name})' if isinstance(name, str) else ''}: {problem}")
    return found


def main() -> int:
    github_dir = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[2] / ".github"
    if not github_dir.is_dir():
        print(f"checkout-credentials FAIL: {github_dir} is not a directory")
        return 1
    if not yamlload.available():
        print("checkout-credentials FAIL: PyYAML is not installed (pip install --require-hashes -r tools/ci/requirements.lock)")
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
