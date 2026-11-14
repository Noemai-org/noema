"""Checks for noema#543 (ruling A203, minor bump of common.v1): the idempotency key, the core-minted request reference and the client's request ID have the shapes the core really writes.

`IdempotencyKeyId` is `idk_v1_` and the 64 lower-case hex of a SHA-256 (PERD 27.2; `platform/crypto`): 71 characters. `RequestId` is the ID the core mints for an input, `req_` and a ULID (A184,
`inputgateway`), not a client's UUID. `ClientRequestId` is the client's lower-case UUID of `canonical-input.v2`. All values are SYNTHETIC.
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)

KEY = "idk_v1_" + "ab12" * 16  # 71 characters, 64 lower-case hex
MINTED = "req_01K9ZB4M7Q2XJ5R8T3V6WCD0EF"  # req_ and a 26-character ULID
UUID = "0b8f3c1e-52a4-4d7e-9a61-3c0f5d2e7b19"


def valid(definition: str, value: str) -> bool:
    return not list(validator_for("common", SCHEMAS, REGISTRY, definition).iter_errors(value))


class IdempotencyKeyId(unittest.TestCase):
    def test_the_real_key_validates(self):
        self.assertTrue(valid("IdempotencyKeyId", KEY))

    def test_what_is_not_the_real_key_is_refused(self):
        for name, value in {
            "the old short shape": "idk_ab",
            "the old shape at its maximum": "idk_" + "a" * 32,
            "63 hex": "idk_v1_" + "a" * 63,
            "65 hex": "idk_v1_" + "a" * 65,
            "upper-case hex": "idk_v1_" + "A" * 64,
            "a non-hex letter": "idk_v1_" + "g" * 64,
            "another version": "idk_v2_" + "a" * 64,
            "no prefix": "a" * 64,
        }.items():
            with self.subTest(name):
                self.assertFalse(valid("IdempotencyKeyId", value))


class RequestId(unittest.TestCase):
    def test_the_id_the_core_mints_for_an_input_validates(self):
        self.assertTrue(valid("RequestId", MINTED))

    def test_a_client_uuid_is_not_a_request_id(self):
        self.assertFalse(valid("RequestId", UUID))

    def test_the_description_says_who_mints_it(self):
        text = SCHEMAS["common.v1.json"]["$defs"]["RequestId"]["description"]
        self.assertIn("core", text.lower())
        self.assertNotIn("client-supplied", text.lower())


class ClientRequestId(unittest.TestCase):
    def test_a_lower_case_uuid_validates(self):
        self.assertTrue(valid("ClientRequestId", UUID))

    def test_what_is_not_a_lower_case_uuid_is_refused(self):
        for name, value in {
            "upper case": UUID.upper(),
            "a core-minted ID": MINTED,
            "no dashes": UUID.replace("-", ""),
            "a trailing character": UUID + "0",
            "braces": "{" + UUID + "}",
        }.items():
            with self.subTest(name):
                self.assertFalse(valid("ClientRequestId", value))

    def test_canonical_input_v2_uses_it(self):
        ref = SCHEMAS["canonical-input.v2.json"]["properties"]["request_id"]
        self.assertTrue("$ref" in ref and ref["$ref"].endswith("#/$defs/ClientRequestId"), ref)


if __name__ == "__main__":
    unittest.main()
