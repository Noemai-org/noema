"""Checks for confirmation-answer.v1 (TRU-07; REQ-TRU-009, INV-25, INV-12, SoT §43.2a, AGENT-15): the approver's answer to a confirmation request.

The body is two members, `request_id` and `response`. The approver's principal, roles, channel and time come from the authenticated session and from the core's clock, never from the
body (INV-12; SoT §43.2a: "the approver's principal (from authentication, never from text)"), so the schema has no member that could carry one and refuses any that arrives. The response is the existing
`ConfirmationResponse` enum, referenced and not copied, so there is one list of the two words. A new contract at major 1: no existing contract changes. All values are SYNTHETIC.
"""
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import FIXTURES, ROOT, load_schemas, load_strict, make_registry, validator_for, walk  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
FILE = "confirmation-answer.v1.json"
ID_BASE = "https://contracts.noema.invalid/"
# Every member name a body must never carry: identity, role, channel and time come from the session (INV-12), the tenant and the correlation chain from the session and the request line.
NOT_IN_A_BODY = ["tenant_id", "principal_id", "principal", "approver_id", "approver_principal_id", "approver_roles", "roles", "role", "session", "authn_ref", "asserted_by", "channel", "at",
                 "correlation_id", "schema_version", "confirmation_event_id", "confirmation_request_id", "note", "reason"]


def validator():
    return validator_for("confirmation-answer", SCHEMAS, REGISTRY)


def errors(document) -> list:
    return list(validator().iter_errors(document))


class Shape(unittest.TestCase):
    """REQ-TRU-009, INV-25: strict, two required members, nothing else."""

    def setUp(self):
        self.schema = SCHEMAS[FILE]

    def test_the_schema_is_a_2020_12_object_with_its_own_id(self):
        self.assertEqual(self.schema["$schema"], "https://json-schema.org/draft/2020-12/schema")
        self.assertEqual(self.schema["$id"], ID_BASE + FILE)
        self.assertEqual(self.schema["type"], "object")

    def test_it_is_closed(self):
        self.assertIs(self.schema["additionalProperties"], False)

    def test_exactly_two_members_and_both_are_required(self):
        self.assertEqual(set(self.schema["properties"]), {"request_id", "response"})
        self.assertEqual(set(self.schema["required"]), {"request_id", "response"})
        self.assertEqual(len(self.schema["required"]), 2)

    def test_request_id_is_the_confirmation_request_id_of_common(self):
        self.assertEqual(self.schema["properties"]["request_id"], {"$ref": ID_BASE + "common.v1.json#/$defs/ConfirmationRequestId"})
        pattern = SCHEMAS["common.v1.json"]["$defs"]["ConfirmationRequestId"]["pattern"]
        self.assertTrue(pattern.startswith("^cfr_"), pattern)

    def test_response_is_the_existing_enum_and_is_not_copied(self):
        self.assertEqual(self.schema["properties"]["response"], {"$ref": ID_BASE + "enums.v1.json#/$defs/ConfirmationResponse"})
        keys = {k for _, node in walk(self.schema) if isinstance(node, dict) for k in node}
        self.assertFalse(keys & {"enum", "const"}, "an inline enum would be a second list of the two words")
        self.assertNotIn("$defs", self.schema)

    def test_the_enum_it_refers_to_is_exactly_accept_and_decline(self):
        self.assertEqual(SCHEMAS["enums.v1.json"]["$defs"]["ConfirmationResponse"]["enum"], ["ACCEPT", "DECLINE"])
        self.assertEqual(SCHEMAS["confirmation-event.v1.json"]["properties"]["response"], self.schema["properties"]["response"], "the answer and the event use the same enum")

    def test_no_member_carries_an_identity_a_role_a_channel_or_a_time(self):
        for name in NOT_IN_A_BODY:
            self.assertNotIn(name, self.schema["properties"], name)
        text = json.dumps(self.schema["properties"])
        for word in ("PrincipalId", "TenantId", "Timestamp", "Role"):
            self.assertNotIn(word, text, word)

    def test_it_is_new_and_edits_nothing(self):
        self.assertEqual(SCHEMAS["confirmation-event.v1.json"]["properties"]["confirmation_request_id"], {"$ref": ID_BASE + "common.v1.json#/$defs/ConfirmationRequestId"})
        self.assertIn("confirmation-request.v1.json", SCHEMAS)


