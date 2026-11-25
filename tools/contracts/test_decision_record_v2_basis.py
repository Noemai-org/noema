"""Checks for the three basis members of decision-record.v2 (A574, A577, interim; REQ-TRU-001, REQ-TRU-007, INV-21; SoT 43.2).

`evidence_snapshot_id`, `policy_version` and `evidence_freshness` are REQUIRED KEYS of every Decision Record. Their VALUE is null for an outcome that was reached before the evidence and the
policy were evaluated (REJECT, CLARIFY, WAIT, ESCALATE, UNKNOWN): a null claims no evaluation. For ALLOW and CONFIRM the value is non-null, so neither can carry a missing basis. No outcome may omit
a key, and no marker is added. All values are SYNTHETIC.
"""
import copy
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
FIX = ROOT / "contracts" / "fixtures" / "valid" / "decision-record.v2"
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
BASIS = ("evidence_snapshot_id", "policy_version", "evidence_freshness")
EARLY = {"REJECT": "reject_prohibited.json", "CLARIFY": "clarify_sot_example.json", "WAIT": "wait_with_reevaluate_at.json", "ESCALATE": "escalate_two_tiers.json", "UNKNOWN": "unknown_internal_error.json"}
ACTION = {"ALLOW": "allow.json", "CONFIRM": "confirm.json"}


def doc(name):
    return json.loads((FIX / name).read_text())


def errors(d):
    return list(validator_for("decision-record.v2", SCHEMAS, REGISTRY).iter_errors(d))


def nulled(d, *keys):
    out = copy.deepcopy(d)
    for k in keys:
        out[k] = None
    return out


class EarlyOutcomes(unittest.TestCase):
    def test_every_early_outcome_may_carry_null_for_all_three_basis_members(self):
        for outcome, name in EARLY.items():
            d = nulled(doc(name), *BASIS)
            self.assertEqual(d["outcome"], outcome)
            self.assertEqual(errors(d), [], f"{outcome} with a null basis is refused")

    def test_every_early_outcome_may_carry_each_member_null_on_its_own(self):
        for outcome, name in EARLY.items():
            for k in BASIS:
                self.assertEqual(errors(nulled(doc(name), k)), [], f"{outcome} with a null {k} is refused")

    def test_an_early_outcome_with_a_real_basis_is_still_valid(self):
        for outcome, name in EARLY.items():
            self.assertEqual(errors(doc(name)), [], outcome)


class ActionOutcomes(unittest.TestCase):
    def test_allow_and_confirm_refuse_a_null_in_any_basis_member(self):
        for outcome, name in ACTION.items():
            self.assertEqual(errors(doc(name)), [], outcome)
            for k in BASIS:
                self.assertTrue(errors(nulled(doc(name), k)), f"{outcome} with a null {k} is accepted")
            self.assertTrue(errors(nulled(doc(name), *BASIS)), f"{outcome} with a null basis is accepted")


class NoOmission(unittest.TestCase):
    def test_no_outcome_may_omit_a_basis_key(self):
        for name in {**EARLY, **ACTION}.values():
            for k in BASIS:
                d = doc(name)
                del d[k]
                self.assertTrue(errors(d), f"{d['outcome']} without {k} is accepted")

    def test_a_value_of_the_wrong_shape_is_refused_for_every_outcome(self):
        bad = {"evidence_snapshot_id": ("", "evs", 1, [], {}), "policy_version": ("", "x", 1, [], {}), "evidence_freshness": ("", "x", 1, {}, [None], [{}])}
        for name in {**EARLY, **ACTION}.values():
            for k, values in bad.items():
                for v in values:
                    d = doc(name)
                    d[k] = v
                    self.assertTrue(errors(d), f"{d['outcome']} with {k}={v!r} is accepted")


class Versions(unittest.TestCase):
    def test_version_1_is_unchanged(self):
        v1 = SCHEMAS["decision-record.v1.json"]
        for k in BASIS:
            self.assertIn(k, v1["required"])
        d = json.loads((ROOT / "contracts" / "fixtures" / "valid" / "decision-record" / "reject_prohibited.json").read_text())
        d["evidence_snapshot_id"] = None
        self.assertTrue(list(validator_for("decision-record.v1", SCHEMAS, REGISTRY).iter_errors(d)))


if __name__ == "__main__":
    unittest.main()
