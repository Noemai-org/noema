"""CON-09: message-request.v1, the body of POST /v1/conversations/{conversation_ref}/messages (PERD v1.23 section 39.2; REQ-API-001, REQ-INP-004; INV-12, AGENT-15).

Pins what the fixtures cannot say: the member list is exactly the PERD's four (nothing the session supplies: INV-12), the bounds are those of
canonical-input.v2 and clarification.v1, and a trailing newline is no ID. All values are SYNTHETIC.
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
UUID = "0b8f3c1e-52a4-4d7e-9a61-3c0f5d2e7b19"


def refused(doc: dict) -> bool:
    return bool(list(validator_for("message-request", SCHEMAS, REGISTRY).iter_errors(doc)))


class MessageRequestShape(unittest.TestCase):
    def test_members_are_exactly_the_perd_39_2_four(self):
        schema = SCHEMAS["message-request.v1.json"]
        self.assertEqual(set(schema["properties"]), {"request_id", "text", "option_id", "clarification_id"})
        self.assertEqual(schema["required"], ["request_id"])
        self.assertIs(schema["additionalProperties"], False)

    def test_text_bounds_are_those_of_canonical_input_v2(self):
        mine = SCHEMAS["message-request.v1.json"]["properties"]["text"]
        theirs = SCHEMAS["canonical-input.v2.json"]["properties"]["text"]
        self.assertEqual((mine["minLength"], mine["maxLength"]), (theirs["minLength"], theirs["maxLength"]))

    def test_the_id_patterns_are_the_shared_ones(self):
        props = SCHEMAS["message-request.v1.json"]["properties"]
        self.assertTrue(props["request_id"]["$ref"].endswith("common.v1.json#/$defs/ClientRequestId"))
        self.assertTrue(props["clarification_id"]["$ref"].endswith("common.v1.json#/$defs/ClarificationId"))
        self.assertEqual(props["option_id"]["pattern"], SCHEMAS["clarification.v1.json"]["properties"]["options"]["items"]["properties"]["option_id"]["pattern"])

    def test_unicode_length_counts_characters(self):
        self.assertFalse(refused({"request_id": UUID, "text": "é" * 2000}))
        self.assertTrue(refused({"request_id": UUID, "text": "é" * 2001}))

    def test_the_rules_that_join_members(self):
        self.assertTrue(refused({"request_id": UUID}))
        self.assertTrue(refused({"request_id": UUID, "clarification_id": "clr_01J9X5"}))
        self.assertTrue(refused({"request_id": UUID, "option_id": "opt_1"}))
        self.assertFalse(refused({"request_id": UUID, "option_id": "opt_1", "clarification_id": "clr_01J9X5"}))
        self.assertFalse(refused({"request_id": UUID, "text": "hello"}))

    def test_a_trailing_newline_is_not_an_id(self):
        self.assertTrue(refused({"request_id": UUID + "\n", "text": "hello"}))
        self.assertTrue(refused({"request_id": UUID, "option_id": "opt_1\n", "clarification_id": "clr_01J9X5"}))


if __name__ == "__main__":
    unittest.main()
