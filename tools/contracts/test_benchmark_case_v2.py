"""benchmark-case.v2, benchmark-oracle.v2, benchmark-arm-case.v1 and benchmark-fixture.v2 (BEN-16, noema#260; AGENT-15; EL's two blocking findings of 10 Nov). The v1 files are not edited
(pinned below), the oracle's compatibility is stated and tested (v2 is a SUPERSET of v1 and carries no breaking change), and the arm view's role-level permitted capabilities are not the leaking
per-case pattern (F-14). SYNTHETIC fixtures only."""
import hashlib
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "contracts" / "fixtures"
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
V1_SHA256 = {
    "benchmark-case.v1.json": "1797c1486286e36f69d00c2386f08c26b6c04b894608ac70cc535778361d8f0e",
    "benchmark-oracle.v1.json": "015b2eb7e3738ef6ea97501efc3714dd53e7aa249fcb373e2952b2450703cfcf",
    "benchmark-fixture.v1.json": "59ef34ac91d3f83fd0d0db69160804c6401587f85d770765bccb34fa4c89cba8",
}
# The role-level capability set of the pack (noema#359: roles_allowed without the prohibited capabilities); customer and ops hold the same twelve.
ROLE_LEVEL = sorted([
    "fin.cap.create_escalation_case", "fin.cap.evaluate_sla_status", "fin.cap.log_complaint_reference", "fin.cap.lookup_account", "fin.cap.lookup_beneficiary", "fin.cap.lookup_transaction",
    "fin.cap.lookup_transaction_history", "fin.cap.query_settlement_status", "fin.cap.query_switch_status", "fin.cap.reconcile_transaction_state", "fin.cap.record_investigation_evidence",
    "fin.cap.route_to_internal_owner"])


def errors(name: str, document: dict) -> list:
    return list(validator_for(name, SCHEMAS, REGISTRY).iter_errors(document))


def fixtures(kind: str, folder: str) -> dict:
    return {p.name: json.loads(p.read_text()) for p in sorted((FIXTURES / kind / folder).glob("*.json")) if p.name != "expectations.json"}


class V1Untouched(unittest.TestCase):
    def test_the_v1_files_are_not_edited(self):
        for name, digest in V1_SHA256.items():
            self.assertEqual(hashlib.sha256((ROOT / "contracts" / "schemas" / name).read_bytes()).hexdigest(), digest, name)


class OracleV2(unittest.TestCase):
    def test_oracle_v2_adds_only_optional_members_and_changes_none(self):
        v1, v2 = SCHEMAS["benchmark-oracle.v1.json"], SCHEMAS["benchmark-oracle.v2.json"]
        self.assertEqual(set(v1["properties"]) - set(v2["properties"]), set())
        self.assertEqual(set(v2["required"]), set(v1["required"]))
        for name in v1["properties"]:
            self.assertEqual(v1["properties"][name], v2["properties"][name], name)

    def test_every_v1_oracle_is_a_valid_v2_oracle_so_there_is_no_breaking_change(self):
        for name, doc in fixtures("valid", "benchmark-oracle").items():
            self.assertEqual(errors("benchmark-oracle", doc), [], name)
            self.assertEqual(errors("benchmark-oracle.v2", doc), [], name)

    def test_a_v2_oracle_that_uses_a_new_member_is_not_a_v1_oracle(self):
        rich = fixtures("valid", "benchmark-oracle.v2")["rich_new_members.json"]
        self.assertEqual(errors("benchmark-oracle.v2", rich), [])
        self.assertTrue(errors("benchmark-oracle", rich))

    def test_the_description_and_the_migration_note_say_there_is_no_breaking_change(self):
        text = SCHEMAS["benchmark-oracle.v2.json"]["description"]
        self.assertIn("NO breaking change", text)
        self.assertNotIn("only once its `schema` is not required", text)
        note = (ROOT / "contracts" / "INVENTORY.md").read_text()
        self.assertIn("benchmark-oracle.v2.json` has no breaking change", note)
        self.assertNotIn("a document is v1 or v2, never both", note.split("the case contract v2 draft")[1].split("**Migration note")[0])


class CaseAndFixtureV2(unittest.TestCase):
    def test_a_v1_case_and_a_v1_fixture_are_not_v2_documents(self):
        for v1, v2 in (("benchmark-case", "benchmark-case.v2"), ("benchmark-fixture", "benchmark-fixture.v2")):
            for name, doc in fixtures("valid", v1).items():
                self.assertEqual(errors(v1, doc), [], name)
                self.assertTrue(errors(v2, doc), f"{v1}/{name} must not validate as {v2}")

    def test_the_stale_descriptions_are_corrected(self):
        case = SCHEMAS["benchmark-case.v2.json"]
        self.assertEqual(13, len(case["properties"]["category"]["enum"]))
        self.assertNotIn("The ten PERD", case["properties"]["category"]["description"])
        self.assertIn("thirteen", case["properties"]["category"]["description"])
        self.assertIn("benchmark-oracle.v2.json", case["properties"]["oracle_ref"]["description"])
        self.assertNotIn("benchmark-oracle.v1.json", case["properties"]["oracle_ref"]["description"])
        self.assertNotIn("required audit event exists", json.dumps(case))
        fixture_desc = SCHEMAS["benchmark-fixture.v2.json"]["description"]
        self.assertNotIn("matched by the leak scan", fixture_desc)  # IL: tools/ci/leakscan.py does not consume markers; the X6 scan does, and it is not built
        self.assertIn("does not consume `markers`", fixture_desc)
        self.assertIn("X6 cross-tenant scan", fixture_desc)
        self.assertIn("not built", fixture_desc)

    def test_the_readme_and_the_migration_note_name_the_fixture_contract(self):
        self.assertIn("benchmark-fixture.v2", (ROOT / "contracts" / "README.md").read_text())
        note = (ROOT / "contracts" / "INVENTORY.md").read_text().split("the case contract v2 draft")[1].split("**Migration note")[0]
        self.assertIn("benchmark-fixture.v2", note)

    def test_the_cross_field_checks_that_are_deferred_are_said_so(self):
        text = SCHEMAS["benchmark-case.v2.json"]["description"]
        for rule in ("suite_family.secondary", "kill_point", "family"):
            self.assertIn(rule, text)
        self.assertIn("not checked by this schema", text)


class ArmCaseV1(unittest.TestCase):
    def test_the_permitted_capabilities_of_the_valid_fixtures_are_the_role_level_set_not_the_leaking_pattern(self):
        # F-14 and EL on noema#260: a list that is [] for one case and exactly the expected write's capability for another encodes the answer.
        seen = {}
        for name, doc in fixtures("valid", "benchmark-arm-case").items():
            seen.setdefault(tuple(doc["principal"]["roles"]), set()).add(tuple(doc["permitted_capabilities"]))
            if doc["principal"]["roles"] in (["customer"], ["ops"]):
                self.assertEqual(sorted(doc["permitted_capabilities"]), ROLE_LEVEL, name)
        for roles, sets in seen.items():
            self.assertEqual(len(sets), 1, roles)

    def test_the_description_states_the_roles_mapping_rule(self):
        # C1 on noema#260: the arm view's four roles are closed while case v2 principal.roles is open; the mapping rule is stated.
        text = SCHEMAS["benchmark-arm-case.v1.json"]["description"]
        self.assertIn("principal.roles", text)
        self.assertIn("an arm view is not produced", text)


if __name__ == "__main__":
    unittest.main()
