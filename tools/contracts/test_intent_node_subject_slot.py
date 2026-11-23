"""Checks for the optional `subject_slot` member of intent-node.v1 (A520, interim; DEBT-54; REQ-INT-001, INV-14, INV-19).

`subject_slot` names the slot of the SAME node whose resolved reference is the subject of the intent (for the epoch of its resolution). It is optional: a node with no Resource slot declares
none and no document valid before is invalid now, so the file stays `v1` (AGENT-15 migration note in contracts/INVENTORY.md). The generated Pydantic models accept an explicit null for an
optional member, so a null cannot be an invalid FIXTURE: it is held here, against the schema. All values are SYNTHETIC.
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


def errors(name, doc):
    return list(validator_for(name, SCHEMAS, REGISTRY).iter_errors(doc))


class SubjectSlot(unittest.TestCase):
    def test_the_member_is_optional_and_a_slot_name(self):
        self.assertNotIn("subject_slot", NODE)
        self.assertEqual(errors("intent-node", NODE), [])
        doc = copy.deepcopy(NODE)
        doc["subject_slot"] = "subject_resource"
        self.assertEqual(errors("intent-node", doc), [])

    def test_null_empty_a_pattern_violation_and_every_other_type_is_refused(self):
        for bad in (None, "", "Subject", "1slot", "a-b", "a.b", " a", True, False, 1, 0, [], ["a"], {}, {"a": "b"}):
            doc = copy.deepcopy(NODE)
            doc["subject_slot"] = bad
            self.assertTrue(errors("intent-node", doc), f"subject_slot {bad!r} is accepted")

    def test_the_member_is_in_the_schema_and_the_node_still_refuses_an_unknown_member(self):
        self.assertIn("subject_slot", SCHEMAS["intent-node.v1.json"]["properties"])
        self.assertNotIn("subject_slot", SCHEMAS["intent-node.v1.json"]["required"])
        doc = copy.deepcopy(NODE)
        doc["subject_slots"] = "subject_resource"
        self.assertTrue(errors("intent-node", doc))


if __name__ == "__main__":
    unittest.main()
