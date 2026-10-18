"""Checks for the DEC-2026-10-18 gap decisions that the schemas alone cannot state."""
import json
import re
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import FIXTURES, load_schemas, make_registry, validator_for  # noqa: E402

SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)


class RetryableErrors(unittest.TestCase):
    """DEC-2026-10-18: retryable only if the identical request can succeed with no change by the caller."""

    def setUp(self):
        self.defs = SCHEMAS["errors.v1.json"]["$defs"]

    def test_partition_of_the_catalogue(self):
        retry, non, every = (set(self.defs[k]["enum"]) for k in ("RetryableErrorCode", "NonRetryableErrorCode", "ErrorCode"))
        self.assertFalse(retry & non, "a code cannot be both")
        self.assertEqual(retry | non, every, "every code is classified exactly once")

    def test_only_transient_codes_are_retryable(self):
        self.assertEqual(set(self.defs["RetryableErrorCode"]["enum"]),
                         {"EVIDENCE_UNAVAILABLE", "TARGET_RATE_LIMITED", "TARGET_TIMEOUT", "PROVIDER_UNAVAILABLE", "DEPENDENCY_UNAVAILABLE"})

    def test_no_decision_validation_or_authorization_code_is_retryable(self):
        for code in ("SCHEMA_INVALID", "INPUT_INVALID", "UNKNOWN_ENUM_VALUE", "UNAUTHENTICATED", "SESSION_INVALID", "NOT_FOUND_OR_NOT_AUTHORIZED",
                     "TENANT_SCOPE_MISMATCH", "APPROVER_NOT_ELIGIBLE", "CAPABILITY_PROHIBITED", "WRITES_DISABLED", "GRANT_EXPIRED", "GRANT_ALREADY_SPENT",
                     "AMBIGUOUS_TARGET", "OUTCOME_UNKNOWN", "REJECTED_STALE_VERSION"):
            self.assertIn(code, self.defs["NonRetryableErrorCode"]["enum"], code)


class IdempotencyKeyForm(unittest.TestCase):
    """The golden vectors, then the PERD, define the key (DEC-2026-10-18): idk_v1_ plus 64 lower-case hex."""

    def test_every_golden_key_validates_in_a_receipt(self):
        gold = json.loads((FIXTURES / "golden_vectors.json").read_text())["idempotency_key_vectors"]
        self.assertGreaterEqual(len(gold), 2)
        key_schema = SCHEMAS["execution-receipt.v1.json"]["properties"]["idempotency_key"]
        for vector in gold:
            self.assertRegex(vector["expected_idempotency_key"], key_schema["pattern"], vector["name"])

    def test_the_old_sot_form_is_rejected(self):
        pattern = SCHEMAS["execution-receipt.v1.json"]["properties"]["idempotency_key"]["pattern"]
        self.assertIsNone(re.match(pattern, "idk_v1_sha256:" + "3e" * 32))


class ClarificationAnswerPairRule(unittest.TestCase):
    """An answer may name only the slot its clarification requested (REQ-AMB-005, DEC-2026-10-18)."""

    def test_pair_rule_catches_unrequested_slots(self):
        import tempfile
        from validate import unrequested_slots
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            (tmp / "a.clarification.json").write_text(json.dumps({"slot": "subject_resource"}))
            (tmp / "a.json").write_text(json.dumps({"answers": {"subject_resource": {}, "amount": {}}}))
            (tmp / "b.clarification.json").write_text(json.dumps({"slot": "subject_resource"}))
            (tmp / "b.json").write_text(json.dumps({"answers": {"subject_resource": {}}}))
            (tmp / "c.json").write_text(json.dumps({"answers": {"subject_resource": {}}}))
            self.assertEqual(unrequested_slots(tmp / "a.json"), {"amount"})
            self.assertEqual(unrequested_slots(tmp / "b.json"), set())
            self.assertIsNone(unrequested_slots(tmp / "c.json"), "no companion clarification, no verdict")


class RecordEventPrefix(unittest.TestCase):
    def test_recev_prefix_and_the_old_example_is_rejected(self):
        validator = validator_for("common", SCHEMAS, REGISTRY, "RecordEventId")
        self.assertFalse(list(validator.iter_errors("recev_1001")))
        self.assertTrue(list(validator.iter_errors("rec_1001")))

    def test_only_record_events_are_event_types(self):
        events = set(SCHEMAS["record-event.v1.json"]["$defs"]["RecordEventType"]["enum"])
        for reason_or_log_entry in ("INTENT_SUPERSEDED", "STATE_CHANGED", "RECORD_ACCESSED"):
            self.assertNotIn(reason_or_log_entry, events)
        for added in ("AUDIO_RETAINED", "INBOUND_EVENT_RECEIVED", "RESOLUTION_POSTED", "CONFIG_CHANGED"):
            self.assertIn(added, events)


if __name__ == "__main__":
    unittest.main()
