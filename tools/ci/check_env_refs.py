#!/usr/bin/env python3
"""Environment-reference check (PLT-02; DEC-2026-10-13 R3).

Each workflow may reference only its own area's GitHub Environment.
Comments are ignored. Usage: check_env_refs.py [workflows_dir]
"""
import re
import sys
from pathlib import Path

ALL_ENVS = {"core-ci", "model-ci", "bench"}
# workflow file name -> the environments it may reference. Any other workflow may reference none.
ALLOWED = {"core.yml": {"core-ci"}, "model.yml": {"model-ci"}}


def allowed_for(name: str) -> set[str]:
    if name.startswith("bench"):
        return {"bench"}
    return ALLOWED.get(name, set())


def violations(directory: Path) -> list[str]:
    found = []
    for path in sorted(directory.glob("*.y*ml")):
        allowed = allowed_for(path.name)
        for number, line in enumerate(path.read_text().splitlines(), 1):
            code = line.split("#", 1)[0]
            for env in ALL_ENVS - allowed:
                if re.search(rf"(?<![\w-]){re.escape(env)}(?![\w-])", code):
                    found.append(f"{path.name}:{number}: references environment '{env}' of another area")
    return found


def main() -> int:
    directory = Path(sys.argv[1] if len(sys.argv) > 1 else ".github/workflows")
    found = violations(directory)
    for item in found:
        print("env-refs FAIL: " + item)
    if not found:
        print("env-refs OK")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
