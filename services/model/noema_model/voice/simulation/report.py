"""The per-locale simulation report that precedes every field session (VOI-17, REQ-VOI-007).

It records counts only: no audio, no transcript and no personal identifier (AGENT-13). It is "ready" only with an approving
native-speaker review for the same locale and with zero actions on a low-confidence key slot (REQ-VOI-003, REQ-AMB-007).
Being ready never passes the field gate (REQ-VOI-005); it only allows a session to be scheduled.
"""

import json
from dataclasses import dataclass

from noema_model.providers.speech.base import Locale


@dataclass(frozen=True, slots=True)
class ScenarioResult:
    scenario: str
    utterances: int
    key_slots_correct: int
    clarified: int
    acted_on_low_confidence: int

    def __post_init__(self) -> None:
        counts = (self.key_slots_correct, self.clarified, self.acted_on_low_confidence)
        if self.utterances < 1 or min(counts) < 0:
            raise ValueError("scenario counts out of range")
        if self.key_slots_correct + self.clarified > self.utterances or self.acted_on_low_confidence > self.utterances:
            raise ValueError("scenario counts exceed the number of utterances")


@dataclass(frozen=True, slots=True)
class NativeReview:
    reviewer_ref: str  # an opaque reference to the tester, never a name
    locale: Locale
    approved: bool


@dataclass(frozen=True, slots=True)
class SimulationReport:
    locale: Locale
    seed: int
    results: tuple[ScenarioResult, ...]
    native_review: NativeReview | None


def ready_for_field(report: SimulationReport) -> bool:
    review = report.native_review
    if not report.results or review is None or review.locale is not report.locale or not review.approved:
        return False
    return all(result.acted_on_low_confidence == 0 for result in report.results)


def report_json(report: SimulationReport) -> str:
    review = report.native_review
    document = {
        "synthetic": True,
        "locale": report.locale.value,
        "seed": report.seed,
        "results": [
            {
                "scenario": r.scenario,
                "utterances": r.utterances,
                "key_slots_correct": r.key_slots_correct,
                "clarified": r.clarified,
                "acted_on_low_confidence": r.acted_on_low_confidence,
            }
            for r in report.results
        ],
        "native_review": None
        if review is None
        else {"reviewer_ref": review.reviewer_ref, "locale": review.locale.value, "approved": review.approved},
    }
    return json.dumps(document, sort_keys=True, separators=(",", ":"))
