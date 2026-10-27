#!/usr/bin/env python3
"""Contracts validator (CON-01; SoT §43, DEC-2026-10-16 money-and-value-lists M4 and §2.4).

  validate.py                       check every schema, every fixture and the inventory
  validate.py --pack FILE           validate one pack file against pack.v1.json and list every error
  validate.py --instance NAME FILE  validate one document against contracts/schemas/NAME.v1.json

What the default run checks:
  1. every schema is valid JSON Schema 2020-12, its $id matches its file name, every $ref resolves,
     and every object type states additionalProperties;
  2. money fields are integers with the M3 bounds, never `number` (M4);
  3. every document in fixtures/valid/<schema>/ is accepted (a clarification answer must also name only slots its
     companion <name>.clarification.json requested; expectation 'unknown_slot') and every one in fixtures/invalid/<schema>/
     is rejected, for the reason named in that folder's expectations.json;
  4. every schema has a row in contracts/INVENTORY.md and every row names an existing schema;
  5. the enum drift guard (drift.py): enums.v1.json against the vendored state-registry.md, and
     money.v1.json against currencies.json.

Documents are read strictly. NaN, Infinity and duplicate object keys are decoder errors. A number written
with a decimal point or an exponent (2000000.0, 2e6) is kept as a float and is never an `integer`, because
JSON Schema alone would accept 2.0 as one (M4). Ratios such as 0.35 stay valid where the schema says `number`.
"""
import argparse
import json
import re
import sys
from pathlib import Path

from jsonschema import Draft202012Validator
from jsonschema.validators import extend
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

ROOT = Path(__file__).resolve().parent.parent.parent
CONTRACTS = ROOT / "contracts"
SCHEMAS = CONTRACTS / "schemas"
FIXTURES = CONTRACTS / "fixtures"
INVENTORY = CONTRACTS / "INVENTORY.md"
ID_BASE = "https://contracts.noema.invalid/"
MAX_MINOR = 9007199254740991
MONEY_NAME = re.compile(r"^(amount.*|.*_minor)$")


class StrictJsonError(ValueError):
    pass


DecoderErrors = (ValueError,)  # StrictJsonError and json.JSONDecodeError are both ValueErrors


class WireFloat(float):
    """A number written with a decimal point or an exponent. Never an `integer`, even when it is whole."""


def _is_integer(checker, instance):
    return not isinstance(instance, (bool, WireFloat)) and isinstance(instance, int)


StrictValidator = extend(Draft202012Validator, type_checker=Draft202012Validator.TYPE_CHECKER.redefine("integer", _is_integer))


def _no_constant(text: str):
    raise StrictJsonError(f"{text} is not valid JSON")


def _no_duplicates(pairs):
    seen = {}
    for key, value in pairs:
        if key in seen:
            raise StrictJsonError(f"duplicate object key {key!r}")
        seen[key] = value
    return seen


def load_strict(path: Path):
    return json.loads(path.read_text(encoding="utf-8"), parse_float=WireFloat, parse_constant=_no_constant, object_pairs_hook=_no_duplicates)


VERSIONED = re.compile(r"^(?P<name>[a-z0-9-]+)\.v(?P<major>[0-9]+)\.json$")


def load_schemas() -> dict[str, dict]:
    """Every schema, keyed by file name (`<name>.v<major>.json`). A new major is a new file beside the old one (AGENT-15)."""
    return {p.name: load_strict(p) for p in sorted(SCHEMAS.glob("*.json")) if VERSIONED.match(p.name)}


def schema_file(name: str) -> str:
    """`record-event` is record-event.v1.json (the name every v1 fixture folder uses); `record-event.v2` is record-event.v2.json."""
    return f"{name}.json" if re.search(r"\.v[0-9]+$", name) else f"{name}.v1.json"


def fixture_name(filename: str) -> str:
    """The fixture folder of a schema: `<name>` for v1, `<name>.v<major>` for a later major."""
    m = VERSIONED.match(filename)
    return m["name"] if m["major"] == "1" else f"{m['name']}.v{m['major']}"


def make_registry(schemas: dict[str, dict]) -> Registry:
    registry = Registry()
    for schema in schemas.values():
        registry = registry.with_resource(schema["$id"], Resource.from_contents(schema, default_specification=DRAFT202012))
    return registry


def validator_for(name: str, schemas: dict[str, dict], registry: Registry, definition: str | None = None) -> Draft202012Validator:
    """Validator for a schema, or for one of its $defs (a fixture named <Def>__<case>.json targets that definition)."""
    schema = schemas[schema_file(name)]
    if definition:
        return StrictValidator({"$ref": f"{schema['$id']}#/$defs/{definition}"}, registry=registry)
    return StrictValidator(schema, registry=registry)


