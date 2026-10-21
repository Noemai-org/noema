"""Typed rejections. The harness rejects; it never repairs, guesses or falls back to a heuristic (REQ-INT-002).

Each rejection carries a code from /contracts/schemas/errors.v1.json, a retryable flag that follows the catalogue
(only a transient provider or dependency failure is retryable), a safe fixed message with no customer text and no run of
five digits, and the CallRecord when a call was made.
"""

from enum import StrEnum
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from noema_model.harness.accounting import CallRecord


class Reason(StrEnum):
    NO_PROVIDER_ANSWERED = "NO_PROVIDER_ANSWERED"
    OUTPUT_MISMATCH = "OUTPUT_MISMATCH"
    OUTPUT_TRUNCATED = "OUTPUT_TRUNCATED"
    OUTPUT_REFUSED = "OUTPUT_REFUSED"
    OUTPUT_TYPE_UNSAFE = "OUTPUT_TYPE_UNSAFE"
    NO_ABSTENTION = "NO_ABSTENTION"
    QUESTION_INVALID = "QUESTION_INVALID"
    FIELD_INVALID = "FIELD_INVALID"
    TASK_NOT_ELIGIBLE = "TASK_NOT_ELIGIBLE"
    ROUTE_MISSING = "ROUTE_MISSING"


_CODES = {
    Reason.NO_PROVIDER_ANSWERED: "PROVIDER_UNAVAILABLE",
    Reason.OUTPUT_MISMATCH: "SCHEMA_INVALID",
    Reason.OUTPUT_TRUNCATED: "SCHEMA_INVALID",
    Reason.OUTPUT_REFUSED: "SCHEMA_INVALID",
    Reason.OUTPUT_TYPE_UNSAFE: "INPUT_INVALID",
    Reason.NO_ABSTENTION: "INPUT_INVALID",
    Reason.QUESTION_INVALID: "INPUT_INVALID",
    Reason.FIELD_INVALID: "INPUT_INVALID",
    Reason.TASK_NOT_ELIGIBLE: "INPUT_INVALID",
    Reason.ROUTE_MISSING: "INTERNAL_SAFETY_BLOCK",
}

_MESSAGES = {
    Reason.NO_PROVIDER_ANSWERED: "No configured provider produced an acceptable answer.",
    Reason.OUTPUT_MISMATCH: "The provider output did not match the required schema and was rejected.",
    Reason.OUTPUT_TRUNCATED: "The provider output was cut short and was rejected.",
    Reason.OUTPUT_REFUSED: "The provider declined to answer and nothing was used.",
    Reason.OUTPUT_TYPE_UNSAFE: "The requested output type does not meet the schema-forcing rules.",
    Reason.NO_ABSTENTION: "A choice question must offer the none-or-other option.",
    Reason.QUESTION_INVALID: "A question is malformed.",
    Reason.FIELD_INVALID: "A data field is malformed.",
    Reason.TASK_NOT_ELIGIBLE: "This task is not eligible for a decision model.",
    Reason.ROUTE_MISSING: "No provider route is configured for this task.",
}

_RETRYABLE = frozenset({"PROVIDER_UNAVAILABLE", "DEPENDENCY_UNAVAILABLE"})


class HarnessRejection(Exception):
    def __init__(self, reason: Reason, call: "CallRecord | None" = None) -> None:
        super().__init__(f"{_CODES[reason]}: {reason.value}")
        self.reason = reason
        self.code = _CODES[reason]
        self.retryable = self.code in _RETRYABLE
        self.call = call

    def error_body(self, correlation_id: str) -> dict[str, object]:
        """The ErrorBody of errors.v1.json for this rejection."""
        return {
            "code": self.code,
            "message": _MESSAGES[self.reason],
            "retryable": self.retryable,
            "correlation_id": correlation_id,
        }
