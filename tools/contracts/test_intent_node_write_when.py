"""Checks for the optional `write_when` member of intent-node.v1 and the optional `case_reasons` member of pack.v1 (A364, interim, relayed, not yet committed; REQ-CAP-001, INV-03).

`write_when` is a predicate in the pack's predicate language over the intent node's slots, evaluated on the admitted intent's resolved slots (an absent slot is null): true means
the intent needs write evaluation. It is NOT a member of evidence-source.v1, and neither are `may_write` or `produced_by` (evidence-descriptor.v1) any more (A363). The generated
Pydantic models accept an explicit null for an optional member, so a null cannot be an invalid FIXTURE: it is held here, against the schema. All values are SYNTHETIC.
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
PACK = json.loads((FIX / "pack" / "synthetic_fintech.json").read_text())


def errors(name, doc):
    return list(validator_for(name, SCHEMAS, REGISTRY).iter_errors(doc))


class WriteWhen(unittest.TestCase):
    def test_the_member_is_optional_and_a_predicate_string(self):
        self.assertNotIn("write_when", NODE)
        self.assertEqual(errors("intent-node", NODE), [])
        doc = copy.deepcopy(NODE)
        doc["write_when"] = "question_type == 'ESCALATION_REQUEST' or requested_remedy != null"
        self.assertEqual(errors("intent-node", doc), [])

    def test_null_empty_and_every_other_type_is_refused(self):
        for bad in (None, "", True, False, 1, 0, [], ["true"], {}, {"a": "true"}):
            doc = copy.deepcopy(NODE)
            doc["write_when"] = bad
            self.assertTrue(errors("intent-node", doc), f"write_when {bad!r} is accepted")

    def test_the_evidence_source_and_descriptor_carry_none_of_it(self):
        props = SCHEMAS["evidence-source.v1.json"]["properties"]
        self.assertFalse({"may_write", "write_when"} & set(props))
        self.assertNotIn("produced_by", SCHEMAS["evidence-descriptor.v1.json"]["properties"])
        self.assertIn("write_when", SCHEMAS["intent-node.v1.json"]["properties"])


class CaseReasons(unittest.TestCase):
    def test_the_member_is_optional_a_non_empty_list_of_codes_with_an_optional_label(self):
        self.assertNotIn("case_reasons", PACK)
        self.assertEqual(errors("pack", PACK), [])
        for good in ([{"code": "A_B1"}], [{"code": "A_B1", "label": "SYNTHETIC: a label"}, {"code": "C"}]):
            doc = copy.deepcopy(PACK)
            doc["case_reasons"] = good
            self.assertEqual(errors("pack", doc), [], good)

    def test_null_and_malformed_values_are_refused(self):
        for bad in (None, [], {}, "A", [None], ["A"], [{"code": ""}], [{"code": "a"}], [{"code": "A", "label": ""}], [{"code": "A", "label": 1}], [{"label": "x"}]):
            doc = copy.deepcopy(PACK)
            doc["case_reasons"] = bad
            self.assertTrue(errors("pack", doc), f"case_reasons {bad!r} is accepted")


if __name__ == "__main__":
    unittest.main()
