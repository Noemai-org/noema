"""errors.v2.json (DEC-2026-10-27-record-event-v2, D6): errors.v1 plus one code, DUPLICATE_CREATE. AGENT-15, PERD REQ-EXE-007, REQ-API-002."""
import hashlib
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from validate import load_schemas, make_registry, validator_for  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = load_schemas()
REGISTRY = make_registry(SCHEMAS)
V1_SHA256 = "4f42cdb94fd27bdf3f8f0fc1940f041ae71621206c48d52f4ef15934c1027cf7"


def defs(version: str) -> dict:
    return SCHEMAS[f"errors.v{version}.json"]["$defs"]


def error(code: str, retryable: bool) -> dict:
    return {"error": {"code": code, "message": "A case already exists for this subject.", "retryable": retryable, "correlation_id": "corr_4c21f9"}}


class ErrorsV2(unittest.TestCase):
    def test_v1_is_not_edited(self):
        self.assertEqual(hashlib.sha256((ROOT / "contracts" / "schemas" / "errors.v1.json").read_bytes()).hexdigest(), V1_SHA256)

    def test_v2_is_v1_plus_exactly_the_duplicate_create_code(self):
        self.assertEqual(set(defs("2")["ErrorCode"]["enum"]) - set(defs("1")["ErrorCode"]["enum"]), {"DUPLICATE_CREATE"})
        self.assertEqual(set(defs("1")["ErrorCode"]["enum"]) - set(defs("2")["ErrorCode"]["enum"]), set())

    def test_the_catalogue_is_partitioned_and_the_new_code_is_not_retryable(self):
        retry, non, every = (set(defs("2")[k]["enum"]) for k in ("RetryableErrorCode", "NonRetryableErrorCode", "ErrorCode"))
        self.assertFalse(retry & non)
        self.assertEqual(retry | non, every)
        self.assertEqual((len(retry), len(non)), (5, 30))
        self.assertIn("DUPLICATE_CREATE", non)

    def test_the_code_is_distinct_from_the_idempotency_and_version_codes(self):
        every = defs("2")["ErrorCode"]["enum"]
        for other in ("IDEMPOTENCY_CONFLICT", "REJECTED_STALE_VERSION"):
            self.assertIn(other, every)
        self.assertEqual(len(every), len(set(every)))

    def test_v1_rejects_the_new_code_and_v2_accepts_it(self):
        ok = error("DUPLICATE_CREATE", False)
        self.assertEqual([e.message for e in validator_for("errors.v2", SCHEMAS, REGISTRY, "ErrorEnvelope").iter_errors(ok)], [])
        self.assertTrue(list(validator_for("errors", SCHEMAS, REGISTRY, "ErrorEnvelope").iter_errors(ok)))

    def test_the_new_code_cannot_be_marked_retryable(self):
        self.assertTrue(list(validator_for("errors.v2", SCHEMAS, REGISTRY, "ErrorEnvelope").iter_errors(error("DUPLICATE_CREATE", True))))

    def test_the_go_transport_classes_use_the_catalogue_code(self):
        for rel in ("services/core/execution/controller/types.go", "services/core/adapters/case.go"):
            self.assertIn('TransportClass = "DUPLICATE_CREATE"', (ROOT / rel).read_text(), rel)


if __name__ == "__main__":
    unittest.main()
