"""The shape rules for the JSON schema a strict structured-output request carries (INV-02: a reply is data and crosses only the schema gate).

Implements: the strict-schema check of the harness (REQ-INT-002). One function reads a schema and lists what a strict provider mode would refuse
or what would make a missing field a default instead of a rejection. It is a pure reading of the schema: no network, no provider.

The rules, each from OpenAI's documented strict structured-output restrictions or from the harness's own "no default" stance:
  1. The root is an object.
  2. Every object lists all its properties as required and sets additionalProperties to false (an object without properties too).
  3. A `$ref` stands alone: no other keyword beside it (a description, a title, a default). OpenAI strict mode is known to refuse that shape; the
     provider's 400 body was not recorded for the run that showed it (noema compile run 44521380842), so this rule rests on that report.

Not checked here, because the exact limits were not verified offline: unsupported per-type keywords (pattern, format, minimum and the like), the
limits on nesting depth and property counts, anyOf and allOf placement, and `$defs` siblings of a root `$ref`.
"""

from collections.abc import Mapping
from typing import Any


_NAMED_SCHEMAS = frozenset({"properties", "$defs", "definitions"})


def strict_schema_problems(schema: Any) -> list[str]:
    """Every rule the schema breaks, each as a short path-and-reason string. An empty list means the schema passes."""
    problems: list[str] = []
    if not isinstance(schema, Mapping) or not _is_object(schema):
        problems.append("$: the root is not an object")
    _walk(schema, "$", problems)
    return problems


def _is_object(node: Mapping[str, Any]) -> bool:
    kind = node.get("type")
    return kind == "object" or (isinstance(kind, list) and "object" in kind) or "properties" in node


def _walk(node: Any, path: str, problems: list[str]) -> None:
    if isinstance(node, Mapping):
        if "$ref" in node and len(node) > 1:
            problems.append(f"{path}: $ref has sibling keywords {sorted(k for k in node if k != '$ref')}")
        if _is_object(node):
            props = node.get("properties", {})
            if not isinstance(props, Mapping) or set(node.get("required", ())) != set(props):
                problems.append(f"{path}: an object must list all its properties as required")
            if node.get("additionalProperties") is not False:
                problems.append(f"{path}: an object must set additionalProperties to false")
        for key, child in node.items():
            if key in _NAMED_SCHEMAS and isinstance(child, Mapping):
                for name, member in child.items():  # a property may be called "type" or "$ref": its name is not a keyword
                    _walk(member, f"{path}.{key}.{name}", problems)
            else:
                _walk(child, f"{path}.{key}", problems)
    elif isinstance(node, list):
        for index, child in enumerate(node):
            _walk(child, f"{path}[{index}]", problems)
