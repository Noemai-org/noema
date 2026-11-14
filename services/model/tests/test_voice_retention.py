"""VOI-05, REQ-VOI-004, AGENT-13: the checks that prove "no audio without consent" and "deleted on time" for the study store.

Plan (docs/voice/recording-plan-DRAFT.md section 5, noema#201, a draft until the Founder signs it): the evidence is a consent register and a bucket scan (every object carries a
`consent_ref` that is in the register; the count of study objects without one is zero), and audio is deleted a set time after analysis and a set time after a withdrawal. The
durations are the Founders' decisions and are not yet in a committed record, so they are arguments here, never defaults (AGENT-16, AGENTS section 2 item 3). This module holds
no audio and reads no bucket: the caller lists the objects. All data in these tests is SYNTHETIC (AGENT-25).
"""

from datetime import UTC, datetime, timedelta, timezone

import pytest

from noema_model.voice.retention import StudyObject, deletions_due, scan_objects
from noema_model.voice.study import ConsentRecord

NOW = datetime(2026, 12, 1, 12, 0, tzinfo=UTC)
REGISTER = {f"c-{i}": ConsentRecord(f"c-{i}") for i in range(20)}
RETAIN = timedelta(days=90)
WITHDRAW = timedelta(days=7)


def obj(i: int = 0, **over: object) -> StudyObject:
    fields: dict[str, object] = dict(object_key=f"study/o-{i}", consent_ref=f"c-{i}", analysed_at=None, withdrawn_at=None, deleted_at=None)
    fields.update(over)
    return StudyObject(**fields)  # type: ignore[arg-type]


def due(objects: list[StudyObject], now: datetime = NOW) -> list[tuple[str, str, bool]]:
    return [(d.object_key, d.reason, d.overdue) for d in deletions_due(objects, now, retention_after_analysis=RETAIN, withdrawal_deadline=WITHDRAW)]


# ---- the bucket scan -----------------------------------------------------------------------------------------------------------------------------------


def test_objects_that_all_carry_a_registered_consent_pass_the_scan() -> None:
    report = scan_objects([obj(i) for i in range(5)], REGISTER)
    assert report.ok and report.n_objects == 5 and report.n_without_consent == 0 and report.violations == ()


@pytest.mark.parametrize("ref", [None, "", "   ", "\t"])
def test_an_object_without_a_consent_ref_is_a_violation_and_is_counted(ref: object) -> None:
    report = scan_objects([obj(0), obj(1, consent_ref=ref)], REGISTER)
    assert not report.ok and report.n_without_consent == 1
    assert report.violations == (("study/o-1", "no consent_ref: delete the object and log it"),)


def test_a_consent_ref_missing_from_the_register_is_a_violation() -> None:
    report = scan_objects([obj(0, consent_ref="c-unknown")], REGISTER)
    assert not report.ok and report.violations == (("study/o-0", "consent_ref not in the register: delete the object and log it"),)


def test_an_object_of_a_withdrawn_consent_that_is_still_there_is_a_violation() -> None:
    register = {**REGISTER, "c-0": ConsentRecord("c-0", withdrawn=True)}
    report = scan_objects([obj(0)], register)
    assert not report.ok and report.violations == (("study/o-0", "consent withdrawn: the object must be deleted"),)


def test_a_deleted_object_of_a_withdrawn_consent_is_not_a_violation() -> None:
    register = {**REGISTER, "c-0": ConsentRecord("c-0", withdrawn=True)}
    assert scan_objects([obj(0, deleted_at=NOW)], register).ok


@pytest.mark.parametrize("record", [None, "c-0", 3, ConsentRecord("c-other"), ConsentRecord("c-0", withdrawn=None)])  # type: ignore[arg-type]
def test_a_register_entry_that_is_not_a_real_consent_record_is_a_violation_not_a_crash(record: object) -> None:
    report = scan_objects([obj(0)], {**REGISTER, "c-0": record})  # type: ignore[dict-item]
    assert not report.ok and report.violations[0][1].startswith("consent record malformed")


@pytest.mark.parametrize("key", [None, "", "  ", 5, ["k"]])
def test_an_object_key_that_is_not_text_is_a_violation_not_a_crash(key: object) -> None:
    report = scan_objects([obj(0, object_key=key)], REGISTER)
    assert not report.ok and any("object_key" in problem for _, problem in report.violations)


@pytest.mark.parametrize("item", [None, "study/o-0", 7, {"object_key": "x"}])
def test_something_that_is_not_a_study_object_is_a_violation_not_a_crash(item: object) -> None:
    report = scan_objects([item], REGISTER)  # type: ignore[list-item]
    assert not report.ok and report.n_objects == 1


