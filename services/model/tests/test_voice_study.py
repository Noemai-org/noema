"""VOI-06, REQ-VOI-004, REQ-VOI-005: the study records of the voice subset (consent register, annotations) are checked before any session counts.

Plan: docs/voice/recording-plan-DRAFT.md (sections 5 and 6, still a draft until the Founder signs it): no recording without a consent in the register, a withdrawn
consent leaves no session behind, every session has two annotators and a locale, a disagreement is settled by a native-speaker tester, failed and unusable sessions stay in the
denominator. This module holds records, never audio (AGENT-13). All data in these tests is SYNTHETIC (AGENT-25).
"""

import pytest

from noema_model.voice.study import (
    MIN_SUBSET_SESSIONS,
    SLOTS,
    AnnotatedSession,
    ConsentRecord,
    Resolution,
    check_session,
    subset_report,
)

LOCALES = ("en-NG", "pcm", "yo", "ha", "ig")
READING = {"amount": "10000", "counterparty": "Ada Test", "time": "yesterday"}
REGISTER = {f"c-{i}": ConsentRecord(f"c-{i}") for i in range(200)}


def session(i: int = 0, **over: object) -> AnnotatedSession:
    fields: dict[str, object] = dict(
        session_ref=f"s-{i}", consent_ref=f"c-{i}", locale=LOCALES[i % 5], script_id="script-1", intended=dict(READING),
        annotator_a="ann-1", reading_a=dict(READING), annotator_b="ann-2", reading_b=dict(READING), resolution=None, unusable_by_bystander=False)
    fields.update(over)
    return AnnotatedSession(**fields)  # type: ignore[arg-type]


def test_a_complete_session_has_no_problem() -> None:
    assert check_session(session(), REGISTER) == []


@pytest.mark.parametrize("ref", ["", "   ", "\t\n"])
def test_a_blank_consent_ref_is_refused(ref: str) -> None:
    assert "consent_ref blank" in check_session(session(consent_ref=ref), REGISTER)


def test_a_consent_ref_missing_from_the_register_is_refused() -> None:
    assert "consent_ref not in the register" in check_session(session(consent_ref="c-unknown"), REGISTER)


def test_a_withdrawn_consent_leaves_no_session_behind() -> None:
    register = {**REGISTER, "c-0": ConsentRecord("c-0", withdrawn=True)}
    assert "consent withdrawn: the recording and its copies must be deleted" in check_session(session(), register)


@pytest.mark.parametrize("locale", ["", "EN-NG", "en_NG", "fr", "pidgin", "PCM"])
def test_a_locale_outside_the_five_is_refused_and_case_is_not_folded(locale: str) -> None:
    assert "locale not one of the five" in check_session(session(locale=locale), REGISTER)


def test_two_distinct_annotators_are_required() -> None:
    assert "two distinct annotators are required" in check_session(session(annotator_b="ann-1"), REGISTER)
    assert "two distinct annotators are required" in check_session(session(annotator_a=" "), REGISTER)


@pytest.mark.parametrize("bad", [{}, {"amount": "1"}, {**READING, "extra": "x"}, {**READING, "time": ""}, {**READING, "amount": "  "}])
def test_a_reading_must_hold_exactly_the_three_key_slots_with_text(bad: dict[str, str]) -> None:
    assert "intended slots malformed" in check_session(session(intended=bad), REGISTER)
    assert "annotator A reading malformed" in check_session(session(reading_a=bad), REGISTER)
    assert "annotator B reading malformed" in check_session(session(reading_b=bad), REGISTER)


def test_a_disagreement_without_a_native_speaker_resolution_is_refused() -> None:
    other = {**READING, "amount": "100000"}
    assert "annotators disagree and no resolution is recorded" in check_session(session(reading_b=other), REGISTER)


def test_a_disagreement_settled_by_a_native_speaker_tester_is_accepted() -> None:
    other = {**READING, "amount": "100000"}
    settled = session(reading_b=other, resolution=Resolution("native_speaker_tester", "tester-1", dict(READING)))
    assert check_session(settled, REGISTER) == []


def test_a_resolution_by_anyone_else_or_with_a_bad_reading_is_refused() -> None:
    other = {**READING, "amount": "100000"}
    by_founder = session(reading_b=other, resolution=Resolution("founder", "f-1", dict(READING)))
    assert "resolution must come from a native-speaker tester" in check_session(by_founder, REGISTER)
    blank = session(reading_b=other, resolution=Resolution("native_speaker_tester", " ", dict(READING)))
    assert "resolution resolver_ref blank" in check_session(blank, REGISTER)
    bad = session(reading_b=other, resolution=Resolution("native_speaker_tester", "tester-1", {"amount": "1"}))
    assert "resolution reading malformed" in check_session(bad, REGISTER)


