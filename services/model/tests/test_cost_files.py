"""Bench-only cost files: each model call's CallRecord.to_dict(), written to $BENCH_OUT/cost/<call_id>.json (REQ-NFR-007).

Faults come from the real harness and the real files, not from mocks (AGENT-12)."""

import json
from pathlib import Path

import pytest

from noema_model.platform.telemetry import (
    COST_EVENT, CostFileTelemetry, InMemoryTelemetry, NullTelemetry, cost_files_from_environment,
)
from tests.test_harness_call import Out, TEMPLATE, good, request, version  # noqa: F401
from noema_model.harness import Harness
from tests.support import ManualClock, ScriptedProvider, make_config, run
from typing import Any


def harness_with(telemetry: Any) -> Harness:
    return Harness(make_config(providers=("alpha",)), {"alpha": ScriptedProvider("alpha", [good("alpha"), good("alpha")])},
                   ManualClock(), telemetry)


def test_without_bench_out_the_telemetry_is_returned_unchanged(tmp_path: Path) -> None:
    inner = InMemoryTelemetry()
    assert cost_files_from_environment(inner, {}) is inner
    assert cost_files_from_environment(inner, {"BENCH_OUT": ""}) is inner
    assert not list(tmp_path.iterdir())


def test_with_bench_out_a_call_writes_exactly_its_call_record(tmp_path: Path) -> None:
    inner = InMemoryTelemetry()
    telemetry = cost_files_from_environment(inner, {"BENCH_OUT": str(tmp_path)})
    call = run(harness_with(telemetry).generate(request())).call
    files = sorted((tmp_path / "cost").glob("*.json"))
    assert len(files) == 1
    assert json.loads(files[0].read_text()) == call.to_dict()
    assert files[0].name.startswith(request().correlation_id)
    # the other telemetry still sees its events and metrics
    assert inner.total("noema_provider_call_total") >= 1


def test_each_call_gets_its_own_file_and_none_is_overwritten(tmp_path: Path) -> None:
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path)
    harness = harness_with(telemetry)
    run(harness.generate(request()))
    run(harness.generate(request()))
    # a second process with the same correlation id must not overwrite the first one's files
    run(harness_with(CostFileTelemetry(NullTelemetry(), tmp_path)).generate(request()))
    assert len(list((tmp_path / "cost").glob("*.json"))) == 3


def test_a_correlation_id_cannot_choose_the_path(tmp_path: Path) -> None:
    telemetry = CostFileTelemetry(NullTelemetry(), tmp_path / "out")
    telemetry.event(COST_EVENT, "../../escape", {"record": "{}"})
    assert not (tmp_path / "escape").exists()
    written = list((tmp_path / "out" / "cost").iterdir())
    assert len(written) == 1 and written[0].parent == tmp_path / "out" / "cost"


def test_other_events_write_nothing(tmp_path: Path) -> None:
    CostFileTelemetry(NullTelemetry(), tmp_path).event("PROVIDER_FAILOVER", "corr_1", {"route": "x"})
    assert not (tmp_path / "cost").exists()


def test_a_write_failure_is_loud_not_silent(tmp_path: Path) -> None:
    blocked = tmp_path / "file"
    blocked.write_text("x")
    with pytest.raises(OSError):
        CostFileTelemetry(NullTelemetry(), blocked).event(COST_EVENT, "corr_1", {"record": "{}"})
