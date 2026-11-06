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