class Accepts(unittest.TestCase):
    def test_both_words(self):
        for word in ("ACCEPT", "DECLINE"):
            self.assertEqual(errors({"request_id": "cfr_2Q8", "response": word}), [], word)

    def test_the_valid_fixtures_are_one_of_each_word(self):
        found = {load_strict(p)["response"] for p in (FIXTURES / "valid" / "confirmation-answer").glob("*.json")}
        self.assertEqual(found, {"ACCEPT", "DECLINE"})
        for p in (FIXTURES / "valid" / "confirmation-answer").glob("*.json"):
            self.assertEqual(errors(load_strict(p)), [], p.name)


class Refuses(unittest.TestCase):
    def setUp(self):
        self.good = {"request_id": "cfr_2Q8", "response": "ACCEPT"}

    def test_a_member_missing(self):
        for name in ("request_id", "response"):
            doc = dict(self.good)
            del doc[name]
            self.assertTrue(errors(doc), name)
        self.assertTrue(errors({}))

    def test_a_response_that_is_not_exactly_one_of_the_two_words(self):
        for value in ("accept", "Accept", "decline", "APPROVE", "APPROVED", "REJECT", "EXPIRED", "YES", "", " ACCEPT", "ACCEPT ", "ACCEPT\n", None, True, 1, ["ACCEPT"], {"v": "ACCEPT"}):
            self.assertTrue(errors({**self.good, "response": value}), repr(value))

    def test_a_request_id_that_is_not_a_confirmation_request_id(self):
        for value in ("cfe_01J9X7", "cnf_2Q8", "CFR_2Q8", "cfr_", "cfr_2", "cfr_" + "A" * 33, "cfr_2Q8 ", "cfr_2Q8\n", " cfr_2Q8", "cfr_2-Q8", "2Q8X", "", None, 12345, ["cfr_2Q8"]):
            self.assertTrue(errors({**self.good, "request_id": value}), repr(value))

    def test_a_request_id_at_the_edges_of_the_pattern_is_accepted(self):
        for value in ("cfr_2Q", "cfr_" + "A" * 32, "cfr_12345"):
            self.assertEqual(errors({**self.good, "request_id": value}), [], value)

    def test_every_identity_role_channel_or_time_member_in_the_body(self):
        for name in NOT_IN_A_BODY:
            self.assertTrue(errors({**self.good, name: "x"}), name)

    def test_a_body_that_is_not_an_object(self):
        for doc in ([], [self.good], "ACCEPT", None, 1, True):
            self.assertTrue(errors(doc), repr(doc))

    def test_every_invalid_fixture_is_refused_for_its_recorded_keyword(self):
        folder = FIXTURES / "invalid" / "confirmation-answer"
        expectations = load_strict(folder / "expectations.json")
        files = {p.name for p in folder.glob("*.json")} - {"expectations.json"}
        self.assertEqual(set(expectations), files)
        for name, keyword in expectations.items():
            found = {e.validator for e in errors(load_strict(folder / name))}
            self.assertIn(keyword, found, f"{name}: {found}")

    def test_the_invalid_fixtures_cover_each_way_to_get_it_wrong(self):
        folder = FIXTURES / "invalid" / "confirmation-answer"
        names = {p.stem for p in folder.glob("*.json")}
        for wanted in ("request_id_missing", "response_missing", "response_lower_case", "response_approve", "response_null", "request_id_event_prefix", "request_id_other_prefix",
                       "request_id_empty", "approver_id_in_body", "tenant_id_in_body", "principal_id_in_body", "role_in_body", "note_in_body", "body_array", "body_string"):
            self.assertIn(wanted, names)


class Documented(unittest.TestCase):
    def test_the_inventory_has_a_row_and_the_readme_a_migration_note(self):
        inventory = (ROOT / "contracts" / "INVENTORY.md").read_text(encoding="utf-8")
        self.assertIn("`confirmation-answer.v1.json`", inventory)
        readme = (ROOT / "contracts" / "README.md").read_text(encoding="utf-8")
        self.assertIn("confirmation-answer.v1", readme)
        self.assertIn("Migration note", readme.split("confirmation-answer.v1", 1)[1])


if __name__ == "__main__":
    unittest.main()
