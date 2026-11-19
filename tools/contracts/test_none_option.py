"""A275 (REQ-AMB-004, PERD §39.2; AGENT-15): the core-minted `none_option_id` of a clarification.

The clarification object carries `none_option_id`, unique per clarification, which the UI submits as `option_id` to answer "None of these". The UI never invents a
sentinel. The member has the form of an option ID (`clarification.v1`: lower case, digits and underscore, 1 to 32; the overlay card adds the digit bound of its option_id), it is
required wherever `allow_none` is (it is always true), and it is never equal to an option_id of the same clarification, which JSON Schema cannot compare (tools/contracts/validate.py
`none_option_collisions`). All values are SYNTHETIC.
"""
import json
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "codegen"))
from anchors import fully_anchored  # noqa: E402
from validate import FIXTURES, load_schemas, load_strict, none_option_collisions  # noqa: E402

SCHEMAS = load_schemas()
CLARIFICATION = SCHEMAS["clarification.v1.json"]
CARD = SCHEMAS["overlay-card.v1.json"]["$defs"]["ClarificationCard"]


def holders(node):
    """Every object below node that is a clarification (it has allow_none)."""
    if isinstance(node, dict):
        if "allow_none" in node:
            yield node
        for value in node.values():
            yield from holders(value)
    elif isinstance(node, list):
        for value in node:
            yield from holders(value)


class NoneOptionSchema(unittest.TestCase):
    def test_a_clarification_and_a_clarification_card_require_the_id_of_none_of_these(self):
        for name, schema in (("clarification.v1", CLARIFICATION), ("overlay-card.v1 ClarificationCard", CARD)):
            self.assertIn("none_option_id", schema["properties"], name)
            self.assertIn("none_option_id", schema["required"], name)
            self.assertIn("allow_none", schema["required"], name)
            self.assertEqual(schema["properties"]["none_option_id"]["type"], "string", name)

    def test_the_id_has_the_form_of_an_option_id_in_each_contract(self):
        for name, schema in (("clarification.v1", CLARIFICATION), ("overlay-card.v1 ClarificationCard", CARD)):
            option = schema["properties"]["options"]["items"]["properties"]["option_id"]
            self.assertEqual(schema["properties"]["none_option_id"]["pattern"], option["pattern"], name)

    def test_the_id_on_a_card_cannot_carry_a_number_and_on_either_is_bounded(self):
        # the card reaches the customer as written (A211): at most four digits in all. Both contracts bound it to 32 characters of lower case, digits and underscore.
        for schema, accepted in ((CLARIFICATION, ("opt_none", "none", "n", "n" * 32, "none_1234", "a1b2c3d4")), (CARD, ("opt_none", "none", "n", "n" * 32, "none_1234", "a1b2c3d4"))):
            pattern = fully_anchored(schema["properties"]["none_option_id"]["pattern"])
            for text in accepted:
                self.assertIsNotNone(re.search(pattern, text), text)
            for text in ("", "n" * 33, "None", "none of these", "none-of-these", "none\n", "\u0664"):
                self.assertIsNone(re.search(pattern, text), repr(text))
        for schema in (CLARIFICATION, CARD):
            pattern = fully_anchored(schema["properties"]["none_option_id"]["pattern"])
            for text in ("none_12345", "4" * 32, "n1o2n3e4_5", "opt_12345", "x_" + "1" * 20):
                self.assertIsNone(re.search(pattern, text), text)

    def test_one_option_id_form_across_the_source_and_the_card_contracts(self):
        # noema#736 review, P2: a value core mints in the clarification (an option_id or the none_option_id) is submitted as option_id (PERD section 39.2) and must be accepted by every
        # contract that carries it. All four members hold the same pattern, so `opt_12345` cannot validate at the source and be refused at the card or in the request.
        forms = {
            "clarification option_id": CLARIFICATION["properties"]["options"]["items"]["properties"]["option_id"]["pattern"],
            "clarification none_option_id": CLARIFICATION["properties"]["none_option_id"]["pattern"],
            "card option_id": CARD["properties"]["options"]["items"]["properties"]["option_id"]["pattern"],
            "card none_option_id": CARD["properties"]["none_option_id"]["pattern"],
        }
        self.assertEqual(len(set(forms.values())), 1, forms)

    def test_no_other_contract_defines_a_second_none_member(self):
        # A member named none_option_id, not a mention in a description: message-request.v1 (main) names it in prose, because "None of these" is submitted there as an option_id.
        def members(node):
            if isinstance(node, dict):
                for key, value in node.items():
                    yield key
                    yield from members(value)
            elif isinstance(node, list):
                for value in node:
                    yield from members(value)

        for name, schema in SCHEMAS.items():
            if name not in ("clarification.v1.json", "overlay-card.v1.json"):
                self.assertNotIn("none_option_id", set(members(schema)), name)


class NoneOptionFixtures(unittest.TestCase):
    def test_every_valid_clarification_carries_an_id_that_is_none_of_its_options(self):
        seen = 0
        for path in sorted((FIXTURES / "valid").rglob("*.json")):
            for node in holders(load_strict(path)):
                seen += 1
                self.assertIn("none_option_id", node, str(path))
                self.assertNotIn(node["none_option_id"], {o["option_id"] for o in node["options"]}, str(path))
        self.assertGreaterEqual(seen, 10)

    def test_every_clarification_in_an_invalid_fixture_carries_the_id_unless_its_reason_is_the_missing_id(self):
        for path in sorted((FIXTURES / "invalid").rglob("*.json")):
            if path.name == "expectations.json" or "none_option_id" in path.name:
                continue
            for node in holders(load_strict(path)):
                self.assertIn("none_option_id", node, f"{path}: the fixture must fail for its own reason, not for a missing none_option_id")

    def test_the_collision_check_names_an_id_equal_to_an_option_id(self):
        card = {"options": [{"option_id": "opt_1", "label": "a"}, {"option_id": "opt_2", "label": "b"}], "none_option_id": "opt_2", "allow_none": True}
        self.assertEqual(none_option_collisions(card), ["opt_2"])
        self.assertEqual(none_option_collisions({"event": "clarification", "card": card}), ["opt_2"])
        self.assertEqual(none_option_collisions({"decision": {"clarification": card}}), ["opt_2"])
        self.assertEqual(none_option_collisions({**card, "none_option_id": "opt_none"}), [])
        self.assertEqual(none_option_collisions({**card, "options": []}), [])
        self.assertEqual(none_option_collisions({"options": [{"option_id": "opt_1"}]}), [])


if __name__ == "__main__":
    unittest.main()
