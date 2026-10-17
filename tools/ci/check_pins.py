#!/usr/bin/env python3
"""Dependency-pinning check (PLT-02).

Go: a go.mod that requires modules needs a go.sum. Python: direct dependencies use '==' and every
lock entry carries hashes. Node: exact versions and a package-lock.json. Docker: every base image
carries a digest. GitHub Actions: every 'uses:' is a full commit SHA. Usage: check_pins.py [root]
"""
import json
import re
import sys
import tomllib
from pathlib import Path

SKIP_DIRS = {"node_modules", ".venv", ".git", "canaries"}
EXACT = re.compile(r"^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$")


def walk(root: Path, pattern: str):
    for path in sorted(root.rglob(pattern)):
        if not SKIP_DIRS.intersection(path.relative_to(root).parts):
            yield path


def check_go(root: Path) -> list[str]:
    found = []
    for mod in walk(root, "go.mod"):
        text = mod.read_text()
        if re.search(r"^\s*(require\b|[\w./-]+\s+v\d)", text, re.M) and not mod.with_name("go.sum").exists():
            found.append(f"{mod.relative_to(root)}: requires modules but has no go.sum")
    return found


def check_python(root: Path) -> list[str]:
    found = []
    for pyproject in walk(root, "pyproject.toml"):
        data = tomllib.loads(pyproject.read_text())
        project = data.get("project", {})
        deps = list(project.get("dependencies", []))
        for group in project.get("optional-dependencies", {}).values():
            deps += group
        for dep in deps:
            if "==" not in dep:
                found.append(f"{pyproject.relative_to(root)}: '{dep}' is not pinned with '=='")
    for lock in walk(root, "requirements*.lock"):
        logical = re.sub(r"\\\n", " ", lock.read_text())
        for line in logical.splitlines():
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "==" not in line or "--hash=sha256:" not in line:
                found.append(f"{lock.relative_to(root)}: entry without version pin and hashes: {line[:60]}")
    return found


def check_node(root: Path) -> list[str]:
    found = []
    for pkg in walk(root, "package.json"):
        data = json.loads(pkg.read_text())
        for group in ("dependencies", "devDependencies", "optionalDependencies"):
            for name, version in data.get(group, {}).items():
                if not EXACT.match(version):
                    found.append(f"{pkg.relative_to(root)}: {name}@{version} is not an exact version")
        if (data.get("dependencies") or data.get("devDependencies")) and not pkg.with_name("package-lock.json").exists():
            found.append(f"{pkg.relative_to(root)}: no package-lock.json")
    return found


def check_docker(root: Path) -> list[str]:
    found = []
    for dockerfile in walk(root, "Dockerfile*"):
        stages = set()
        for number, line in enumerate(dockerfile.read_text().splitlines(), 1):
            m = re.match(r"\s*FROM\s+(?:--\S+\s+)?(\S+)(?:\s+AS\s+(\S+))?", line, re.I)
            if not m:
                continue
            image, alias = m.groups()
            if image not in stages and image != "scratch" and "@sha256:" not in image:
                found.append(f"{dockerfile.relative_to(root)}:{number}: base image '{image}' has no digest")
            if alias:
                stages.add(alias)
    return found


def check_actions(root: Path) -> list[str]:
    found = []
    for workflow in walk(root / ".github", "*.y*ml") if (root / ".github").exists() else []:
        for number, line in enumerate(workflow.read_text().splitlines(), 1):
            m = re.match(r"\s*-?\s*uses:\s*(\S+)", line)
            if m and not m.group(1).startswith("./") and not re.search(r"@[0-9a-f]{40}$", m.group(1)):
                found.append(f"{workflow.relative_to(root)}:{number}: '{m.group(1)}' is not pinned to a commit SHA")
    return found


def main() -> int:
    root = Path(sys.argv[1] if len(sys.argv) > 1 else ".")
    found = check_go(root) + check_python(root) + check_node(root) + check_docker(root) + check_actions(root)
    for item in found:
        print("pins FAIL: " + item)
    if not found:
        print("pins OK")
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())
