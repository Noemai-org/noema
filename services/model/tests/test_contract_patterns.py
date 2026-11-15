"""CON-03, REQ-API-001, A138: every pattern of /contracts is fully anchored in the generated Pydantic models.

A `^...$` pattern run by Python's `re` accepts a string with a trailing newline (`$` also matches before a final "\\n"), while the
contract means "the whole string" (JSON Schema's ECMA 262 `$`, and Go's `$`, match only at the end). The generator therefore emits
`\\A...\\Z`. These tests hold every generated pattern to that, on the whole contract set, with witnesses taken from the valid fixtures.
All data is SYNTHETIC.
"""
import ast
import json
import re
from pathlib import Path
from typing import Any

from noema_model.contracts import registry  # noqa: F401  (imports the generated modules)

ROOT = Path(__file__).resolve().parents[3]
CONTRACTS = ROOT / "services" / "model" / "noema_model" / "contracts"
FIXTURES = ROOT / "contracts" / "fixtures" / "valid"
ESCAPED_OR_CLASS = re.compile(r"\\.|\[\^?\]?(?:\\.|[^\]\\])*\]")


def generated_patterns() -> set[str]:
    """Every `pattern=` argument of every generated module, read from the source so no model has to be built."""
    out: set[str] = set()
    for path in sorted(CONTRACTS.glob("*.py")):
        for node in ast.walk(ast.parse(path.read_text())):
            if isinstance(node, ast.keyword) and node.arg == "pattern" and isinstance(node.value, ast.Constant) and isinstance(node.value.value, str):
                out.add(node.value.value)
    return out


def strings_in(value: Any) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, dict):
        return [s for v in value.values() for s in strings_in(v)]
    if isinstance(value, list):
        return [s for v in value for s in strings_in(v)]
    return []


def fixture_strings() -> set[str]:
    out: set[str] = set()
    for path in sorted(FIXTURES.rglob("*.json")):
        out.update(strings_in(json.loads(path.read_text())))
    return out


def test_there_are_patterns_to_check() -> None:
    assert len(generated_patterns()) >= 60


def test_no_generated_pattern_keeps_a_bare_caret_or_dollar() -> None:
    """A bare `$` is the bug; a bare `^` is `\\A` in a pattern without the multi-line flag, but is replaced too so one rule covers both."""
    bad = [p for p in sorted(generated_patterns()) if re.search(r"[\^$]", ESCAPED_OR_CLASS.sub("", p))]
    assert not bad, bad


def schema_patterns() -> set[str]:
    out: set[str] = set()

    def walk(o: Any) -> None:
        if isinstance(o, dict):
            for k, v in o.items():
                if k == "pattern" and isinstance(v, str):
                    out.add(v)
                walk(v)
        elif isinstance(o, list):
            for v in o:
                walk(v)

    for path in sorted((ROOT / "contracts" / "schemas").glob("*.json")):
        walk(json.loads(path.read_text()))
    return out


def test_every_schema_pattern_is_whole_string_anchored_in_the_models() -> None:
    """Each pattern of /contracts/schemas is `^core$` and the model carries exactly `\\Acore\\Z`; the generator may exclude a contract, never relax a pattern."""
    schema = schema_patterns()
    assert all(p.startswith("^") and p.endswith("$") for p in schema), [p for p in schema if not (p.startswith("^") and p.endswith("$"))]
    expected = {"\\A" + p[1:-1] + "\\Z" for p in schema}
    generated = generated_patterns()
    assert generated <= expected, sorted(generated - expected)
    assert len(generated) >= 60


def test_a_witness_is_accepted_and_the_same_string_with_a_newline_follows_the_whole_string_rule() -> None:
    """A witness is a string of a valid fixture that the pattern accepts. With "\\n" added, the model's pattern must give the answer of
    `re.fullmatch(core, ...)` (the whole-string meaning of the schema), never the `$` answer."""
    strings = sorted(fixture_strings())
    witnessed = 0
    failures: list[str] = []
    for schema_pattern in sorted(schema_patterns()):
        core = schema_pattern[1:-1]
        generated = "\\A" + core + "\\Z"
        if generated not in generated_patterns():
            continue
        compiled = re.compile(generated)
        for s in strings:
            if compiled.search(s):
                witnessed += 1
                want = re.fullmatch(core, s + "\n") is not None
                if (compiled.search(s + "\n") is not None) != want:
                    failures.append(f"{generated!r} on {s!r} + newline: want {want}")
                break
    assert witnessed >= 40, witnessed
    assert not failures, failures
