"""The study records of the voice subset: the consent register, the annotations and the subset report (VOI-06, REQ-VOI-004, REQ-VOI-005, AGENT-13).

The checks run on records, never on audio: audio is never in the repository, a fixture, a log or a PR (AGENT-13), and the Voice Engineer works from transcripts and annotations
(docs/voice/recording-plan-DRAFT.md sections 5 and 6, a draft until the Founder signs it). Nothing here reads a file or the network; the caller passes the records.

Every record is validated by type as well as by value, so one bad record is listed as a problem and never crashes the report or fails open. A session counts only with a consent in the register, a withdrawn consent leaves no session behind, every session has two distinct annotators and a locale of the five, and a
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
    reading: Mapping[str, str] = field(repr=False)  # what a speaker said is never printed in a traceback or a diff (AGENT-13)


@dataclass(frozen=True, slots=True)
class AnnotatedSession:
    session_ref: str
    consent_ref: str
    locale: str
    script_id: str
    intended: Mapping[str, str] = field(repr=False)
    annotator_a: str
    reading_a: Mapping[str, str] = field(repr=False)
    annotator_b: str
    reading_b: Mapping[str, str] = field(repr=False)
    resolution: Resolution | None = None
    unusable_by_bystander: bool = False


@dataclass(frozen=True, slots=True)
class SubsetReport:
    ready: bool
    reasons: tuple[str, ...]
    n: int  # every session handed in, usable or not: a session lost to a bystander is a failed session in the denominator, never dropped (plan section 3)
    n_unusable: int
    n_usable: int
    n_other_locale: int  # sessions whose locale is not one of the five (each is also a problem); `per_locale` does not count them
    per_locale: tuple[tuple[str, int], ...]
    problems: tuple[tuple[str, str], ...] = field(default=())  # (session_ref or "#<position>", problem), every one of them


def _blank(value: object) -> bool:
    return not isinstance(value, str) or not value.strip()


def _same_person(a: str, b: str) -> bool:
    return a.strip().casefold() == b.strip().casefold()


def _reading_ok(reading: object) -> bool:
    return (isinstance(reading, Mapping) and set(reading) == set(SLOTS)
            and all(isinstance(v, str) and v.strip() for v in reading.values()))


def _locale_ok(locale: object) -> bool:
    return isinstance(locale, str) and locale in _LOCALES  # exact match: no case folding, no aliases


def check_session(s: AnnotatedSession, register: Mapping[str, ConsentRecord]) -> list[str]:
    """Every problem of one session, in a fixed order; an empty list means the session may count. A value of the wrong type is a problem, never an exception."""
    if not isinstance(s, AnnotatedSession):
        return ["not an annotated session"]
    problems: list[str] = []
    if _blank(s.session_ref):
        problems.append("session_ref blank")
    if _blank(s.consent_ref):
        problems.append("consent_ref blank")
    elif s.consent_ref not in register:
        problems.append("consent_ref not in the register")
    else:
        record = register[s.consent_ref]
        if not isinstance(record, ConsentRecord) or record.consent_ref != s.consent_ref or type(record.withdrawn) is not bool:
            problems.append("consent record malformed")  # fail closed: a flag that is not a real bool never counts as "not withdrawn"
        elif record.withdrawn:
            problems.append("consent withdrawn: the recording and its copies must be deleted")
    if not _locale_ok(s.locale):
        problems.append("locale not one of the five")
    if _blank(s.annotator_a) or _blank(s.annotator_b) or _same_person(s.annotator_a, s.annotator_b):
        problems.append("two distinct annotators are required")
    if not _reading_ok(s.intended):
        problems.append("intended slots malformed")
    if not _reading_ok(s.reading_a):
        problems.append("annotator A reading malformed")
    if not _reading_ok(s.reading_b):
        problems.append("annotator B reading malformed")
    if type(s.unusable_by_bystander) is not bool:
        problems.append("unusable_by_bystander must be true or false")
    if _reading_ok(s.reading_a) and _reading_ok(s.reading_b) and dict(s.reading_a) != dict(s.reading_b):
        if s.resolution is None:
            problems.append("annotators disagree and no resolution is recorded")
        elif not isinstance(s.resolution, Resolution):
            problems.append("resolution malformed")
        else:
            if s.resolution.resolver_role != RESOLVER_ROLE:
                problems.append("resolution must come from a native-speaker tester")
            if _blank(s.resolution.resolver_ref):
                problems.append("resolution resolver_ref blank")
            if not _reading_ok(s.resolution.reading):
                problems.append("resolution reading malformed")
    return problems


def subset_report(sessions: Sequence[AnnotatedSession], register: Mapping[str, ConsentRecord], *, min_sessions: int) -> SubsetReport:
    """Whether the recorded subset may be used: enough sessions, at least one that can be analysed, and no session with a problem.

    Sessions lost to a bystander count toward `n` (the plan's denominator rule) and are reported in `n_unusable`; a subset of only such sessions is not ready.
    `min_sessions` must be a real `int` (a float, a nan, a bool or None is refused) and not below the plan's minimum.
    """
    if type(min_sessions) is not int:
        raise TypeError("min_sessions must be an int")
    if min_sessions < MIN_SUBSET_SESSIONS:
        raise ValueError(f"min_sessions {min_sessions} is weaker than the plan's minimum {MIN_SUBSET_SESSIONS}")
    problems: list[tuple[str, str]] = []
    seen: set[str] = set()
    for position, s in enumerate(sessions):
        ref = s.session_ref if isinstance(s, AnnotatedSession) and not _blank(s.session_ref) else f"#{position}"
        for problem in check_session(s, register):
            problems.append((ref, problem))
        if isinstance(s, AnnotatedSession) and not _blank(s.session_ref):
            if s.session_ref in seen:
                problems.append((ref, "duplicate session_ref"))
            seen.add(s.session_ref)
    reasons: list[str] = []
    if len(sessions) < min_sessions:
        reasons.append("too few sessions")
    n_usable = sum(1 for s in sessions if isinstance(s, AnnotatedSession) and s.unusable_by_bystander is False)
    if sessions and n_usable == 0:
        reasons.append("no usable session")
    if problems:
        reasons.append("sessions with a problem")
    per_locale = Counter(s.locale for s in sessions if isinstance(s, AnnotatedSession) and _locale_ok(s.locale))
    return SubsetReport(
        ready=not reasons,
        reasons=tuple(reasons),
        n=len(sessions),
        n_unusable=sum(1 for s in sessions if isinstance(s, AnnotatedSession) and s.unusable_by_bystander is True),
        n_usable=n_usable,
        n_other_locale=sum(1 for s in sessions if not (isinstance(s, AnnotatedSession) and _locale_ok(s.locale))),
        per_locale=tuple((locale.value, per_locale.get(locale.value, 0)) for locale in Locale),
        problems=tuple(problems),
    )
