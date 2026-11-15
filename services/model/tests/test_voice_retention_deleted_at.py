"""A malformed deletion marker never makes audio look deleted (VOI-05, REQ-VOI-004, AGENT-13; Codex P1 on #485 at 0307b23).

`deleted_at` suppressed both the withdrawn-consent violation and the retention timer solely because it was not None, so a value such as "" or False
made unconsented, expired audio disappear from both reports. A non-null marker now counts as deletion only when it is a timezone-aware datetime. The scan
reports a malformed one as a violation and treats the object as not deleted; the retention timer refuses it like every other bad timestamp. All data is
SYNTHETIC (AGENT-25).
"""

from datetime import UTC, datetime, timedelta

import pytest

from noema_model.voice.retention import StudyObject, deletions_due, scan_objects
from noema_model.voice.study import ConsentRecord

NOW = datetime(2026, 12, 1, 12, 0, tzinfo=UTC)
WITHDRAWN = {"c-0": ConsentRecord("c-0", withdrawn=True)}
LIVE = {"c-0": ConsentRecord("c-0")}
MALFORMED = pytest.mark.parametrize("marker", ["", "  ", False, True, 0, 1, "2026-11-01T00:00:00Z", datetime(2026, 11, 1), object()],
                                    ids=["empty", "blank", "false", "true", "zero", "one", "string", "naive", "object"])


def obj(**over: object) -> StudyObject:
    fields: dict[str, object] = dict(object_key="study/o-0", consent_ref="c-0", analysed_at=None, withdrawn_at=None, deleted_at=None)
    fields.update(over)
    return StudyObject(**fields)  # type: ignore[arg-type]


@MALFORMED
def test_a_malformed_deletion_marker_does_not_hide_a_withdrawn_consent(marker: object) -> None:
    report = scan_objects([obj(deleted_at=marker)], WITHDRAWN)
    assert not report.ok
    reasons = [reason for _, reason in report.violations]
    assert "consent withdrawn: the object must be deleted" in reasons  # still treated as not deleted
    assert any("deleted_at" in reason and "malformed" in reason for reason in reasons)  # and the bad marker is itself reported


@MALFORMED
def test_a_malformed_deletion_marker_is_a_violation_even_when_nothing_else_is_wrong(marker: object) -> None:
    report = scan_objects([obj(deleted_at=marker)], LIVE)
    assert not report.ok
    assert [r for _, r in report.violations] == ["deleted_at malformed: treat the object as not deleted"]


def test_a_timezone_aware_deletion_marker_still_clears_a_withdrawn_consent() -> None:
    report = scan_objects([obj(deleted_at=NOW - timedelta(days=1))], WITHDRAWN)
    assert report.ok and report.violations == ()


@MALFORMED
def test_the_retention_timer_refuses_a_malformed_marker_instead_of_skipping_the_object(marker: object) -> None:
    expired = obj(analysed_at=NOW - timedelta(days=400), deleted_at=marker)
    with pytest.raises(ValueError, match="deleted_at"):
        deletions_due([expired], NOW, retention_after_analysis=timedelta(days=90), withdrawal_deadline=timedelta(days=7))


def test_a_valid_deletion_marker_still_removes_the_object_from_the_timer() -> None:
    done = obj(analysed_at=NOW - timedelta(days=400), deleted_at=NOW - timedelta(days=300))
    assert deletions_due([done], NOW, retention_after_analysis=timedelta(days=90), withdrawal_deadline=timedelta(days=7)) == ()
