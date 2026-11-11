"""The study records of the voice subset: the consent register, the annotations and the subset report (VOI-06, REQ-VOI-004, REQ-VOI-005, AGENT-13).

The checks run on records, never on audio: audio is never in the repository, a fixture, a log or a PR (AGENT-13), and the Voice Engineer works from transcripts and annotations
(docs/voice/recording-plan-DRAFT.md sections 5 and 6, a draft until the Founder signs it). Nothing here reads a file or the network; the caller passes the records.

A session counts only with a consent in the register, a withdrawn consent leaves no session behind, every session has two distinct annotators and a locale of the five, and a
disagreement between the annotators is settled by a native-speaker tester and recorded. A session unusable because of a bystander stays in the denominator and is counted, never
dropped. Every problem of every session is listed. The plan's minimum of 50 sessions is a floor a call cannot lower (AGENT-16). All data in the tests is SYNTHETIC (AGENT-25).
"""

from collections import Counter
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field

from noema_model.providers.speech.base import Locale

SLOTS = ("amount", "counterparty", "time")
MIN_SUBSET_SESSIONS = 50  # VOI-06: "first half (at least 50 sessions)"; plan row, not a threshold of this module
RESOLVER_ROLE = "native_speaker_tester"
_LOCALES = frozenset(locale.value for locale in Locale)


@dataclass(frozen=True, slots=True)
class ConsentRecord:
    """One row of the consent register. A withdrawn consent means the recording and its derived copies are deleted within 7 days (plan section 5)."""

    consent_ref: str
    withdrawn: bool = False


@dataclass(frozen=True, slots=True)
class Resolution:
    """A native-speaker tester's reading of a session the two annotators disagreed on."""

    resolver_role: str
    resolver_ref: str
    reading: Mapping[str, str]


@dataclass(frozen=True, slots=True)
class AnnotatedSession:
    session_ref: str
    consent_ref: str
    locale: str
    script_id: str
    intended: Mapping[str, str]
    annotator_a: str
    reading_a: Mapping[str, str]
    annotator_b: str
    reading_b: Mapping[str, str]
    resolution: Resolution | None = None
    unusable_by_bystander: bool = False


@dataclass(frozen=True, slots=True)
class SubsetReport:
    ready: bool
    reasons: tuple[str, ...]
    n: int
    n_unusable: int
    per_locale: tuple[tuple[str, int], ...]
    problems: tuple[tuple[str, str], ...] = field(default=())  # (session_ref, problem), every one of them


def _blank(value: object) -> bool:
    return not isinstance(value, str) or not value.strip()


def _reading_ok(reading: object) -> bool:
    return (isinstance(reading, Mapping) and set(reading) == set(SLOTS)
            and all(isinstance(v, str) and v.strip() for v in reading.values()))


def check_session(s: AnnotatedSession, register: Mapping[str, ConsentRecord]) -> list[str]:
    """Every problem of one session, in a fixed order; an empty list means the session may count."""
    problems: list[str] = []
    if _blank(s.session_ref):
        problems.append("session_ref blank")
    if _blank(s.consent_ref):
        problems.append("consent_ref blank")
    else:
        record = register.get(s.consent_ref)
        if record is None:
            problems.append("consent_ref not in the register")
        elif record.withdrawn:
            problems.append("consent withdrawn: the recording and its copies must be deleted")
    if s.locale not in _LOCALES:  # exact match: no case folding, no aliases
        problems.append("locale not one of the five")
    if _blank(s.annotator_a) or _blank(s.annotator_b) or s.annotator_a == s.annotator_b:
        problems.append("two distinct annotators are required")
    if not _reading_ok(s.intended):
        problems.append("intended slots malformed")
    if not _reading_ok(s.reading_a):
        problems.append("annotator A reading malformed")
    if not _reading_ok(s.reading_b):
        problems.append("annotator B reading malformed")
    if _reading_ok(s.reading_a) and _reading_ok(s.reading_b) and dict(s.reading_a) != dict(s.reading_b):
        if s.resolution is None:
            problems.append("annotators disagree and no resolution is recorded")
        else:
            if s.resolution.resolver_role != RESOLVER_ROLE:
                problems.append("resolution must come from a native-speaker tester")
            if _blank(s.resolution.resolver_ref):
                problems.append("resolution resolver_ref blank")
            if not _reading_ok(s.resolution.reading):
                problems.append("resolution reading malformed")
    return problems


def subset_report(sessions: Sequence[AnnotatedSession], register: Mapping[str, ConsentRecord], *, min_sessions: int) -> SubsetReport:
    """Whether the recorded subset may be used: enough sessions and no session with a problem. Nothing is dropped from the count."""
    if min_sessions < MIN_SUBSET_SESSIONS:
        raise ValueError(f"min_sessions {min_sessions} is weaker than the plan's minimum {MIN_SUBSET_SESSIONS}")
    problems: list[tuple[str, str]] = []
    seen: set[str] = set()
    for s in sessions:
        for problem in check_session(s, register):
            problems.append((s.session_ref, problem))
        if s.session_ref in seen:
            problems.append((s.session_ref, "duplicate session_ref"))
        seen.add(s.session_ref)
    reasons: list[str] = []
    if len(sessions) < min_sessions:
        reasons.append("too few sessions")
    if problems:
        reasons.append("sessions with a problem")
    per_locale = Counter(s.locale for s in sessions)
    return SubsetReport(
        ready=not reasons,
        reasons=tuple(reasons),
        n=len(sessions),
        n_unusable=sum(1 for s in sessions if s.unusable_by_bystander),
        per_locale=tuple((locale.value, per_locale.get(locale.value, 0)) for locale in Locale),
        problems=tuple(problems),
    )
