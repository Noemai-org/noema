"""Checks for the optional node member `params` of action-graph.v2 (A369, interim, relayed, not yet committed; REQ-AG-001, INV-02, INV-18).

Each param names its source: SLOT (a resolved slot of the intent node), CONST (a literal) or DEFAULT (a literal used only when the slot is absent). It is the ParamSpec of the
PlanGraphs adapter (noema#762). Additive and optional: a graph without `params` still validates. A null `params` is held here and not as an invalid fixture, because the
generated Pydantic models accept a null optional member. All values are SYNTHETIC.
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

    def test_each_source_takes_exactly_its_members(self):
        good = [{"name": "a", "source": "SLOT", "slot": "s"}, {"name": "b", "source": "CONST", "value": 1},
                {"name": "c", "source": "DEFAULT", "slot": "s", "value": "x"}, {"name": "d", "source": "CONST", "value": True}]
        self.assertEqual(errors(with_params(good)), [])
        for bad in ({"name": "a", "source": "SLOT"}, {"name": "a", "source": "SLOT", "slot": "s", "value": 1},
                    {"name": "a", "source": "CONST"}, {"name": "a", "source": "CONST", "value": 1, "slot": "s"},
                    {"name": "a", "source": "DEFAULT", "slot": "s"}, {"name": "a", "source": "DEFAULT", "value": 1},
                    {"name": "a", "source": "slot", "slot": "s"}, {"name": "a", "source": None},
                    {"name": "a", "source": "CONST", "value": None}, {"name": "a", "source": "CONST", "value": [1]},
                    {"name": "a", "source": "CONST", "value": 1.5}):
            self.assertTrue(errors(with_params([bad])), f"{bad} is accepted")


if __name__ == "__main__":
    unittest.main()
