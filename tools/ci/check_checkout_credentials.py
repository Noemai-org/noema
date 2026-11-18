#!/usr/bin/env python3
"""No checkout step leaves a credential on disk (PLT-05, REQ-SEC-005, REQ-SEC-007).

`actions/checkout` writes the job's token into `.git/config` unless the step says `persist-credentials: false`. Every later step of the job, and every script or test the job runs
(a benchmark, a model adapter, a build script), can then read that token from the workspace. A token that nothing needs should not be there. This check fails when any `actions/checkout`
step under `.github/` (workflows and composite actions) does not set `persist-credentials: false` in its own `with:` mapping. The value must be the boolean `false` or the string "false":
an expression, `true`, `no`, a missing mapping or a comment that only mentions the key does not count. A file that cannot be read, parsed or that repeats a key fails.

The files are PARSED, not scanned by line (A283, OD-P-95 D1): a checkout written in a flow mapping, with quoted or escaped keys, an anchor or a quoted `#` is the same document whatever its
layout. PyYAML is the one approved parser (hash-pinned in tools/ci/requirements.lock, installed in the safeguards job only); the loader is `yaml.SafeLoader` with two changes that only
refuse more: a repeated key is an error (GitHub refuses it, and a last-key-wins read could hide a `true`), and a boolean is only true or false in any capitalisation (YAML 1.2, as GitHub reads it:
`no` and `off` are strings, not false). Without PyYAML the check fails closed.

A job that really needs git credentials (a push, a fetch of a private repository) is not allowed to rely on the persisted one: it passes the token to the step that needs it. There is no
exemption list: a new one would be a reviewed change of this file.

Usage: check_checkout_credentials.py [github_dir]
"""
import os
import re
import sys
from pathlib import Path
from typing import Any

try:
    import yaml
except ImportError:  # fail closed: a check that cannot read the files must not pass them
    yaml = None  # type: ignore[assignment]

SUFFIXES = {".yml", ".yaml"}
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


def documents(text: str) -> list[Any]:
    return list(yaml.load_all(text, Loader=make_loader()))  # SafeLoader subclass: no object construction beyond plain data


def is_checkout(uses: Any) -> bool:
    return isinstance(uses, str) and uses.strip().lower().startswith("actions/checkout")


def sets_false(step: dict[Any, Any]) -> bool:
    """True when the step's own `with` mapping carries persist-credentials as false (the key in any capitalisation, once, with the boolean or the string "false")."""
    withs = [v for k, v in step.items() if isinstance(k, str) and k.lower() == "with"]
    if len(withs) != 1 or not isinstance(withs[0], dict):
        return False
    values = [v for k, v in withs[0].items() if isinstance(k, str) and k.lower() == "persist-credentials"]
    return len(values) == 1 and (values[0] is False or values[0] == "false")


def walk(node: Any, path: str, found: list[str], depth: int = 0) -> None:
    if depth > MAX_DEPTH:
        found.append(f"{path}: nested deeper than {MAX_DEPTH}: this check does not read it")
        return
    if isinstance(node, dict):
        uses = [v for k, v in node.items() if isinstance(k, str) and k.lower() == "uses"]
        if any(is_checkout(u) for u in uses) and not sets_false(node):
            name = next((v for k, v in node.items() if isinstance(k, str) and k.lower() == "name" and isinstance(v, str)), "")
            found.append(f"{path}{' (' + name[:60] + ')' if name else ''}: actions/checkout without `persist-credentials: false` in its own `with:` mapping")
        for key, value in node.items():
            walk(value, f"{path}.{key}" if path else str(key), found, depth + 1)
    elif isinstance(node, list):
        for index, item in enumerate(node):
            walk(item, f"{path}[{index}]", found, depth + 1)


def violations(github_dir: Path) -> list[str]:
    found: list[str] = []
    if yaml is None:
        return ["PyYAML is not installed: the check cannot read the workflow files (the safeguards job installs tools/ci/requirements.lock)"]

    def on_error(exc: OSError) -> None:
        found.append(f"{exc.filename}: directory cannot be listed ({exc.__class__.__name__})")

    files = sorted(Path(d) / n for d, _dirs, names in os.walk(github_dir, onerror=on_error) for n in names)
    for path in (p for p in files if p.suffix.lower() in SUFFIXES):
        rel = path.relative_to(github_dir).as_posix()
        try:
            docs = documents(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError) as exc:
            found.append(f"{rel}: cannot be read ({exc.__class__.__name__})")
            continue
        except yaml.YAMLError as exc:
            mark = getattr(exc, "problem_mark", None)
            found.append(f"{rel}: cannot be parsed ({getattr(exc, 'problem', 'invalid YAML')}{', line ' + str(mark.line + 1) if mark else ''})")
            continue
        problems: list[str] = []
        for number, doc in enumerate(docs):
            walk(doc, f"document {number}" if len(docs) > 1 else "", problems)
        found += [f"{rel}: {p.lstrip(':').strip()}" if p.startswith(":") else f"{rel}: {p}" for p in problems]
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