def test_every_violation_of_every_object_is_listed() -> None:
    report = scan_objects([obj(0, consent_ref=""), obj(1), obj(2, consent_ref="c-unknown")], REGISTER)
    assert [key for key, _ in report.violations] == ["study/o-0", "study/o-2"]


def test_an_empty_store_passes_the_scan_and_says_it_scanned_nothing() -> None:
    report = scan_objects([], REGISTER)
    assert report.ok and report.n_objects == 0


# ---- the retention timer --------------------------------------------------------------------------------------------------------------------------------


def test_nothing_is_due_before_the_deadline() -> None:
    assert due([obj(0, analysed_at=NOW - timedelta(days=89, hours=23))]) == []


def test_audio_is_due_when_the_retention_after_analysis_has_run_out() -> None:
    assert due([obj(0, analysed_at=NOW - RETAIN)]) == [("study/o-0", "retention after analysis has run out", False)]


def test_an_object_past_its_deadline_is_overdue() -> None:
    assert due([obj(0, analysed_at=NOW - RETAIN - timedelta(seconds=1))]) == [("study/o-0", "retention after analysis has run out", True)]


def test_a_withdrawal_is_due_within_its_own_shorter_deadline() -> None:
    assert due([obj(0, withdrawn_at=NOW - WITHDRAW)]) == [("study/o-0", "withdrawn: the deletion deadline has been reached", False)]
    assert due([obj(0, withdrawn_at=NOW - WITHDRAW + timedelta(seconds=1))]) == []


def test_the_earlier_of_the_two_deadlines_applies() -> None:
    got = due([obj(0, analysed_at=NOW - timedelta(days=10), withdrawn_at=NOW - timedelta(days=8))])
    assert got == [("study/o-0", "withdrawn: the deletion deadline has been reached", True)]


def test_an_object_never_analysed_and_never_withdrawn_is_never_due() -> None:
    assert due([obj(0)]) == []


def test_a_deleted_object_is_not_due() -> None:
    assert due([obj(0, analysed_at=NOW - timedelta(days=400), deleted_at=NOW - timedelta(days=1))]) == []


@pytest.mark.parametrize("bad", [datetime(2026, 1, 1), "2026-01-01", 5, 0.5])
def test_a_time_that_is_not_a_timezone_aware_datetime_is_refused(bad: object) -> None:
    with pytest.raises((ValueError, TypeError)):
        due([obj(0, analysed_at=bad)])
    with pytest.raises((ValueError, TypeError)):
        due([obj(0, withdrawn_at=bad)])


def test_an_aware_time_in_another_zone_is_compared_as_the_same_instant() -> None:
    lagos = timezone(timedelta(hours=1))
    assert due([obj(0, analysed_at=(NOW - RETAIN).astimezone(lagos))]) == [("study/o-0", "retention after analysis has run out", False)]


@pytest.mark.parametrize("now", [datetime(2026, 12, 1), None, "now", 5])
def test_now_must_be_a_timezone_aware_datetime(now: object) -> None:
    with pytest.raises((ValueError, TypeError)):
        deletions_due([], now, retention_after_analysis=RETAIN, withdrawal_deadline=WITHDRAW)  # type: ignore[arg-type]


@pytest.mark.parametrize("bad", [timedelta(0), timedelta(days=-1), 90, None, "90 days", float("nan")])
def test_the_durations_are_required_positive_timedeltas_and_have_no_default(bad: object) -> None:
    with pytest.raises((ValueError, TypeError)):
        deletions_due([], NOW, retention_after_analysis=bad, withdrawal_deadline=WITHDRAW)  # type: ignore[arg-type]
    with pytest.raises((ValueError, TypeError)):
        deletions_due([], NOW, retention_after_analysis=RETAIN, withdrawal_deadline=bad)  # type: ignore[arg-type]
    with pytest.raises(TypeError):
        deletions_due([], NOW)  # type: ignore[call-arg]


def test_a_study_object_that_is_not_a_study_object_is_refused_by_the_timer() -> None:
    with pytest.raises(TypeError):
        due([None])  # type: ignore[list-item]


def test_every_due_object_is_listed_in_the_order_given() -> None:
    got = due([obj(0, analysed_at=NOW - RETAIN), obj(1), obj(2, withdrawn_at=NOW - timedelta(days=30))])
    assert [key for key, _, _ in got] == ["study/o-0", "study/o-2"]


def test_the_repr_of_an_object_does_not_print_its_consent_ref_or_key() -> None:
    """AGENT-13: a consent ref ties a recording to a speaker; keep it out of a traceback."""
    text = repr(obj(0, consent_ref="c-SECRETREF", object_key="study/SECRETKEY"))
    assert "SECRETREF" not in text and "SECRETKEY" not in text
