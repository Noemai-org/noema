"""BEN-16 (DoD A131 3b): every array and every free string of the benchmark contracts of this PR is bounded, because the arm-written and generated documents are input that must fail closed on a huge
value. The members `benchmark-oracle.v2` inherits unchanged from v1 are excluded: v2 changes none of them, and the v1 file is pinned. Written before the bounds and seen failing. SYNTHETIC."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = ("benchmark-case.v2.json", "benchmark-oracle.v2.json", "benchmark-arm-case.v1.json", "benchmark-fixture.v2.json")


def unbounded(node, path=""):
    if isinstance(node, dict):
        if node.get("type") == "array" and "maxItems" not in node:
            yield f"{path}: array without maxItems"
        kinds = node.get("type")
        kinds = kinds if isinstance(kinds, list) else [kinds]
        if "string" in kinds and not any(k in node for k in ("maxLength", "pattern", "enum", "const", "format", "$ref")):
            yield f"{path}: string without maxLength, pattern or enum"
        for key, value in node.items():
            yield from unbounded(value, f"{path}/{key}")
    elif isinstance(node, list):
        for index, value in enumerate(node):
            yield from unbounded(value, f"{path}/{index}")


def load(name):
    return json.loads((ROOT / "contracts" / "schemas" / name).read_text())


class Bounds(unittest.TestCase):
    def test_every_array_and_free_string_is_bounded(self):
        for name in SCHEMAS:
            schema = load(name)
            if name == "benchmark-oracle.v2.json":
                inherited = set(load("benchmark-oracle.v1.json")["properties"])
                schema = {**schema, "properties": {k: v for k, v in schema["properties"].items() if k not in inherited}}
            self.assertEqual([], list(unbounded(schema)), name)


if __name__ == "__main__":
    unittest.main()
