#!/usr/bin/env python3
"""Secret-name check (vendor register; AGENTS.md section 10). Needs PyYAML (tools/ci/requirements.lock).

Every secret a workflow reads (`secrets.NAME` or `secrets['NAME']` inside `${{ }}` or an `if:`) must be listed by name in
`.env.example`, the one place that names every secret the system may use. `GITHUB_TOKEN`, which GitHub provides, is the
only exception. A line in `.env.example` that ends its name with `*` (`TUSHE_ORACLE_*=`) covers every name with that prefix,
but listing a prefix there never grants permission to use it: a protected prefix (the oracle and fixture credentials) may
be read only by a job that declares `environment: bench` (and nothing else under `environment`), in a workflow file that
check_env_refs.py allows to use `bench`. Job IDs are free text and prove nothing.

The workflow is parsed, not scanned. The check fails closed. In any job that is not permitted, and in workflow-level keys, it
rejects: a protected secret by dot or bracket form; a secret looked up by an expression; the whole context (`toJSON(secrets)`,
`${{ secrets }}`); and `secrets: inherit`, which hands every secret to the callee. A file it cannot parse is a failure.

Usage: check_secret_refs.py [workflows_dir] [env_example]
"""
import re
import sys
from pathlib import Path

import yaml

BUILT_IN = {"GITHUB_TOKEN"}
PROTECTED = {"TUSHE_ORACLE_": "bench", "TUSHE_FIXTURE_": "bench"}  # prefix -> the one GitHub Environment allowed to read it
EXPR = re.compile(r"\$\{\{(.*?)\}\}", re.S)
DOT = re.compile(r"\bsecrets\.([A-Za-z_][A-Za-z0-9_-]*)")
INDEX = re.compile(r"\bsecrets\[\s*(['\"])([^'\"]*)\1\s*\]")
ANY_INDEX = re.compile(r"\bsecrets\s*\[")
WHOLE = re.compile(r"\bsecrets\b(?!\s*[.\[])")
LINE = re.compile(r"^([A-Z][A-Z0-9_]*\*?)=")


def declared(env_example: Path) -> tuple[set[str], list[str]]:
    exact, prefixes = set(), []
    for line in env_example.read_text().splitlines():
        m = LINE.match(line.strip())
        if m:
            name = m.group(1)
            (prefixes.append(name[:-1]) if name.endswith("*") else exact.add(name))
    return exact, prefixes


def strings(node, key=None):
    """Every string in a parsed document, with the key it sits under. Keys are strings too."""
    if isinstance(node, str):
        yield key, node
    elif isinstance(node, dict):
        for k, v in node.items():
            yield from strings(k, None)
            yield from strings(v, k)
    elif isinstance(node, list):
        for item in node:
            yield from strings(item, key)


def expressions(key, text):
    """The expression sources in a string: each `${{ }}` body, and the whole value of an `if:`."""
    found = [m.group(1) for m in EXPR.finditer(text)]
    if key == "if":
        found.append(text)
    return found


def job_permitted(job, protected_env: str, workflow_name: str) -> bool:
    if not isinstance(job, dict) or "uses" in job:
        return False
    env = job.get("environment")
    named = env if isinstance(env, str) else (env.get("name") if isinstance(env, dict) and set(env) <= {"name"} else None)
    return named == protected_env and workflow_name.startswith(protected_env)


def scan(label: str, node, permitted: bool, protected_env: str, exact, prefixes, where: str) -> list[str]:
    found = []
    if isinstance(node, dict) and node.get("secrets") == "inherit" and not permitted:
        found.append(f"{where}: {label}: `secrets: inherit` passes every secret to the callee")
    for key, text in strings(node):
        for expr in expressions(key, text):
            names = [m.group(1) for m in DOT.finditer(expr)] + [m.group(2) for m in INDEX.finditer(expr)]
            if len(ANY_INDEX.findall(expr)) > len(INDEX.findall(expr)) and not permitted:
                found.append(f"{where}: {label}: a secret is looked up by an expression, which cannot be checked")
            if WHOLE.search(expr) and not permitted:
                found.append(f"{where}: {label}: the whole secrets context is used, which exposes every secret")
            for name in names:
                upper = name.upper()
                if upper in BUILT_IN:
                    continue
                if upper not in exact and not any(upper.startswith(p) for p in prefixes):
                    found.append(f"{where}: {label}: secret '{name}' is not listed in .env.example")
                    continue
                for prefix, env in PROTECTED.items():
                    if upper.startswith(prefix) and not (permitted and env == protected_env):
                        found.append(f"{where}: {label}: '{name}' is a bench-only secret and this is not a job with `environment: bench`")
    return found


def violations(workflows: Path, env_example: Path) -> list[str]:
    exact, prefixes = declared(env_example)
    exact, prefixes = {e.upper() for e in exact}, [p.upper() for p in prefixes]
    found = []
    for path in sorted(workflows.glob("*.y*ml")):
        try:
            doc = yaml.safe_load(path.read_text())
        except yaml.YAMLError as exc:
            found.append(f"{path.name}: cannot be parsed ({type(exc).__name__}); failing closed")
            continue
        if not isinstance(doc, dict) or not isinstance(doc.get("jobs"), dict):
            found.append(f"{path.name}: no `jobs` mapping; failing closed")
            continue
        top = {k: v for k, v in doc.items() if k != "jobs"}
        found += scan("workflow level", top, False, "bench", exact, prefixes, path.name)
        for job_id, job in doc["jobs"].items():
            ok = job_permitted(job, "bench", path.name)
            found += scan(f"job '{job_id}'", job, ok, "bench", exact, prefixes, path.name)
    return found


def main() -> int:
    workflows = Path(sys.argv[1] if len(sys.argv) > 1 else ".github/workflows")
    env_example = Path(sys.argv[2] if len(sys.argv) > 2 else ".env.example")
    if not env_example.exists():
        print(f"secret-refs FAIL: {env_example} does not exist")
        return 1
    found = violations(workflows, env_example)
    for item in found:
        print("secret-refs FAIL: " + item)
    if not found:
        print("secret-refs OK")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
