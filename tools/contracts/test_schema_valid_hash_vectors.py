"""Schema-valid intent_hash and plan_hash vectors, the Python side (CON-04; ruling A672).

REQ-INT-001, REQ-PLN-002, REQ-API-001; INV-17 (a grant binds the intent hash and the plan hash, so both must be the same bytes in every language), INV-18 (a plan that names a
prohibited or unregistered capability has no hash). Everything in the vectors is SYNTHETIC.

The older `intent_hash_vectors` and `plan_hash_vectors` hold inputs that are not documents any contract accepts. These three families hold documents that are:
  * `intent_hash_v3_vectors`: an intent-object.v3; the hash is SHA-256 of the RFC 8785 form of the whole object;
  * `plan_hash_v1_vectors`: a plan.v1; the hash is SHA-256 of the RFC 8785 form of the plan document, steps numbered 0, 1, 2 with no gap, every capability registered and not prohibited;
  * `plan_hash_v1_refused_vectors`: a plan.v1 the schema accepts and that must have NO hash (a prohibited capability, an unregistered one, a gap in the step numbers).

The Go side reads the same file (services/core/internal/vectors/families_test.go). The JCS and the hash here come from tools/check_vectors.py, which is written from the PERD text and shares
no code with the Go implementation.
"""
import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_vectors  # noqa: E402
from validate import load_schemas, make_registry, validator_for  # noqa: E402

VECTORS = json.loads((ROOT / "contracts" / "fixtures" / "golden_vectors.json").read_text(encoding="utf-8"))
PACK = json.loads((ROOT / "packs" / "fintech_v1.json").read_text(encoding="utf-8"))
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
CAPABILITIES = {c["capability_id"]: c for c in PACK["capabilities"]}


def family(name):
    vectors = VECTORS.get(name)
    if not vectors:
        raise AssertionError(f"the vector family {name} is missing or empty")
    return vectors


def errors(doc, schema):
    return [e.message for e in validator_for(schema, SCHEMAS, REGISTRY).iter_errors(doc)]


def plan_refusal(doc):
    """Why a plan has no hash (INV-18), or None. The Go side is planner.HashDocument."""
    for i, step in enumerate(doc["steps"]):
        if step["step"] != i:
            return "step_numbering"
        capability = CAPABILITIES.get(step["capability_id"])
        if capability is None:
            return "unregistered_capability"
        if capability["prohibited"]:
            return "prohibited_capability"
    return None


class SchemaValidHashVectors(unittest.TestCase):
    def test_every_intent_vector_is_a_valid_v3_object_with_the_stated_hash(self):
        for v in family("intent_hash_v3_vectors"):
            with self.subTest(v["name"]):
                self.assertEqual(errors(v["input"], "intent-object.v3"), [])
                self.assertEqual(check_vectors.jcs(v["input"]), v["expected_jcs"])
                self.assertEqual(check_vectors.sha256_of(v["expected_jcs"]), v["expected_intent_hash"])

    def test_every_plan_vector_is_a_valid_plan_that_may_be_hashed_with_the_stated_hash(self):
        for v in family("plan_hash_v1_vectors"):
            with self.subTest(v["name"]):
                self.assertEqual(errors(v["input"], "plan.v1"), [])
                self.assertIsNone(plan_refusal(v["input"]))
                self.assertEqual(check_vectors.jcs(v["input"]), v["expected_jcs"])
                self.assertEqual(check_vectors.sha256_of(v["expected_jcs"]), v["expected_plan_hash"])

    def test_every_refused_plan_is_schema_valid_and_has_no_hash(self):
        for v in family("plan_hash_v1_refused_vectors"):
            with self.subTest(v["name"]):
                self.assertEqual(errors(v["input"], "plan.v1"), [])
                self.assertEqual(plan_refusal(v["input"]), v["expected_refusal"])
                self.assertNotIn("expected_plan_hash", v)

    def test_a_plan_with_its_steps_in_another_order_has_another_hash(self):
        for v in family("plan_hash_v1_vectors"):
            if len(v["input"]["steps"]) < 2:
                continue
            with self.subTest(v["name"]):
                other = json.loads(json.dumps(v["input"]))
                other["steps"].reverse()
                self.assertNotEqual(check_vectors.sha256_of(check_vectors.jcs(other)), v["expected_plan_hash"])


if __name__ == "__main__":
    unittest.main()
