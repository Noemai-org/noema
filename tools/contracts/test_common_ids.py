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


class WhatTheSchemaLeavesToTheRecordGuard(unittest.TestCase):
    """The Security Reviewer's asks on noema#564. The schema checks the shape; the letter rule (a body of digits only is no ID) lives in the Record guard (`record/payloadguard`, `bodyHasLetter`)
    and the gateway, not here. These tests pin that division so nobody moves it by accident: if the schema ever refuses these, the guard's test and this one change together."""

    def test_an_all_digit_body_is_accepted_by_the_schema_and_refused_by_the_guard(self):
        self.assertTrue(valid("IdempotencyKeyId", "idk_v1_" + "4" * 64))  # guard_test.go: "an idempotency key of digits" is refused
        self.assertTrue(valid("RequestId", "req_" + "4" * 32))  # guard_test.go: "an all-digit minted request id" is refused
        self.assertTrue(valid("ClientRequestId", "41111111-1111-1111-1111-111111111111"))  # guard_test.go: "a UUID of digits" is refused

    def test_a_value_with_a_unicode_digit_or_a_trailing_newline_is_not_an_id(self):
        arabic_four = "٤"  # a decimal digit of another script: [0-9] does not match it
        for name, definition, value in (
            ("a key with a Unicode digit", "IdempotencyKeyId", "idk_v1_" + arabic_four * 64),
            ("a minted request id with a Unicode digit", "RequestId", "req_" + arabic_four * 26),
            ("a UUID with a Unicode digit", "ClientRequestId", UUID.replace("0", arabic_four, 1)),
        ):
            with self.subTest(name):
                self.assertFalse(valid(definition, value))

    def test_the_generated_python_models_anchor_the_whole_string(self):
        """The tool layer's jsonschema reads `$` as 'before a trailing newline' too, so the guard against `key + newline` is the generated model: `\\A ... \\Z`."""
        text = (Path(__file__).resolve().parents[2] / "services" / "model" / "noema_model" / "contracts" / "common_v1.py").read_text(encoding="utf-8")
        self.assertIn("common.v1.json#$defs/IdempotencyKeyId", text)
        self.assertIn(r"\\Aidk_v1_[0-9a-f]{64}\\Z", text)
