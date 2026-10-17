"""Python architecture rules (PLT-03). Standard library only: ast and tomllib.

Each function returns a list of violations: (rule, file, line, message).
"""
import ast
import re
import tomllib
from pathlib import Path

Violation = tuple[str, str, int, str]

SKIP = {".git", "node_modules", ".venv", "__pycache__"}
MONEY_NAME = re.compile(r"^(amount.*|.*_minor|.*minor)$", re.I)
BAD_MONEY_TYPE = re.compile(r"\b(float|Decimal)\b")
DB_AND_TEMPORAL = {
    "psycopg", "psycopg2", "psycopg2-binary", "psycopg-binary", "asyncpg", "sqlalchemy", "sqlmodel", "pymysql", "mysqlclient",
    "mysql-connector-python", "aiomysql", "pg8000", "databases", "peewee", "tortoise-orm", "pymongo", "motor", "alembic", "temporalio",
}
MOCK_MODULES = {"unittest.mock", "mock", "pytest_mock", "responses", "httpretty", "requests_mock", "respx", "moto"}


def _files(root: Path, base: str, suffix: str = ".py"):
    start = root / base
    if not start.exists():
        return
    for path in sorted(start.rglob(f"*{suffix}")):
        rel = path.relative_to(root)
        if SKIP.intersection(rel.parts) or str(rel).startswith("tools/lint/canaries"):
            continue
        yield path, rel.as_posix()


def _imports(tree: ast.AST):
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                yield alias.name, node.lineno
        elif isinstance(node, ast.ImportFrom) and node.module:
            yield node.module, node.lineno
            for alias in node.names:
                yield f"{node.module}.{alias.name}", node.lineno


def clock(root: Path) -> list[Violation]:
    """datetime.now and friends only inside the clock module (INV-21)."""
    out = []
    for path, rel in _files(root, "services/model"):
        if "platform/clock" in rel:
            continue
        for node in ast.walk(ast.parse(path.read_text())):
            if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr in {"now", "utcnow", "today"}:
                owner = ast.unparse(node.func.value)
                if owner in {"datetime", "date", "datetime.datetime", "datetime.date"}:
                    out.append(("py-clock", rel, node.lineno, f"{owner}.{node.func.attr}() outside the clock module"))
    return out


def money(root: Path) -> list[Violation]:
    """No float or Decimal on money fields (BD-08)."""
    out = []

    def check(name: str, annotation: ast.AST | None, line: int, rel: str, owner: str = "") -> None:
        if annotation is not None and BAD_MONEY_TYPE.search(ast.unparse(annotation)):
            if MONEY_NAME.match(name) or (owner == "Quantity" and name == "value"):
                out.append(("py-money-field", rel, line, f"money field '{name}' is annotated {ast.unparse(annotation)}"))

    for path, rel in _files(root, "services/model"):
        tree = ast.parse(path.read_text())
        for node in ast.walk(tree):
            if isinstance(node, ast.ClassDef):
                for stmt in node.body:
                    if isinstance(stmt, ast.AnnAssign) and isinstance(stmt.target, ast.Name):
                        check(stmt.target.id, stmt.annotation, stmt.lineno, rel, node.name)
            elif isinstance(node, ast.AnnAssign) and isinstance(node.target, ast.Name):
                check(node.target.id, node.annotation, node.lineno, rel)
            elif isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                for arg in node.args.args + node.args.kwonlyargs:
                    check(arg.arg, arg.annotation, arg.lineno, rel)
    return out


def cross_service(root: Path) -> list[Violation]:
    """The model service never references the core, and neither Dockerfile reaches outside its context."""
    out = []
    for path, rel in _files(root, "services/model"):
        for node in ast.walk(ast.parse(path.read_text())):
            if isinstance(node, ast.Constant) and isinstance(node.value, str) and "services/core" in node.value:
                out.append(("py-cross-service", rel, node.lineno, f"model service references the core: {node.value!r}"))
        for module, line in _imports(ast.parse(path.read_text())):
            if module.startswith("github.com") or "services.core" in module:
                out.append(("py-cross-service", rel, line, f"model service imports {module}"))
    for service, other in (("core", "model"), ("model", "core")):
        dockerfile = root / "services" / service / "Dockerfile"
        if dockerfile.exists():
            for number, line in enumerate(dockerfile.read_text().splitlines(), 1):
                code = line.split("#", 1)[0]
                if "../" in code or f"services/{other}" in code:
                    out.append(("dockerfile-cross-service", f"services/{service}/Dockerfile", number, "reaches outside its own build context"))
    return out


def manifest(root: Path) -> list[Violation]:
    """No DB driver or Temporal client in the model manifest (INV-29, PERD §41.1)."""
    out = []
    base = root / "services" / "model"
    names: list[tuple[str, str, int]] = []
    pyproject = base / "pyproject.toml"
    if pyproject.exists():
        project = tomllib.loads(pyproject.read_text()).get("project", {})
        deps = list(project.get("dependencies", []))
        for group in project.get("optional-dependencies", {}).values():
            deps += group
        names += [(re.split(r"[=<>!~\[; ]", d, 1)[0], "services/model/pyproject.toml", 0) for d in deps]
    for lock in sorted(base.glob("requirements*.lock")) if base.exists() else []:
        for number, line in enumerate(lock.read_text().splitlines(), 1):
            m = re.match(r"^([A-Za-z0-9_.-]+)==", line)
            if m:
                names.append((m.group(1), f"services/model/{lock.name}", number))
    for name, rel, line in names:
        if name.lower().replace("_", "-") in DB_AND_TEMPORAL:
            out.append(("manifest-model-db", rel, line, f"'{name}' is a DB driver or Temporal client; the model service holds none"))
    return out


def mocks(root: Path) -> list[Violation]:
    """No mock library in benchmark or integration packages (AGENT-12, REQ-TST-004)."""
    out = []
    for path, rel in _files(root, "."):
        if not (rel.startswith("benchmark/") or "/integration/" in "/" + rel):
            continue
        for module, line in _imports(ast.parse(path.read_text())):
            if module in MOCK_MODULES or module.split(".")[0] in {"mock", "pytest_mock", "responses", "httpretty", "requests_mock", "respx", "moto"}:
                out.append(("mock-library", rel, line, f"mock library '{module}'"))
    return out


ALL = (clock, money, cross_service, manifest, mocks)