def target(name: str, path: Path, schemas: dict[str, dict], registry: Registry) -> Draft202012Validator:
    definition = path.stem.split("__", 1)[0] if "__" in path.stem else None
    return validator_for(name, schemas, registry, definition)


def walk(node, path=""):
    yield path, node
    if isinstance(node, dict):
        for key, value in node.items():
            yield from walk(value, f"{path}/{key}")
    elif isinstance(node, list):
        for index, value in enumerate(node):
            yield from walk(value, f"{path}/{index}")


def check_schemas(schemas: dict[str, dict], registry: Registry) -> list[str]:
    problems = []
    ids = set()
    for filename, schema in schemas.items():
        try:
            Draft202012Validator.check_schema(schema)
        except Exception as exc:  # noqa: BLE001 - report whatever the metaschema says
            problems.append(f"{filename}: not a valid 2020-12 schema: {exc}")
            continue
        if schema.get("$schema") != "https://json-schema.org/draft/2020-12/schema":
            problems.append(f"{filename}: $schema must be 2020-12")
        if schema.get("$id") != ID_BASE + filename:
            problems.append(f"{filename}: $id {schema.get('$id')!r} does not match the file name")
        if schema.get("$id") in ids:
            problems.append(f"{filename}: duplicate $id")
        ids.add(schema.get("$id"))
        for where, node in walk(schema):
            if not isinstance(node, dict):
                continue
            if "$ref" in node and isinstance(node["$ref"], str):
                try:
                    registry.resolver(base_uri=schema["$id"]).lookup(node["$ref"])
                except Exception as exc:  # noqa: BLE001
                    problems.append(f"{filename}{where}: $ref {node['$ref']} does not resolve ({type(exc).__name__})")
            if node.get("type") == "object" and "properties" in node and "additionalProperties" not in node:
                problems.append(f"{filename}{where}: object type without additionalProperties")
            for prop, sub in (node.get("properties") or {}).items():
                if MONEY_NAME.match(prop) and isinstance(sub, dict):
                    if sub.get("type") != "integer" and "$ref" not in sub:
                        problems.append(f"{filename}{where}/properties/{prop}: money field must be an integer (M4), found {sub.get('type')!r}")
                    if sub.get("type") == "integer" and (sub.get("minimum") != 0 or sub.get("maximum") != MAX_MINOR):
                        problems.append(f"{filename}{where}/properties/{prop}: money field must be bounded 0..{MAX_MINOR} (M3)")
    return problems


def check_fixtures(schemas: dict[str, dict], registry: Registry) -> list[str]:
    problems = []
    names = {fixture_name(f) for f in schemas}
    for kind in ("valid", "invalid"):
        base = FIXTURES / kind
        for folder in sorted(p for p in base.iterdir() if p.is_dir()) if base.exists() else []:
            if folder.name not in names:
                problems.append(f"fixtures/{kind}/{folder.name}: no schema of that name")
    for name in sorted(names):
        valid_dir, invalid_dir = FIXTURES / "valid" / name, FIXTURES / "invalid" / name
        valid_files = sorted(f for f in valid_dir.glob("*.json") if not f.name.endswith(".clarification.json")) if valid_dir.exists() else []
        invalid_files = sorted(f for f in invalid_dir.glob("*.json") if f.name != "expectations.json" and not f.name.endswith(".clarification.json")) if invalid_dir.exists() else []
        if not valid_files:
            problems.append(f"{name}: no valid fixture")
        if not invalid_files:
            problems.append(f"{name}: no invalid fixture")
        for path in valid_files:
            if name == "clarification-answer":
                extra = unrequested_slots(path)
                if extra is None:
                    problems.append(f"valid/{name}/{path.name}: no {path.stem}.clarification.json beside it")
                elif extra:
                    problems.append(f"valid/{name}/{path.name}: names slots the clarification did not request: {sorted(extra)}")
            try:
                errors = list(target(name, path, schemas, registry).iter_errors(load_strict(path)))
            except ValueError as exc:
                problems.append(f"valid/{name}/{path.name}: decoder rejected a document that must be accepted: {exc}")
                continue
            for error in errors[:3]:
                problems.append(f"valid/{name}/{path.name}: rejected at {'/'.join(map(str, error.absolute_path)) or '(root)'}: {error.message[:120]}")
        expectations_path = invalid_dir / "expectations.json"
        expectations = load_strict(expectations_path) if expectations_path.exists() else {}
        for path in invalid_files:
            expected = expectations.get(path.name)
            if expected is None:
                problems.append(f"invalid/{name}/{path.name}: no entry in expectations.json")
                continue
            if expected == "unknown_slot":
                extra = unrequested_slots(path)
                shape = list(target(name, path, schemas, registry).iter_errors(load_strict(path)))
                if extra is None:
                    problems.append(f"invalid/{name}/{path.name}: expects unknown_slot but has no {path.stem}.clarification.json beside it")
                elif shape:
                    problems.append(f"invalid/{name}/{path.name}: must be well formed and rejected only for its slot names, but the schema rejects it: {shape[0].message[:100]}")
                elif not extra:
                    problems.append(f"invalid/{name}/{path.name}: accepted, but must be rejected (it names no unrequested slot)")
                continue
            try:
                errors = list(target(name, path, schemas, registry).iter_errors(load_strict(path)))
                hit = expected in {r for e in errors for r in keywords(e)}
                where = "schema"
            except ValueError as exc:
                hit, where = expected == "decoder", "decoder"
                errors = [exc]
            if not errors:
                problems.append(f"invalid/{name}/{path.name}: accepted, but must be rejected")
            elif not hit:
                problems.append(f"invalid/{name}/{path.name}: rejected by the {where}, but not for the expected reason {expected!r}")
        for stale in sorted(set(expectations) - {f.name for f in invalid_files}):
            problems.append(f"invalid/{name}/expectations.json: names {stale}, which does not exist")
    return problems


