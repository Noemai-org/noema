"""UND-13, REQ-INT-006 (DEC-2026-10-31 D25, AGENT-28): the model service refuses a model ID that has no QUALIFIED record, or an unpinned version.

The lookup rules are the benchmark module's (benchmark/qualification/README.md, `record.check_model`). `fixtures/qualification/gate_golden.json` holds SYNTHETIC records made by
that module's own `build_record` and the answers its own `check_model` gave; this service re-implements the rules and must agree with every one. The held-out set is
not involved (INV-22): records carry digests and verdicts only.
"""

import json
from pathlib import Path
from typing import Any

import pytest

from noema_model.qualification.gate import (
    ModelNotQualified,
    QualificationUnreadable,
    check_model,
    load_records,
    require_qualified,
)
from tests.support import FIXTURES

GOLDEN: dict[str, Any] = json.loads((FIXTURES / "qualification" / "gate_golden.json").read_text())
LOOKUPS = GOLDEN["lookups"]


@pytest.mark.parametrize("case", LOOKUPS, ids=[c["name"] for c in LOOKUPS])
def test_the_lookup_agrees_with_the_benchmark_modules_answer(case: dict[str, Any]) -> None:
    records = [GOLDEN["records"][i] for i in case["records"]]
    assert check_model(records, case["provider"], case["model_id"], case["version"], case["kind"]) == case["expected"]


def test_an_unpinned_version_is_refused_before_any_lookup_even_with_a_matching_record() -> None:
    q = GOLDEN["records"]["q"]
    alias = {**q, "version": "latest"}
    assert check_model([alias], q["provider"], q["model_id"], "latest", "llm")


def test_records_are_read_from_a_directory_and_an_empty_directory_qualifies_nothing(tmp_path: Path) -> None:
    assert load_records(tmp_path) == []
    q = GOLDEN["records"]["q"]
    assert check_model(load_records(tmp_path), q["provider"], q["model_id"], q["version"], "llm")
    (tmp_path / "r.json").write_text(json.dumps(q))
    assert check_model(load_records(tmp_path), q["provider"], q["model_id"], q["version"], "llm") == []


def test_an_unreadable_or_non_object_record_file_fails_closed_instead_of_being_skipped(tmp_path: Path) -> None:
    (tmp_path / "ok.json").write_text(json.dumps(GOLDEN["records"]["q"]))
    (tmp_path / "bad.json").write_text("{not json")
    with pytest.raises(QualificationUnreadable):
        load_records(tmp_path)
    (tmp_path / "bad.json").write_text("[]")
    with pytest.raises(QualificationUnreadable):
        load_records(tmp_path)


def test_a_missing_records_directory_is_unreadable_not_empty(tmp_path: Path) -> None:
    with pytest.raises(QualificationUnreadable):
        load_records(tmp_path / "absent")


def test_require_qualified_names_every_refused_provider_not_only_the_first(tmp_path: Path) -> None:
    q = GOLDEN["records"]["q"]
    (tmp_path / "q.json").write_text(json.dumps(q))
    wanted = [
        ("llm", q["provider"], q["model_id"], q["version"]),
        ("llm", q["provider"], q["model_id"], "latest"),
        ("llm", "gemini", "decision-model-a", "2026-11-01"),
    ]
    with pytest.raises(ModelNotQualified) as raised:
        require_qualified(wanted, tmp_path)
    assert len(raised.value.refusals) == 2
    assert "latest" in raised.value.refusals[0] and "gemini" in raised.value.refusals[1]
    require_qualified(wanted[:1], tmp_path)


def test_the_records_are_never_the_oracle_and_hold_no_case_content() -> None:
    for record in GOLDEN["records"].values():
        assert "expected" not in json.dumps(record) and "oracle" not in json.dumps(record)


@pytest.mark.parametrize("field", ["complete", "tier0_all_zero"])
@pytest.mark.parametrize("value", ["false", "False", 0, 1, None, "true"])
def test_a_flag_that_is_not_a_real_boolean_invalidates_the_record_instead_of_being_coerced(field: str, value: Any) -> None:
    # Codex review of noema#215: bool("false") is True, so a string flag must never let a failed run through.
    q = {**GOLDEN["records"]["q"], field: value}
    assert check_model([q], q["provider"], q["model_id"], q["version"], "llm")


@pytest.mark.parametrize("value", ["false", "True", 0, 1, None])
def test_pass_k_available_must_be_a_real_boolean(value: Any) -> None:
    q = GOLDEN["records"]["q"]
    broken = {**q, "pass_k": {**q["pass_k"], "available": value}}
    assert check_model([broken], q["provider"], q["model_id"], q["version"], "llm")


@pytest.mark.parametrize("day", ["2026W455", "2026-W45-5", "20261106", "2026-11-6", "2026-11-06T00:00:00", " 2026-11-06"])
def test_a_date_that_is_not_the_canonical_spelling_invalidates_the_record(day: str) -> None:
    # Codex review of noema#215: date.fromisoformat accepts several spellings of one day, and the lookup compared the strings.
    q = {**GOLDEN["records"]["q"], "qualified_on": day}
    assert check_model([q], q["provider"], q["model_id"], q["version"], "llm")


def test_records_of_one_day_compare_as_dates_so_a_same_day_conflict_is_refused() -> None:
    q, nq = GOLDEN["records"]["q"], GOLDEN["records"]["nq"]
    same_day = [{**q, "qualified_on": "2026-11-06"}, {**nq, "qualified_on": "2026-11-06"}]
    assert check_model(same_day, q["provider"], q["model_id"], q["version"], "llm")


@pytest.mark.parametrize("version", ["stable-2", "model-default-1", "gpt-preview-3", "2026-11-01-newest", "current-5", "auto-7", "alpha", "x" * 200, "has space 1", "-2026"])
def test_a_floating_alias_or_unpinned_version_is_refused_as_the_harness_refuses_it(version: str) -> None:
    # Codex review of noema#215: the gate must use the harness's own pin rule (harness.config.check_pinned).
    q = {**GOLDEN["records"]["q"], "version": version}
    assert check_model([q], q["provider"], q["model_id"], version, "llm")


@pytest.mark.parametrize("version", ["2026-11-01", "2026-10-01", "gpt-4o-2024-08-06", "claude-sonnet-4-20250514", "1.5.2"])
def test_a_dated_or_numbered_snapshot_is_accepted_when_a_record_exists(version: str) -> None:
    q = {**GOLDEN["records"]["q"], "version": version}
    assert check_model([q], q["provider"], q["model_id"], version, "llm") == []
