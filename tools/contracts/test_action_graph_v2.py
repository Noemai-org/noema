"""action-graph.v2.json (PCK-04): v1 plus the required `roots` list of PERD v1.19 REQ-AG-001 (DEC-2026-10-31 D9). AGENT-15."""
import copy
import hashlib
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
V1_SHA256 = "b8546413e60c9a538bc27c1cf86395901a0e7eb8541a7bfdd48b9496a6c2f7ae"
GRAPHS = sorted((ROOT / "packs" / "fintech" / "graph").glob("*.json"))


def valid(version: str, doc: dict) -> bool:
    return not list(validator_for(f"action-graph.v{version}", SCHEMAS, REGISTRY).iter_errors(doc))


class ActionGraphV2(unittest.TestCase):
    def test_v1_is_not_edited(self):
        self.assertEqual(hashlib.sha256((ROOT / "contracts" / "schemas" / "action-graph.v1.json").read_bytes()).hexdigest(), V1_SHA256)

    def test_v2_is_v1_plus_exactly_roots(self):
        v1, v2 = SCHEMAS["action-graph.v1.json"], SCHEMAS["action-graph.v2.json"]
        self.assertEqual(set(v2["properties"]) - set(v1["properties"]), {"roots"})
        self.assertEqual(set(v2["required"]) - set(v1["required"]), {"roots"})
        for name in v1["properties"]:
            self.assertEqual(v1["properties"][name], v2["properties"][name], name)

    def test_a_graph_is_v1_or_v2_never_both(self):
        self.assertEqual(len(GRAPHS), 3)
        for path in GRAPHS:
            doc = json.loads(path.read_text())
            with self.subTest(path.name):
                self.assertTrue(valid("2", doc))
                self.assertFalse(valid("1", doc))
                old = copy.deepcopy(doc)
                del old["roots"]
                self.assertTrue(valid("1", old))
                self.assertFalse(valid("2", old))


if __name__ == "__main__":
    unittest.main()
