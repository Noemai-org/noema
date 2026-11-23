"""Checks for `mode: "SLOT"` of clarification.v1 (A582, A563, A591, interim; REQ-AMB-003, REQ-AMB-004; INV-02, AGENT-09, AGENT-15).

A SLOT clarification asks the customer for the value of one slot (for example the time to search in) when there is nothing to choose from: no candidate set was read, so `candidate_set_id` and
`candidate_set_version` are not required and `options` is empty. In every other mode (the member absent, OPTIONS, ATTRIBUTE) those three members are required, so a candidate card can never arrive
without choices (the Security Reviewer's condition). The question text is a pack template named by `question_key`; nothing here carries wording. All values are SYNTHETIC.
"""
import copy
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
FIX = ROOT / "contracts" / "fixtures" / "valid" / "clarification"
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
CANDIDATE_MEMBERS = ("candidate_set_id", "candidate_set_version", "options")


def errors(doc):
    return list(validator_for("clarification", SCHEMAS, REGISTRY).iter_errors(doc))


def options_card():
    return json.loads((FIX / "two_options.json").read_text())


def slot_card():
    d = copy.deepcopy(options_card())
    d["mode"] = "SLOT"
    d["slot"] = "when"
    d["question_key"] = "fin.clarify.when"
    d["options"] = []
    for k in ("candidate_set_id", "candidate_set_version", "attribute"):
        d.pop(k, None)
    return d


class SlotMode(unittest.TestCase):
    def test_the_mode_enum_lists_slot(self):
        self.assertIn("SLOT", SCHEMAS["clarification.v1.json"]["properties"]["mode"]["enum"])

    def test_a_slot_card_needs_no_candidate_set_and_no_options(self):
        self.assertEqual(errors(slot_card()), [])

    def test_a_slot_card_may_still_carry_an_empty_candidate_set_reference(self):
        d = slot_card()
        d["candidate_set_id"], d["candidate_set_version"] = options_card()["candidate_set_id"], 1
        self.assertEqual(errors(d), [])

    def test_a_slot_card_with_options_or_an_attribute_is_refused(self):
        d = slot_card()
        d["options"] = options_card()["options"]
        self.assertTrue(errors(d), "SLOT with options is accepted")
        d = slot_card()
        d["attribute"] = "occurred_at"
        self.assertTrue(errors(d), "SLOT with an attribute is accepted")

    def test_a_slot_card_still_needs_its_other_required_members(self):
        for k in ("clarification_id", "slot", "question_key", "allow_none", "none_option_id", "expires_at", "round"):
            d = slot_card()
            del d[k]
            self.assertTrue(errors(d), f"SLOT without {k} is accepted")


class OtherModes(unittest.TestCase):
    def test_a_card_with_no_mode_an_options_mode_or_an_attribute_mode_still_needs_its_choices(self):
        for mode in (None, "OPTIONS"):
            for k in CANDIDATE_MEMBERS:
                d = copy.deepcopy(options_card())
                if mode:
                    d["mode"] = mode
                d.pop("attribute", None)
                del d[k]
                self.assertTrue(errors(d), f"mode {mode!r} without {k} is accepted")
        for p in sorted(FIX.glob("*attribute*.json")):
            for k in CANDIDATE_MEMBERS:
                d = json.loads(p.read_text())
                del d[k]
                self.assertTrue(errors(d), f"{p.name} without {k} is accepted")

    def test_the_existing_valid_cards_are_unchanged(self):
        for p in sorted(FIX.glob("*.json")):
            self.assertEqual(errors(json.loads(p.read_text())), [], p.name)

    def test_an_unknown_mode_is_still_refused(self):
        d = copy.deepcopy(options_card())
        d["mode"] = "FREE_TEXT"
        self.assertTrue(errors(d))


if __name__ == "__main__":
    unittest.main()
