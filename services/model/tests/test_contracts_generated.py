"""CON-03, REQ-API-001, REQ-TST-003: the generated Pydantic models against the fixtures of /contracts.

The same fixtures are used by the JSON Schema validator (tools/contracts/validate.py) and by the generated Go types
(internal/contracts/roundtrip_test.go in the Go module), so the two languages are held to one set of documents. A model that
rewrites a valid document, or accepts a document that breaks a shape keyword, fails here.
"""
import json
import re
from pathlib import Path
from typing import Any

import pytest
from noema_model.contracts import registry
from noema_model.contracts._base import validator_for

ROOT = Path(__file__).resolve().parents[3]
FIXTURES = ROOT / "contracts" / "fixtures"
VERSIONED = re.compile(r"\.v[0-9]+$")


EXCLUDED = json.loads((ROOT / "tools" / "codegen" / "excluded.json").read_text())


def excluded(key: str) -> bool:
    """tools/codegen/excluded.json: contracts the services hold no types for."""
    return key in EXCLUDED["names"] or any(key.startswith(p) for p in EXCLUDED["prefixes"])


def key_of(folder: str, file: str) -> str:
    key = folder if VERSIONED.search(folder) else folder + ".v1"
    stem = file.removesuffix(".json")
    return f"{key}#{stem.split('__', 1)[0]}" if "__" in stem else key


def fixtures(kind: str) -> list[tuple[str, str, Path]]:
    out = []
    for d in sorted(p for p in (FIXTURES / kind).iterdir() if p.is_dir()):
        if excluded(key_of(d.name, "x.json")):
            continue
        for f in sorted(d.glob("*.json")):
            if f.name == "expectations.json" or f.name.endswith(".clarification.json"):
                continue
            out.append((d.name, f.name, f))
    return out


def same(a: Any, b: Any, path: str = "") -> str | None:
    """Equal as JSON, and of the same JSON type: True is not 1, and 1.0 is not 1."""
    if type(a) is not type(b):
        return f"{path or '/'}: {type(a).__name__} {a!r} != {type(b).__name__} {b!r}"
    if isinstance(a, dict):
        if set(a) != set(b):
            return f"{path or '/'}: members {sorted(set(a) ^ set(b))} differ"
        for k in a:
            if (d := same(a[k], b[k], f"{path}/{k}")) is not None:
                return d
    elif isinstance(a, list):
        if len(a) != len(b):
            return f"{path or '/'}: {len(a)} items != {len(b)}"
        for i, (x, y) in enumerate(zip(a, b, strict=True)):
            if (d := same(x, y, f"{path}/{i}")) is not None:
                return d
    elif a != b:
        return f"{path or '/'}: {a!r} != {b!r}"
    return None


def test_every_schema_and_definition_has_a_generated_model() -> None:
    schemas = sorted((ROOT / "contracts" / "schemas").glob("*.json"))
    assert len(schemas) >= 58
    for p in schemas:
        key = p.name.removesuffix(".json")
        if excluded(key):
            assert key not in registry.TYPES, f"{key} is excluded and has a generated model"
            continue
        assert key in registry.TYPES, f"no generated model for {key}"
        for d in json.loads(p.read_text()).get("$defs", {}):
            assert f"{key}#{d}" in registry.TYPES, f"no generated model for {key}#{d}"


def test_every_valid_fixture_round_trips_through_its_generated_model() -> None:
    bad = []
    valid = fixtures("valid")
    assert len(valid) >= 150
    for folder, file, path in valid:
        key = key_of(folder, file)
        text = path.read_text()
        v = validator_for(registry.TYPES[key])
        try:
            obj = v.validate_json(text)
        except Exception as e:  # a valid fixture refused is the finding; the message names it
            bad.append(f"{folder}/{file}: refused: {str(e)[:240]}")
            continue
        out = v.dump_python(obj, mode="json", by_alias=True, exclude_unset=True)
        if (d := same(json.loads(text), out)) is not None:
            bad.append(f"{folder}/{file}: the round trip changed the document at {d}")
    assert not bad, f"{len(bad)} of {len(valid)} valid fixtures failed:\n" + "\n".join(bad)


def test_the_generated_models_refuse_what_their_shape_forbids() -> None:
    wanted: dict[str, str] = json.loads((ROOT / "tools" / "codegen" / "shape_refusals.json").read_text())
    assert len(wanted) >= 150
    accepted = []
    found = set()
    for folder, file, path in fixtures("invalid"):
        ident = f"{folder}/{file}"
        if ident not in wanted:
            continue
        found.add(ident)
        try:
            validator_for(registry.TYPES[key_of(folder, file)]).validate_json(path.read_text())
        except ValueError:
            continue
        accepted.append(f"{ident} breaks {wanted[ident]!r} and the generated model accepts it")
    assert not accepted, "\n".join(accepted)
    assert found == set(wanted), f"shape_refusals.json names fixtures that do not exist: {sorted(set(wanted) - found)}"


@pytest.mark.parametrize("text", ['{"a": 1, "a": 2}'])
def test_a_repeated_member_is_not_silently_resolved(text: str) -> None:
    """Pydantic keeps the last of two equal keys; the contract decoders refuse them. The models are not the place that decides it,
    so the boundary must run the strict decoder first (tools/contracts/validate.py: load_strict). This test records the fact."""
    from pydantic import BaseModel

    class M(BaseModel):
        a: int

    assert M.model_validate_json(text).a == 2
