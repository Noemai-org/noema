"""Strict loading and validation of documents against /contracts (standard library plus jsonschema).

Mirrors the rules of tools/contracts/validate.py that matter to benchmark files: NaN, Infinity and duplicate
object keys are decoder errors, and a number written with a decimal point or exponent is never an `integer`
(DEC-2026-10-16 money-and-value-lists M4). Canonical bytes (sorted keys, compact separators, UTF-8) are what
every benchmark hash is taken over, so the same document always has the same digest.
"""
import hashlib
import json
import re
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator  # type: ignore[import-untyped]  # no stubs; adding types-jsonschema needs approval (AGENT-14)
from jsonschema.validators import extend  # type: ignore[import-untyped]
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

Json = dict[str, Any]
_VERSIONED = re.compile(r"^[a-z0-9-]+\.v[0-9]+\.json$")

ROOT = Path(__file__).resolve().parent.parent
SCHEMAS = ROOT / "contracts" / "schemas"


class StrictJsonError(ValueError):
    pass


class WireFloat(float):
    """A number written with a decimal point or an exponent. Never an `integer`, even when whole."""


def _is_integer(checker: Any, instance: Any) -> bool:
    return not isinstance(instance, (bool, WireFloat)) and isinstance(instance, int)


_Validator = extend(Draft202012Validator, type_checker=Draft202012Validator.TYPE_CHECKER.redefine("integer", _is_integer))


def _no_constant(text: str) -> Any:
    raise StrictJsonError(f"{text} is not valid JSON")


def _no_duplicates(pairs: list[tuple[str, Any]]) -> Json:
    seen: Json = {}
    for key, value in pairs:
        if key in seen:
            raise StrictJsonError(f"duplicate object key {key!r}")
        seen[key] = value
    return seen


def loads_strict(text: str) -> Any:
    return json.loads(text, parse_float=WireFloat, parse_constant=_no_constant, object_pairs_hook=_no_duplicates)


def load_strict(path: Path) -> Any:
    return loads_strict(path.read_text(encoding="utf-8"))


def canonical_bytes(document: Any) -> bytes:
    """Sorted keys, compact separators, UTF-8. A WireFloat is written with repr, an int as an int."""
    return json.dumps(document, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")


def sha256_of(document: Any) -> str:
    return "sha256:" + hashlib.sha256(canonical_bytes(document)).hexdigest()


def pretty_bytes(document: Any) -> bytes:
    """The bytes a benchmark file is written with: sorted keys, two-space indent, one trailing newline."""
    return (json.dumps(document, sort_keys=True, indent=2, ensure_ascii=False) + "\n").encode("utf-8")


class Contracts:
    """All /contracts schemas, loaded once, with a validator per schema name."""

    def __init__(self, schemas_dir: Path = SCHEMAS) -> None:
        self._schemas: dict[str, Json] = {p.name: load_strict(p) for p in sorted(schemas_dir.glob("*.json")) if _VERSIONED.match(p.name)}
        registry: Registry = Registry()
        for schema in self._schemas.values():
            registry = registry.with_resource(schema["$id"], Resource.from_contents(schema, default_specification=DRAFT202012))
        self._registry = registry
        self._validators: dict[str, Any] = {}

    def errors(self, name: str, document: Any) -> list[str]:
        """Every violation of contracts/schemas/<name>.v1.json (or <name>.json when name ends in .v<major>, e.g. "reconstruction.v2"), as 'path: message', sorted. Empty when valid."""
        if name not in self._validators:
            file = f"{name}.json" if re.search(r"\.v[0-9]+$", name) else f"{name}.v1.json"
            self._validators[name] = _Validator(self._schemas[file], registry=self._registry)
        found = [
            f"{'/'.join(map(str, e.absolute_path)) or '(root)'}: {e.message[:160]}"
            for e in self._validators[name].iter_errors(document)
        ]
        return sorted(found)
