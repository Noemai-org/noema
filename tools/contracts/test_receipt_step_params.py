"""A692 (interim, relayed; OD-P-119; AGENT-15): the signed execution receipt gains an optional `step_params`, the params of the executed plan step that the
outcome definition's PLAN_PARAM bindings name. Additive, so the file stays execution-receipt.v1.json (A698, the rule of the additive optional members).

The schema holds the shape; which params a definition names is not in the schema (it needs the pack's outcome definition) and is the Controller's to keep to.
"""
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import FIXTURES, load_schemas  # noqa: E402

SCHEMAS = load_schemas()
RECEIPT = SCHEMAS["execution-receipt.v1.json"]
PLAN_PARAMS = SCHEMAS["plan.v1.json"]["properties"]["steps"]["items"]["properties"]["params"]


class StepParams(unittest.TestCase):
    def test_the_member_exists_and_is_optional(self):
        self.assertIn("step_params", RECEIPT["properties"], "execution-receipt.v1 has no step_params member")
        self.assertNotIn("step_params", RECEIPT["required"], "step_params must stay optional: a receipt of a capability with no PLAN_PARAM binding carries none")

    def test_it_is_an_object_that_is_never_empty(self):
        m = RECEIPT["properties"]["step_params"]
        self.assertEqual(m["type"], "object")
        self.assertEqual(m.get("minProperties"), 1, "absent means no param was bound; an empty object would say the same twice")

    def test_its_keys_and_values_are_those_of_a_plan_step_param(self):
        m = RECEIPT["properties"]["step_params"]
        self.assertEqual(m["propertyNames"], PLAN_PARAMS["propertyNames"], "a receipt param is named like a plan param")
        value = dict(m["additionalProperties"])
        self.assertEqual(value.pop("maxLength", None), 128, "A709: a disclosed string is at most 128 characters (a hash is 71)")
        self.assertEqual(value, PLAN_PARAMS["additionalProperties"], "a receipt param holds what a plan param may hold, bounded in length")

    def test_the_member_is_bounded_in_size(self):
        self.assertEqual(RECEIPT["properties"]["step_params"].get("maxProperties"), 8, "A709: at most eight bound params")

    def test_the_receipt_is_still_closed(self):
        self.assertIs(RECEIPT["additionalProperties"], False)

    def test_the_fixtures_name_what_they_prove(self):
        valid = FIXTURES / "valid" / "execution-receipt"
        for name in ("with_step_params.json", "step_params_every_scalar_kind.json"):
            doc = json.loads((valid / name).read_text())
            self.assertIn("step_params", doc, name)
        self.assertNotIn("step_params", json.loads((valid / "acknowledged.json").read_text()), "the old receipt shape stays valid and carries none")


if __name__ == "__main__":
    unittest.main()
