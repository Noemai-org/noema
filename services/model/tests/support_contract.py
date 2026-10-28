"""A small checker for the JSON Schema keywords the Intent Object contract uses, so the model service's test run can hold an
Intent Object to `contracts/schemas/intent-object.v1.json` without a schema library (the service has none, and adding one
needs approval, AGENT-14). It returns the list of violations. It is not a general validator: a keyword it does not know raises,
so a contract change that outgrows it fails loudly instead of passing quietly.

The full validator, `python3 tools/contracts/validate.py --instance intent-object FILE`, is the authority; the PR body shows it
run on the same files.
"""

import json
import re
from pathlib import Path
from typing import Any

SCHEMAS = Path(__file__).resolve().parents[3] / "contracts" / "schemas"
KNOWN = {"$schema", "$id", "$ref", "$defs", "title", "description", "type", "properties", "required", "additionalProperties", "enum",
         "const", "pattern", "minimum", "maximum", "minLength", "propertyNames", "oneOf", "items", "prefixItems", "minItems",
         "uniqueItems", "if", "then", "else", "allOf", "not"}


def _load(name: str) -> Any:
    return json.loads((SCHEMAS / name).read_text(encoding="utf-8"))


def _resolve(ref: str, here: str) -> tuple[Any, str]:
    file, _, pointer = ref.partition("#")
    name = file.rsplit("/", 1)[-1] if file else here
    node = _load(name)
    for part in [p for p in pointer.split("/") if p]:
        node = node[part]
    return node, name


def _is(kind: str, value: Any) -> bool:
    if kind == "string":
        return isinstance(value, str)
    if kind == "integer":
        return isinstance(value, int) and not isinstance(value, bool)
    if kind == "number":
        return isinstance(value, (int, float)) and not isinstance(value, bool)
    if kind == "boolean":
        return isinstance(value, bool)
    if kind == "object":
        return isinstance(value, dict)
    if kind == "array":
        return isinstance(value, list)
    if kind == "null":
        return value is None
    raise ValueError(f"unknown type {kind!r}")


def violations(value: Any, schema: Any, here: str = "intent-object.v1.json", path: str = "$") -> list[str]:
    if schema is True:
        return []
    if schema is False:
        return [f"{path}: not allowed"]
    unknown = set(schema) - KNOWN
    if unknown:
        raise ValueError(f"the checker does not know {sorted(unknown)} at {path}; use tools/contracts/validate.py")
    if "$ref" in schema:
        target, name = _resolve(schema["$ref"], here)
        return violations(value, target, name, path)
    out: list[str] = []
    if "type" in schema and not _is(schema["type"], value):
        return [f"{path}: expected {schema['type']}"]
    if "enum" in schema and value not in schema["enum"]:
        out.append(f"{path}: {value!r} not in {schema['enum']}")
    if "const" in schema and value != schema["const"]:
        out.append(f"{path}: expected {schema['const']!r}")
    if isinstance(value, str):
        if "pattern" in schema and not re.search(schema["pattern"], value):
            out.append(f"{path}: {value!r} does not match {schema['pattern']}")
        if "minLength" in schema and len(value) < schema["minLength"]:
            out.append(f"{path}: shorter than {schema['minLength']}")
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        if "minimum" in schema and value < schema["minimum"]:
            out.append(f"{path}: below {schema['minimum']}")
        if "maximum" in schema and value > schema["maximum"]:
            out.append(f"{path}: above {schema['maximum']}")
    if isinstance(value, dict):
        for key in schema.get("required", []):
            if key not in value:
                out.append(f"{path}: missing {key}")
        props = schema.get("properties", {})
        extra = schema.get("additionalProperties", True)
        for key, item in value.items():
            if key in props:
                out += violations(item, props[key], here, f"{path}.{key}")
            elif extra is False:
                out.append(f"{path}: unknown field {key}")
            elif isinstance(extra, dict):
                out += violations(item, extra, here, f"{path}.{key}")
            if "propertyNames" in schema:
                out += violations(key, schema["propertyNames"], here, f"{path}.<name {key}>")
    if isinstance(value, list):
        prefix = schema.get("prefixItems", [])
        if "minItems" in schema and len(value) < schema["minItems"]:
            out.append(f"{path}: fewer than {schema['minItems']} items")
        if schema.get("items") is False and len(value) > len(prefix):
            out.append(f"{path}: more than {len(prefix)} items")
        for index, item in enumerate(value):
            if index < len(prefix):
                out += violations(item, prefix[index], here, f"{path}[{index}]")
            elif isinstance(schema.get("items"), dict):
                out += violations(item, schema["items"], here, f"{path}[{index}]")
        if schema.get("uniqueItems") and len({json.dumps(v, sort_keys=True) for v in value}) != len(value):
            out.append(f"{path}: items are not unique")
    if "oneOf" in schema:
        passing = [i for i, branch in enumerate(schema["oneOf"]) if not violations(value, branch, here, path)]
        if len(passing) != 1:
            out.append(f"{path}: matches {len(passing)} of the oneOf branches, not exactly one")
    if "not" in schema and not violations(value, schema["not"], here, path):
        out.append(f"{path}: matches a forbidden shape")
    for branch in schema.get("allOf", []):
        out += violations(value, branch, here, path)
    if "if" in schema:
        matched = not violations(value, schema["if"], here, path)
        chosen = schema.get("then") if matched else schema.get("else")
        if chosen is not None:
            out += violations(value, chosen, here, path)
    return out
