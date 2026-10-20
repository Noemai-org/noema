#!/usr/bin/env python3
"""Secret-name check (vendor register; AGENTS.md section 10).

Every secret a workflow reads (`secrets.NAME` or `secrets['NAME']`) must be listed by name in `.env.example`, the one place
that names every secret the system may use. `GITHUB_TOKEN`, which GitHub provides, is the only exception. A line in
`.env.example` that ends its name with `*` (for example `TUSHE_ORACLE_*=`) covers every name with that prefix. A secret
looked up by an expression cannot be checked, so it fails. Comments are ignored.

Usage: check_secret_refs.py [workflows_dir] [env_example]
"""
import re
import sys
from pathlib import Path

BUILT_IN = {"GITHUB_TOKEN"}
DOT = re.compile(r"\bsecrets\.([A-Za-z_][A-Za-z0-9_]*)")
INDEX = re.compile(r"\bsecrets\[\s*(['\"])([^'\"]+)\1\s*\]")
ANY_INDEX = re.compile(r"\bsecrets\[")
LINE = re.compile(r"^([A-Z][A-Z0-9_]*\*?)=")


def declared(env_example: Path) -> tuple[set[str], list[str]]:
    exact, prefixes = set(), []
    for line in env_example.read_text().splitlines():
        m = LINE.match(line.strip())
        if not m:
            continue
        name = m.group(1)
        if name.endswith("*"):
            prefixes.append(name[:-1])
        else:
            exact.add(name)
    return exact, prefixes


def violations(workflows: Path, env_example: Path) -> list[str]:
    exact, prefixes = declared(env_example)
    found = []
    for path in sorted(workflows.glob("*.y*ml")):
        for number, line in enumerate(path.read_text().splitlines(), 1):
            code = line.split("#", 1)[0]
            names = [m.group(1) for m in DOT.finditer(code)] + [m.group(2) for m in INDEX.finditer(code)]
            if ANY_INDEX.search(code) and not INDEX.search(code):
                found.append(f"{path.name}:{number}: a secret is looked up by an expression, which cannot be checked")
            for name in names:
                if name in BUILT_IN or name in exact or any(name.startswith(p) for p in prefixes):
                    continue
                found.append(f"{path.name}:{number}: secret '{name}' is not listed in {env_example.name}")
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
