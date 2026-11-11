"""BEN-16 (DoD A131 3b): every array and every free string of the benchmark contracts of this PR is bounded, because the arm-written and generated documents are input that must fail closed on a huge value.
Written before the bounds and seen failing. SYNTHETIC."""
import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = ("benchmark-arm-result.v1.json",)


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


class Bounds(unittest.TestCase):
    def test_every_array_and_free_string_is_bounded(self):
        for name in SCHEMAS:
            schema = json.loads((ROOT / "contracts" / "schemas" / name).read_text())
            self.assertEqual([], list(unbounded(schema)), name)


if __name__ == "__main__":
    unittest.main()
