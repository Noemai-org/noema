"""PCK-01 shape tests (task card, 'Tests to write FIRST'). Run: python -m unittest discover -s packs/tests

Proves REQ-CAP-001, REQ-AG-001, REQ-NFR-005 and SoT 15.3 / 15.4 against the four pack files.
The predicate strings are only checked for being non-prose here; they are parsed and type-checked by
internal/predicate (T6.1) and packcheck (PCK-02).
"""
import json
import re
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
PACKS = ROOT / "packs"
FINTECH = PACKS / "fintech_v1.json"
CONFORMANCE = sorted((PACKS / "conformance").glob("*.json"))
ALL = [FINTECH, *CONFORMANCE]
PROFILE_FIELDS = {"profile_id", "risk_class", "authority", "policy_templates", "clocks", "approvals", "prohibited_capabilities",
                  "evidence_required", "max_evidence_age_seconds", "data_sensitivity"}
OUTCOME_FIELDS = {"outcome_id", "intent_nodes", "success_predicate", "intermediate_states", "terminal_states", "settle_window_seconds",
                  "verification_sources", "compensation", "explanation_templates", "sla"}
VENDORS = {"nibss", "flutterwave", "paystack", "interswitch", "temenos", "finacle", "flexcube", "tushe", "salesforce", "zendesk",
           "servicenow", "oracle", "sap", "mambu", "thoughtmachine", "appzone", "remita"}
PREDICATE = re.compile(r"^(true|false|[A-Za-z0-9_.'\[\],()=!<> \-]+)$")


def load(path):
    return json.loads(path.read_text())


class Fintech(unittest.TestCase):
    pack = load(FINTECH)

    def test_exactly_two_intent_nodes_and_no_upper_case_class(self):  # DEC C2; REQ-CAP-001
        ids = [n["node_id"] for n in self.pack["intent_nodes"]]
        self.assertEqual(ids, ["fin.payments.exception.investigate", "fin.meta.out_of_scope"])
        self.assertNotIn("intent_classes", self.pack)
        blob = json.dumps(self.pack)
        for legacy in ("TRANSFER_FUNDS", "VERIFY_TRANSACTION", "DISPUTE_TRANSACTION", "RECOVER_FUNDS", "VIEW_PAYMENT_STATUS",
                       "VIEW_TRANSFER_STATUS", "SELECT_BENEFICIARY", "REQUEST_REFUND", "REPORT_UNAUTHORIZED_TRANSACTION", "INVESTIGATE_EXCEPTION"):
            self.assertNotIn(legacy, blob)

    def test_out_of_scope_node_has_no_slots_profile_or_outcome(self):  # PUS-03
        node = self.pack["intent_nodes"][1]
        self.assertEqual((node["slots"], node["trust_profile"], node["outcome_definitions"]), ([], None, []))

    def test_investigate_slots(self):  # DEC C2 brief
        slots = {s["name"]: s for s in self.pack["intent_nodes"][0]["slots"]}
        self.assertEqual(set(slots), {"subject_resource", "amount", "counterparty", "when", "question_type", "requested_remedy"})
        self.assertEqual(slots["question_type"]["vocabulary"],
                         ["STATUS", "WHY", "RESEND_ADVICE", "REVERSAL_STATUS", "ESCALATION_REQUEST", "CASE_STATUS", "RECONSTRUCT"])
        self.assertEqual(slots["requested_remedy"]["references"], "capability")

    def test_every_trust_profile_carries_the_sot_fields(self):  # SoT 15.3
        for p in self.pack["trust_profiles"]:
            self.assertEqual(set(p), PROFILE_FIELDS)

    def test_profile_prohibits_all_eight_prohibited_capabilities(self):  # REQ-CAP-002
        prohibited = {c["capability_id"] for c in self.pack["capabilities"] if c["prohibited"]}
        self.assertEqual(len(prohibited), 8)
        self.assertEqual(set(self.pack["trust_profiles"][0]["prohibited_capabilities"]), prohibited)

    def test_every_outcome_has_predicate_sources_and_settle_window(self):  # SoT 15.3, 43.7
        for o in self.pack["outcome_definitions"]:
            self.assertTrue(set(o) <= OUTCOME_FIELDS and OUTCOME_FIELDS - {"sla"} <= set(o), o["outcome_id"])
            self.assertTrue(o["success_predicate"].strip())
            self.assertGreaterEqual(len(o["verification_sources"]), 1)
            self.assertIsInstance(o["settle_window_seconds"], int)

    def test_no_evidence_source_id_names_a_vendor(self):
        for s in self.pack["evidence_sources"]:
            words = set(re.split(r"[._]", s["source_id"].lower()))
            self.assertFalse(words & VENDORS, s["source_id"])

    def test_every_policy_rule_is_an_evaluable_template_not_prose(self):  # SoT 15.3, PERD 23.4
        rules = self.pack["policy"]["rules"]
        self.assertTrue(rules)
        for r in rules:
            self.assertRegex(r["condition"], PREDICATE, r["rule_id"])
            self.assertNotRegex(r["condition"], r"\b(never|must|should|escalation queues)\b")
            self.assertFalse(r["condition"].rstrip().endswith("."))

    def test_reason_catalogue_holds_only_pack_codes(self):  # money-and-value-lists 2.3
        for r in self.pack["reason_catalogue"]:
            self.assertTrue(r["code"].startswith("fin.reason."), r["code"])
        self.assertNotIn("NONE", json.dumps(self.pack["reason_catalogue"]))

    def test_no_legacy_fields_or_tuned_thresholds(self):  # AGENT-16
        for legacy in ("domain", "version", "intent_classes", "routing_allowlist", "reason_codes", "outcomes", "action_graph"):
            self.assertNotIn(legacy, self.pack)
        blob = json.dumps(self.pack)
        for legacy in ("confidence_threshold", "requires_confirmation_on_r3", "max_dispute_amount_kobo", "precedence_ladder",
                       "default_action", "STRICT"):
            self.assertNotIn(legacy, blob)

    def test_consequence_levels_use_the_closed_enum(self):  # SoT 43.8: NONE, LOW, HIGH
        for c in self.pack["capabilities"]:
            for k, v in c["consequence_profile"].items():
                if k != "irreversibility":
                    self.assertIn(v, {"NONE", "LOW", "HIGH"}, c["capability_id"])


