"""Checks for the three optional members of intent-node.v1 `disambiguation` that carry the candidate labels (A555, interim, relayed, not yet committed; REQ-AMB-004, REQ-AMB-003, INV-14).

`options_key` is the pack's question key for OPTIONS mode, `display_template` the label of one candidate in OPTIONS mode, and `attribute_order` the pack's fixed order of askable
attributes (ask by attribute, DEC-2026-10-20 D4), each `{name, question_key, label_template}`. The Go intake already has these as `intake.Disambiguation` (`OptionsKey`,
`AttributeOrder`) and `decide.AttributeSpec`; the pack had no member to fill them. What a template may name (candidate attributes, no digits, no adjacent placeholders) is
packcheck's, not the schema's. The generated Pydantic models accept an explicit null for an optional member, so null cases are held here, against the schema. All values are SYNTHETIC.
"""
import copy
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
FIX = ROOT / "contracts" / "fixtures" / "valid"
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
NODE = json.loads((FIX / "intent-node" / "investigate.json").read_text())
GOOD = {
    "options_key": "fin.clarify.options",
    "display_template": "{attr:counterparty_display}, {attr:direction}, {attr:occurred_at}",
    "attribute_order": [
        {"name": "occurred_at", "question_key": "fin.clarify.by_time", "label_template": "{attr:occurred_at}"},
        {"name": "account_last4", "question_key": "fin.clarify.by_account", "label_template": "the account ending {attr:account_last4}"},
    ],
}


def errors(doc):
    return list(validator_for("intent-node", SCHEMAS, REGISTRY).iter_errors(doc))


def with_members(**members):
    doc = copy.deepcopy(NODE)
    doc["disambiguation"].update(copy.deepcopy(members))
    return doc


class CandidateLabels(unittest.TestCase):
    def test_the_members_are_optional_and_a_good_set_validates(self):
        for name in GOOD:
            self.assertNotIn(name, NODE["disambiguation"])
        self.assertEqual(errors(NODE), [])
        self.assertEqual(errors(with_members(**GOOD)), [])
        for name in GOOD:  # each member alone is a valid shape; that the three go together is packcheck's rule
            self.assertEqual(errors(with_members(**{name: GOOD[name]})), [], name)

    def test_the_members_are_in_the_schema_and_an_unknown_member_is_still_refused(self):
        props = SCHEMAS["intent-node.v1.json"]["properties"]["disambiguation"]["properties"]
        self.assertTrue({"options_key", "display_template", "attribute_order"} <= set(props), sorted(props))
        self.assertTrue(errors(with_members(candidate_labels=[])))
        bad = copy.deepcopy(GOOD)
        bad["attribute_order"][0]["weight"] = 1
        self.assertTrue(errors(with_members(**bad)), "an unknown member of an attribute is accepted")

    def test_null_empty_and_malformed_values_are_refused(self):
        cases = {
            "options_key": (None, "", "Fin.clarify", "fin", "fin.Clarify", 1, [], {}, "fin.clarify options"),
            "display_template": (None, "", 1, [], {}, "x" * 161),
            "attribute_order": (None, [], {}, "a", [None], ["a"], [{}], [{"name": "occurred_at"}], [{"name": "Occurred", "question_key": "fin.q", "label_template": "x"}],
                                [{"name": "occurred_at", "question_key": "Q", "label_template": "x"}], [{"name": "occurred_at", "question_key": "fin.q", "label_template": ""}],
                                [{"name": "occurred_at", "question_key": "fin.q", "label_template": 1}], [{"name": "occurred_at", "question_key": "fin.q", "label_template": "x" * 161}]),
        }
        for member, bad_values in cases.items():
            for bad in bad_values:
                self.assertTrue(errors(with_members(**{member: bad})), f"{member} {bad!r} is accepted")

    def test_at_most_eight_attributes_are_listed(self):
        item = GOOD["attribute_order"][0]
        self.assertEqual(errors(with_members(attribute_order=[item] * 8)), [])
        self.assertTrue(errors(with_members(attribute_order=[item] * 9)))


if __name__ == "__main__":
    unittest.main()
