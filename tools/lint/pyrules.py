"""Python architecture rules (PLT-03). Standard library only: ast and tomllib.

Each function returns a list of violations: (rule, file, line, message).
"""
import ast
import math
import re
import tomllib
from pathlib import Path

Violation = tuple[str, str, int, str]

SKIP = {".git", "node_modules", ".venv", "__pycache__"}
MONEY_NAME = re.compile(r"^(amount.*|.*_minor|.*minor)$", re.I)
BAD_MONEY_TYPE = re.compile(r"\b(float|Decimal)\b")
SQL_BAD_TYPE = r"(?:numeric|decimal|real|double\s+precision|float[48]?|money)"
SQL_MONEY_COLUMN = re.compile(rf"\b(amount\w*|\w*minor)\b\s+(?:set\s+data\s+)?(?:type\s+)?{SQL_BAD_TYPE}\b", re.I)
SQL_MONEY_CAST = re.compile(rf"\b(amount\w*|\w*minor)\b\s*\)?\s*::\s*{SQL_BAD_TYPE}\b", re.I)
_NOT_AS = r"(?:(?!\bas\b)[^()])*?"
SQL_MONEY_CAST_FN = re.compile(rf"\bcast\s*\(\s*\(*{_NOT_AS}\b(amount\w*|\w*minor)\b{_NOT_AS}\)*\s+as\s+{SQL_BAD_TYPE}\b", re.I)
SQL_MONEY_TYPE = re.compile(r"\b\w+\s+money\b(?!\s*[.(])", re.I)
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


MODEL_PKG = "services/model/noema_model"
HTTP_CLIENTS = {"requests", "httpx", "aiohttp", "urllib3", "urllib.request", "http.client", "websockets", "grpc", "socket"}
AGENT_FRAMEWORKS = {"langgraph", "langchain", "langchain_core", "autogen", "crewai", "llama_index", "haystack", "semantic_kernel", "smolagents"}
MUTABLE_CALLS = {"list", "dict", "set", "defaultdict", "deque", "Counter", "OrderedDict", "bytearray"}


def _imports_providers(node: ast.AST) -> list[tuple[str, int]]:
    """Every import in the tree that reaches the providers package, absolute or relative."""
    found = []
    for n in ast.walk(node):
        if isinstance(n, ast.Import):
            found += [(a.name, n.lineno) for a in n.names if "providers" in a.name.split(".")]
        elif isinstance(n, ast.ImportFrom):
            module = n.module or ""
            parts = module.split(".") if module else []
            if "providers" in parts:
                found.append((("." * n.level) + module, n.lineno))
            elif any(a.name == "providers" for a in n.names):
                found.append((("." * n.level) + module + ".providers", n.lineno))
    return found


def _speech_module(module: str) -> bool:
    """True for `providers.speech` or something under it, and for nothing else in the providers package."""
    parts = module.lstrip(".").split(".")
    return "providers" in parts and parts[parts.index("providers") + 1:][:1] == ["speech"]


def harness_only_providers(root: Path) -> list[Violation]:
    """Only the harness imports the providers package (AGENT-28, PERD §19.1). The providers package's own modules and
    the service's tests are not production importers and are not scanned."""
    out = []
    for path, rel in _files(root, MODEL_PKG):
        if rel.startswith(f"{MODEL_PKG}/harness/") or rel.startswith(f"{MODEL_PKG}/providers/"):
            continue
        for module, line in _imports_providers(ast.parse(path.read_text())):
            if rel.startswith(f"{MODEL_PKG}/voice/") and _speech_module(module):
                continue  # A9: the voice pipeline may import providers.speech, and only that
            out.append(("harness-only-providers", rel, line, f"imports {module}; only noema_model/harness may import providers"))
    return out


def _mutable_value(value: ast.AST | None) -> bool:
    if isinstance(value, (ast.List, ast.Dict, ast.Set, ast.ListComp, ast.DictComp, ast.SetComp)):
        return True
    return isinstance(value, ast.Call) and isinstance(value.func, (ast.Name, ast.Attribute)) and (
        (value.func.id if isinstance(value.func, ast.Name) else value.func.attr) in MUTABLE_CALLS)