class References(unittest.TestCase):
    """Every ID used exists (a subset of what packcheck enforces)."""

    def check(self, path):
        p = load(path)
        caps = {c["capability_id"] for c in p["capabilities"]}
        outs = {o["outcome_id"] for o in p["outcome_definitions"]}
        srcs = {s["source_id"] for s in p["evidence_sources"]}
        clocks = {c["clock_id"] for c in p["clocks"]}
        profiles = {t["profile_id"] for t in p["trust_profiles"]}
        rules = {r["rule_id"] for r in p["policy"]["rules"]}
        nodes = {n["node_id"] for n in p["intent_nodes"]}
        codes = {r["code"] for r in p["reason_catalogue"]}
        for n in p["intent_nodes"]:
            self.assertTrue(n["trust_profile"] is None or n["trust_profile"] in profiles, path.name)
            self.assertLessEqual(set(n["outcome_definitions"]), outs, path.name)
        for t in p["trust_profiles"]:
            self.assertLessEqual(set(t["prohibited_capabilities"]), caps, path.name)
            self.assertLessEqual(set(t["evidence_required"]), srcs, path.name)
            self.assertLessEqual(set(t["clocks"]), clocks, path.name)
            self.assertLessEqual(set(t["policy_templates"]), rules, path.name)
        for o in p["outcome_definitions"]:
            self.assertLessEqual(set(o["intent_nodes"]), nodes, path.name)
            self.assertLessEqual({v["source"] for v in o["verification_sources"]}, srcs, path.name)
            if "sla" in o:
                self.assertIn(o["sla"]["clock"], clocks, path.name)
        for c in p["capabilities"]:
            self.assertIn(c["expected_outcome"], outs, c["capability_id"])
            self.assertIn(c["verification"]["source"], srcs, c["capability_id"])
            self.assertLessEqual(set(c["policy_templates"]), rules, c["capability_id"])
        for r in p["policy"]["rules"]:
            self.assertLessEqual(set(r["applies_to"]), caps | nodes, r["rule_id"])
            if r["reason_code"].count(".") > 1:
                self.assertIn(r["reason_code"], codes, r["rule_id"])

    def test_fintech(self):
        self.check(FINTECH)

    def test_conformance(self):
        for path in CONFORMANCE:
            with self.subTest(path.name):
                self.check(path)


class Conformance(unittest.TestCase):
    def test_three_conformance_packs(self):
        self.assertEqual([p.name for p in CONFORMANCE],
                         ["conformance_healthcare.json", "conformance_insurance.json", "conformance_telecom.json"])

    def test_each_stays_within_the_sot_15_4_cap(self):  # SoT 15.4
        for path in CONFORMANCE:
            p = load(path)
            with self.subTest(path.name):
                self.assertLessEqual(len(p["intent_nodes"]), 3)
                self.assertEqual(len(p["trust_profiles"]), 1)
                self.assertEqual(len(p["outcome_definitions"]), 1)
                self.assertNotIn("adapters", p)
                self.assertTrue(p["name"].endswith("(SYNTHETIC, ILLUSTRATIVE)"))
                for c in p["capabilities"]:  # registered, never executed: reads, or prohibited
                    self.assertTrue(c["prohibited"] or c["risk_class"] == "R0_read", c["capability_id"])


class AgainstTheSchema(unittest.TestCase):
    def test_all_four_validate_against_pack_v1(self):  # REQ-NFR-005
        for path in ALL:
            with self.subTest(path.name):
                out = subprocess.run([sys.executable, str(ROOT / "tools/contracts/validate.py"), "--pack", str(path)], capture_output=True, text=True)
                self.assertEqual(out.returncode, 0, out.stdout + out.stderr)


if __name__ == "__main__":
    unittest.main()