def unrequested_slots(answer_file: Path) -> set[str] | None:
    """Slot names an answer carries that its clarification did not request (DEC-2026-10-18), or None without a companion.

    A clarification answer is checked against the clarification it answers: <name>.json is the answer and
    <name>.clarification.json the clarification beside it. Answers may name only the slot the clarification requested.
    """
    companion = answer_file.with_name(answer_file.stem + ".clarification.json")
    if not companion.exists():
        return None
    requested = {load_strict(companion)["slot"]}
    return set(load_strict(answer_file)["answers"]) - requested


def keywords(error) -> set[str]:
    """The failing keyword of an error and of every nested cause (a oneOf failure carries its branches)."""
    found = {error.validator}
    for cause in error.context or []:
        found |= keywords(cause)
    return found


def check_inventory(schemas: dict[str, dict]) -> list[str]:
    if not INVENTORY.exists():
        return ["contracts/INVENTORY.md is missing"]
    rows = set(re.findall(r"^\|\s*`([a-z0-9-]+\.v[0-9]+\.json)`\s*\|", INVENTORY.read_text(), re.M))
    problems = [f"INVENTORY.md has no row for {f}" for f in sorted(set(schemas) - rows)]
    problems += [f"INVENTORY.md names {f}, which does not exist" for f in sorted(rows - set(schemas))]
    return problems


import drift  # noqa: E402  (sits next to this file)

CHECKS = [("enum drift (state-registry.md, currencies.json)", drift.check)]  # (label, callable(schemas, registry) -> list[str])


def run_all() -> int:
    schemas = load_schemas()
    registry = make_registry(schemas)
    steps = [("schemas", lambda: check_schemas(schemas, registry)), ("fixtures", lambda: check_fixtures(schemas, registry)),
             ("inventory", lambda: check_inventory(schemas))] + [(label, lambda f=fn: f(schemas, registry)) for label, fn in CHECKS]
    failed = 0
    for label, step in steps:
        problems = step()
        print(f"[{'FAIL' if problems else 'PASS'}] {label}")
        for problem in problems:
            print("    " + problem)
        failed += bool(problems)
    print(f"{len(schemas)} schemas")
    return 1 if failed else 0


def run_instance(name: str, file: Path) -> int:
    schemas = load_schemas()
    validator = validator_for(name, schemas, make_registry(schemas))
    try:
        document = load_strict(file)
    except (StrictJsonError, ValueError) as exc:
        print(f"decoder FAIL: {exc}")
        return 1
    errors = sorted(validator.iter_errors(document), key=lambda e: list(map(str, e.absolute_path)))
    for error in errors:
        print(f"{'/'.join(map(str, error.absolute_path)) or '(root)'}: [{error.validator}] {error.message[:200]}")
    print(f"{file}: {len(errors)} error(s)")
    return 1 if errors else 0


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--pack", type=Path)
    parser.add_argument("--instance", nargs=2, metavar=("NAME", "FILE"))
    args = parser.parse_args()
    if args.pack:
        return run_instance("pack", args.pack)
    if args.instance:
        return run_instance(args.instance[0], Path(args.instance[1]))
    return run_all()


if __name__ == "__main__":
    sys.exit(main())
