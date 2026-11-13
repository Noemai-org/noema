"""The study store checks: the bucket scan and the retention timer (VOI-05, REQ-VOI-004, AGENT-13, AGENT-16).

The recording plan (docs/voice/recording-plan-DRAFT.md section 5, noema#201; a draft until the Founder signs it) proves "no audio without consent" with a consent register and a
scan of the bucket: every study object carries a `consent_ref` that is in the register, and the count of objects without one is zero. It deletes audio a set time after analysis
and a set time after a withdrawal. This module holds no audio and reads no bucket: the caller lists the objects and passes them in. Nothing here deletes anything; it says what must
be deleted and when, and the Founders (who alone can read the bucket) act on it.

The two durations are the Founders' decisions and are not yet in a committed record (AGENTS section 2, item 3), so they are required arguments and have no default (AGENT-16).
Every record is checked by type as well as by value: a bad record is a listed violation, never a crash and never a pass. A consent that is withdrawn, absent from the register or
malformed fails closed.
"""

from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from datetime import datetime, timedelta

from noema_model.voice.study import ConsentRecord


@dataclass(frozen=True, slots=True)
class StudyObject:
    """One object of the study bucket, as the caller lists it. The key and the consent ref are not printed in a traceback (AGENT-13)."""

    object_key: str = field(repr=False)
    consent_ref: str | None = field(repr=False)
    analysed_at: datetime | None = None
    withdrawn_at: datetime | None = None
    deleted_at: datetime | None = None  # set once the deletion is logged


@dataclass(frozen=True, slots=True)
class ScanReport:
    ok: bool
    n_objects: int
    n_without_consent: int
    violations: tuple[tuple[str, str], ...]  # (object_key or "#<position>", what is wrong and what to do), every one of them


@dataclass(frozen=True, slots=True)
class Due:
    object_key: str
    reason: str
    due_at: datetime
    overdue: bool  # True when `now` is past `due_at`, not merely at it


def _blank(value: object) -> bool:
    return not isinstance(value, str) or not value.strip()


def _valid_marker(value: object) -> bool:
    return isinstance(value, datetime) and value.tzinfo is not None and value.utcoffset() is not None


def _deleted(value: object) -> bool:
    """A deletion counts only when the marker is a timezone-aware datetime; "", False, 0 or a string never prove a deletion."""
    return value is not None and _valid_marker(value)


def scan_objects(objects: Sequence[StudyObject], register: Mapping[str, ConsentRecord]) -> ScanReport:
    """The bucket scan: an object without a consent ref, with one that is not in the register, or whose consent was withdrawn and which is not deleted, is a violation."""
    violations: list[tuple[str, str]] = []
    n_without = 0
    for position, o in enumerate(objects):
        if not isinstance(o, StudyObject):
            violations.append((f"#{position}", "not a study object"))
            continue
        label = o.object_key if not _blank(o.object_key) else f"#{position}"
        if o.deleted_at is not None and not _valid_marker(o.deleted_at):
            violations.append((label, "deleted_at malformed: treat the object as not deleted"))
        if _blank(o.object_key):
            violations.append((label, "object_key blank or not text"))
        if _blank(o.consent_ref):
            n_without += 1
            violations.append((label, "no consent_ref: delete the object and log it"))
        elif (ref := str(o.consent_ref)) not in register:
            violations.append((label, "consent_ref not in the register: delete the object and log it"))
        else:
            record = register[ref]
            if not isinstance(record, ConsentRecord) or record.consent_ref != ref or type(record.withdrawn) is not bool:
                violations.append((label, "consent record malformed: treat as no consent"))
            elif record.withdrawn and not _deleted(o.deleted_at):
                violations.append((label, "consent withdrawn: the object must be deleted"))
    return ScanReport(not violations, len(objects), n_without, tuple(violations))


def _aware(value: object, name: str) -> datetime:
    if not isinstance(value, datetime) or value.tzinfo is None or value.utcoffset() is None:
        raise ValueError(f"{name} must be a timezone-aware datetime")
    return value


def _duration(value: object, name: str) -> timedelta:
    if not isinstance(value, timedelta) or value <= timedelta(0):
        raise ValueError(f"{name} must be a positive timedelta")
    return value


def deletions_due(objects: Sequence[StudyObject], now: datetime, *, retention_after_analysis: timedelta, withdrawal_deadline: timedelta) -> tuple[Due, ...]:
    """The objects whose deletion is due at `now`: audio a set time after analysis, and a set time after a withdrawal; the earlier of the two applies.

    An object that is already deleted, or has neither been analysed nor withdrawn, is never due. Times must be timezone-aware (a naive time is refused, not guessed).
    """
    now = _aware(now, "now")
    retention = _duration(retention_after_analysis, "retention_after_analysis")
    withdrawal = _duration(withdrawal_deadline, "withdrawal_deadline")
    due: list[Due] = []
    for o in objects:
        if not isinstance(o, StudyObject):
            raise TypeError("every item must be a StudyObject")
        if o.deleted_at is not None:
            _aware(o.deleted_at, "deleted_at")  # a malformed marker is refused, never taken as a deletion
            continue
        candidates: list[tuple[datetime, str]] = []
        if o.analysed_at is not None:
            candidates.append((_aware(o.analysed_at, "analysed_at") + retention, "retention after analysis has run out"))
        if o.withdrawn_at is not None:
            candidates.append((_aware(o.withdrawn_at, "withdrawn_at") + withdrawal, "withdrawn: the deletion deadline has been reached"))
        if not candidates:
            continue
        due_at, reason = min(candidates, key=lambda c: c[0])
        if due_at <= now:
            due.append(Due(o.object_key, reason, due_at, now > due_at))
    return tuple(due)