def test_the_three_key_slots_are_amount_counterparty_and_time() -> None:
    assert SLOTS == ("amount", "counterparty", "time")


def good_subset(n: int = MIN_SUBSET_SESSIONS) -> list[AnnotatedSession]:
    return [session(i) for i in range(n)]


def test_fifty_complete_sessions_make_a_ready_subset_and_report_each_locale() -> None:
    report = subset_report(good_subset(), REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert report.ready and report.reasons == () and report.problems == ()
    assert report.n == 50 and dict(report.per_locale) == {locale: 10 for locale in LOCALES}


def test_forty_nine_sessions_are_not_enough() -> None:
    report = subset_report(good_subset(49), REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and "too few sessions" in report.reasons


def test_the_minimum_cannot_be_weakened_below_the_plan() -> None:
    with pytest.raises(ValueError):
        subset_report(good_subset(49), REGISTER, min_sessions=49)
    with pytest.raises(ValueError):
        subset_report(good_subset(), REGISTER, min_sessions=0)


def test_one_session_without_consent_makes_the_whole_subset_not_ready_and_is_named() -> None:
    sessions = good_subset()
    sessions[7] = session(7, consent_ref="")
    report = subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and "sessions with a problem" in report.reasons
    assert report.problems == (("s-7", "consent_ref blank"),)


def test_an_unusable_by_bystander_session_stays_in_the_denominator_and_is_counted() -> None:
    sessions = good_subset()
    sessions[3] = session(3, unusable_by_bystander=True)
    report = subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert report.n == 50 and report.n_unusable == 1 and report.ready


def test_a_duplicate_session_ref_is_a_problem_not_a_second_session() -> None:
    sessions = good_subset()
    sessions[9] = session(9, session_ref="s-8")
    report = subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and ("s-8", "duplicate session_ref") in report.problems


def test_a_consent_shared_by_two_sessions_is_allowed_one_speaker_may_record_many() -> None:
    sessions = good_subset()
    sessions[1] = session(1, consent_ref="c-0")
    assert subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS).ready


def test_every_problem_of_every_session_is_listed_not_only_the_first() -> None:
    sessions = good_subset()
    sessions[2] = session(2, consent_ref="c-unknown", locale="fr")
    report = subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert {code for ref, code in report.problems if ref == "s-2"} == {"consent_ref not in the register", "locale not one of the five"}


def test_an_empty_register_makes_every_session_a_problem() -> None:
    report = subset_report(good_subset(), {}, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and len(report.problems) == 50


# ---- EL review of noema#451 at e2d1782: the floor cannot be bypassed, one bad record never crashes the report, nothing fails open -----------------------


@pytest.mark.parametrize("bad", [float("nan"), 49.5, 50.0, 100.0, None, True, False, "50", float("inf")])
def test_min_sessions_must_be_a_real_int_at_or_above_the_floor(bad: object) -> None:
    """P1: `nan < 50` is False, so a nan used to bypass the floor and make three sessions ready."""
    with pytest.raises((ValueError, TypeError)):
        subset_report(good_subset(3), REGISTER, min_sessions=bad)  # type: ignore[arg-type]


def test_the_minimum_given_by_the_caller_is_the_one_applied() -> None:
    assert not subset_report(good_subset(60), REGISTER, min_sessions=100).ready
    assert "too few sessions" in subset_report(good_subset(60), REGISTER, min_sessions=100).reasons
    assert subset_report(good_subset(60), REGISTER, min_sessions=60).ready


def test_an_empty_list_of_sessions_is_not_ready() -> None:
    report = subset_report([], REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and report.n == 0 and "too few sessions" in report.reasons


@pytest.mark.parametrize("locale", [["yo"], {"a": 1}, None, 5, b"yo"])
def test_a_value_of_the_wrong_type_is_a_problem_not_a_crash(locale: object) -> None:
    """P2: an unhashable locale (`["yo"]`) raised TypeError in the membership test and in the Counter and lost the whole report."""
    sessions = good_subset()
    sessions[4] = session(4, locale=locale)
    report = subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and ("s-4", "locale not one of the five") in report.problems
    assert report.n == 50 and report.n_other_locale == 1  # counted, never dropped


@pytest.mark.parametrize("field_name", ["session_ref", "consent_ref"])
@pytest.mark.parametrize("value", [["x"], None, 7])
def test_a_reference_of_the_wrong_type_is_a_problem_not_a_crash(field_name: str, value: object) -> None:
    sessions = good_subset()
    sessions[2] = session(2, **{field_name: value})
    report = subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and any(ref_problem == f"{field_name} blank" for _, ref_problem in report.problems)


@pytest.mark.parametrize("resolution", ["a native speaker", 7, ["native_speaker_tester"], object()])
def test_a_resolution_that_is_not_a_resolution_is_a_problem_not_a_crash(resolution: object) -> None:
    other = {**READING, "amount": "100000"}
    assert "resolution malformed" in check_session(session(reading_b=other, resolution=resolution), REGISTER)


@pytest.mark.parametrize("record", [None, "c-0", 3, object()])
def test_a_register_entry_that_is_not_a_consent_record_is_a_problem_not_a_crash(record: object) -> None:
    register = {**REGISTER, "c-0": record}
    assert "consent record malformed" in check_session(session(), register)  # type: ignore[arg-type]


def test_a_register_entry_filed_under_another_consent_ref_is_refused() -> None:
    register = {**REGISTER, "c-0": ConsentRecord("c-other")}
    assert "consent record malformed" in check_session(session(), register)


@pytest.mark.parametrize("withdrawn", [None, 0, 1, "no", "", [], "False"])
def test_a_withdrawn_flag_that_is_not_a_bool_fails_closed(withdrawn: object) -> None:
    """P2: `withdrawn=None` or `0` used to count as not withdrawn, which fails open."""
    register = {**REGISTER, "c-0": ConsentRecord("c-0", withdrawn=withdrawn)}  # type: ignore[arg-type]
    assert "consent record malformed" in check_session(session(), register)


@pytest.mark.parametrize("flag", [None, 0, 1, "no", "yes", []])
def test_an_unusable_flag_that_is_not_a_bool_is_a_problem(flag: object) -> None:
    assert "unusable_by_bystander must be true or false" in check_session(session(unusable_by_bystander=flag), REGISTER)  # type: ignore[arg-type]


@pytest.mark.parametrize("other", ["ann-1 ", " ann-1", "ANN-1", "Ann-1", "ann-1\t"])
def test_the_same_person_written_another_way_is_not_a_second_annotator(other: str) -> None:
    assert "two distinct annotators are required" in check_session(session(annotator_a="ann-1", annotator_b=other), REGISTER)


def test_annotator_b_blank_is_refused_on_its_own() -> None:
    assert "two distinct annotators are required" in check_session(session(annotator_b=""), REGISTER)
    assert "two distinct annotators are required" in check_session(session(annotator_b=None), REGISTER)  # type: ignore[arg-type]


@pytest.mark.parametrize("slot", ["amount", "counterparty", "time"])
def test_a_disagreement_on_any_one_slot_needs_a_resolution(slot: str) -> None:
    other = {**READING, slot: READING[slot] + " changed"}
    assert "annotators disagree and no resolution is recorded" in check_session(session(reading_b=other), REGISTER)
    settled = session(reading_b=other, resolution=Resolution("native_speaker_tester", "tester-1", dict(READING)))
    assert check_session(settled, REGISTER) == []


@pytest.mark.parametrize("value", [10000, None, ["10000"], b"10000"])
def test_a_slot_value_that_is_not_text_is_refused(value: object) -> None:
    assert "annotator A reading malformed" in check_session(session(reading_a={**READING, "amount": value}), REGISTER)  # type: ignore[dict-item]


def test_every_bad_session_is_listed_not_only_the_first() -> None:
    sessions = good_subset()
    sessions[3] = session(3, consent_ref="")
    sessions[9] = session(9, locale="fr")
    refs = {ref for ref, _ in subset_report(sessions, REGISTER, min_sessions=MIN_SUBSET_SESSIONS).problems}
    assert refs == {"s-3", "s-9"}


def test_unusable_sessions_count_toward_the_minimum_as_the_plan_says_but_not_all_of_them() -> None:
    """Plan section 3: a session lost to a bystander is a failed session in the denominator, never dropped. So it counts in `n`, and `n_usable` says how many can be analysed."""
    some = good_subset()
    some[3] = session(3, unusable_by_bystander=True)
    report = subset_report(some, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert report.ready and report.n == 50 and report.n_unusable == 1 and report.n_usable == 49
    every = [session(i, unusable_by_bystander=True) for i in range(50)]
    report = subset_report(every, REGISTER, min_sessions=MIN_SUBSET_SESSIONS)
    assert not report.ready and report.n_usable == 0 and "no usable session" in report.reasons


def test_the_repr_of_a_record_never_prints_the_annotation() -> None:
    """AGENT-13: a traceback or an assertion diff must not print what a speaker said."""
    s = session(0, resolution=Resolution("native_speaker_tester", "tester-1", {"amount": "SECRETAMOUNT", "counterparty": "SECRETNAME", "time": "t"}))
    text = repr(s) + repr(s.resolution)
    for word in ("Ada Test", "10000", "yesterday", "SECRETAMOUNT", "SECRETNAME"):
        assert word not in text, word
