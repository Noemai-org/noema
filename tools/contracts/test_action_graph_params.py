"""Checks for the optional node member `params` of action-graph.v2 (A369, interim, relayed, not yet committed; REQ-AG-001, INV-02, INV-18).

A param is `{name, <kind>}` with exactly one kind: `slot` (a resolved slot of the intent node), `const` (a literal), `map` (`{from: <slot>, values: {<key>: <code>}}`), `fact` (a named
fact), `policy_param` (a param of the winning PERMIT rule) or `evidence_packet: true`. Additive and optional: a graph without `params` still validates. A null `params` is held here and
not as an invalid fixture, because the generated Pydantic models accept a null optional member. All values are SYNTHETIC.
"""
import copy
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
GRAPH = json.loads((ROOT / "contracts" / "fixtures" / "valid" / "action-graph.v2" / "with_params.json").read_text())


def errors(doc):
    return list(validator_for("action-graph.v2", SCHEMAS, REGISTRY).iter_errors(doc))


def with_params(params):
    doc = copy.deepcopy(GRAPH)
    doc["nodes"][1]["params"] = params
    return doc


class Params(unittest.TestCase):
    def test_a_graph_with_params_and_one_without_both_validate(self):
        self.assertEqual(errors(GRAPH), [])
        bare = copy.deepcopy(GRAPH)
        del bare["nodes"][1]["params"]
        self.assertEqual(errors(bare), [])
        self.assertEqual(errors(with_params([])), [])

    def test_params_is_the_only_new_member_of_a_node(self):
        v1, v2 = SCHEMAS["action-graph.v1.json"]["properties"]["nodes"], SCHEMAS["action-graph.v2.json"]["properties"]["nodes"]
        self.assertEqual(set(v2["items"]["properties"]) - set(v1["items"]["properties"]), {"params"})
        self.assertEqual(set(v2["items"]["required"]), set(v1["items"]["required"]))

    def test_null_params_is_refused(self):
        self.assertTrue(errors(with_params(None)))

    def test_each_kind_takes_its_shape_and_exactly_one_kind_is_given(self):
        good = [{"name": "a", "slot": "s"}, {"name": "b", "const": 1}, {"name": "c", "const": "x"}, {"name": "d", "const": True},
                {"name": "e", "map": {"from": "q", "values": {"K": "V_1"}}}, {"name": "f", "fact": "case_id"}, {"name": "g", "policy_param": "owner_queue"},
                {"name": "h", "evidence_packet": True}]
        self.assertEqual(errors(with_params(good)), [])
        for bad in ({"name": "a"}, {"name": "a", "slot": "s", "const": 1}, {"name": "a", "fact": "f", "evidence_packet": True}, {"slot": "s"},
                    {"name": "a", "source": "SLOT", "slot": "s"}, {"name": "a", "const": None}, {"name": "a", "const": [1]}, {"name": "a", "const": 1.5},
                    {"name": "a", "map": {"from": "q"}}, {"name": "a", "map": {"values": {"K": "V"}}}, {"name": "a", "map": {"from": "q", "values": {}}},
                    {"name": "a", "map": {"from": "q", "values": {"k": "V"}}}, {"name": "a", "map": {"from": "q", "values": {"K": 1}}},
                    {"name": "a", "evidence_packet": False}, {"name": "a", "fact": ""}, {"name": "a", "policy_param": 1}, {"name": "a", "slot": None}):
            self.assertTrue(errors(with_params([bad])), f"{bad} is accepted")


if __name__ == "__main__":
    unittest.main()