def pipeline_purity(root: Path) -> list[Violation]:
    """The pipeline is stateless pure functions (D3): no module-level mutable state, no global statement, and no
    database, Temporal, HTTP or stateful agent-framework import. It reaches the world only through the harness and
    the core Evidence API client."""
    out = []
    banned = {n.lower().replace("_", "-") for n in DB_AND_TEMPORAL}
    for path, rel in _files(root, f"{MODEL_PKG}/understanding/pipeline"):
        tree = ast.parse(path.read_text())
        for stmt in tree.body:
            if isinstance(stmt, (ast.Assign, ast.AnnAssign, ast.AugAssign)) and _mutable_value(getattr(stmt, "value", None)):
                out.append(("pipeline-purity", rel, stmt.lineno, "module-level mutable state; use a tuple, a frozenset or MappingProxyType"))
        for node in ast.walk(tree):
            if isinstance(node, ast.Global):
                out.append(("pipeline-purity", rel, node.lineno, "global statement"))
        for module, line in _imports(tree):
            top = module.split(".")[0]
            if top.lower().replace("_", "-") in banned or module in HTTP_CLIENTS or top in HTTP_CLIENTS or top in AGENT_FRAMEWORKS:
                out.append(("pipeline-purity", rel, line, f"imports {module}: a database, Temporal, HTTP or agent-framework client"))
    return out


def _power_of_ten(node: ast.AST) -> bool:
    """A literal 10, 100, 1000 ... (also 1e2 or 100.0), 10 ** n, or pow(10, n): a literal exponent multiplier."""
    if isinstance(node, ast.Constant) and isinstance(node.value, (int, float)) and not isinstance(node.value, bool):
        v = abs(float(node.value))
        return v >= 10 and abs(math.log10(v) - round(math.log10(v))) < 1e-9
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Pow):
        return isinstance(node.left, ast.Constant) and node.left.value == 10
    if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == "pow" and node.args:
        return isinstance(node.args[0], ast.Constant) and node.args[0].value == 10
    return False


def _mentions_money(node: ast.AST) -> bool:
    return any((isinstance(n, ast.Name) and MONEY_NAME.match(n.id)) or (isinstance(n, ast.Attribute) and MONEY_NAME.match(n.attr)) for n in ast.walk(node))


def money_multiplier(root: Path) -> list[Violation]:
    """No literal power of ten multiplies, divides or scales a money value in the model service (M1; the exponent comes from the currency table)."""
    out = []
    for path, rel in _files(root, "services/model"):
        for node in ast.walk(ast.parse(path.read_text())):
            if isinstance(node, ast.BinOp) and isinstance(node.op, (ast.Mult, ast.Div, ast.FloorDiv, ast.Mod)):
                if (_mentions_money(node.left) and _power_of_ten(node.right)) or (_mentions_money(node.right) and _power_of_ten(node.left)):
                    out.append(("py-money-multiplier", rel, node.lineno, "a literal power of ten on a money value; the exponent comes from the currency table (M1)"))
    return out


def _sql_code(text: str) -> str:
    """The SQL with comments and string literals blanked, keeping every line break so line numbers stay true."""
    blank = lambda m: re.sub(r"[^\n]", " ", m.group(0))
    text = re.sub(r"/\*.*?\*/", blank, text, flags=re.S)
    text = re.sub(r"--[^\n]*", blank, text)
    return re.sub(r"'(?:[^']|'')*'", blank, text)


def money_sql(root: Path) -> list[Violation]:
    """No REAL, DOUBLE PRECISION, FLOAT, NUMERIC, DECIMAL or MONEY on a money column or cast of a migration, and no MONEY type anywhere (M4)."""
    out = []
    for path, rel in _files(root, "services/core/migrations", ".sql"):
        code = _sql_code(path.read_text())
        for pattern, why in ((SQL_MONEY_COLUMN, "a money column"), (SQL_MONEY_CAST, "a cast of a money value"), (SQL_MONEY_CAST_FN, "a cast of a money value"), (SQL_MONEY_TYPE, "the MONEY type")):
            for m in pattern.finditer(code):
                kind = re.search(SQL_BAD_TYPE, m.group(0)[len(m.group(1)) if m.lastindex else 0:], re.I)
                out.append(("money-sql-type", rel, code.count("\n", 0, m.start()) + 1, f"{why} uses {' '.join(kind.group(0).upper().split()) if kind else 'a non-integer type'}; money is BIGINT in minor units (M4)"))
    return sorted(set(out))


ALL = (clock, money, money_multiplier, money_sql, cross_service, manifest, mocks, harness_only_providers, pipeline_purity)
