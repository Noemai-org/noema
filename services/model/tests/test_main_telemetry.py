"""The entry point builds the service's telemetry from its environment; bench runs get cost files (REQ-NFR-007, REQ-OPS-001)."""

import json

import pytest
from pathlib import Path

from noema_model.main import build_telemetry, create_app
from noema_model.platform.telemetry import CostFileTelemetry, InMemoryTelemetry
from tests.support import ManualClock, ScriptedProvider, make_config, run
from tests.test_harness_call import good, request
from noema_model.harness import Harness


def test_without_bench_out_the_service_writes_no_cost_file(tmp_path: Path) -> None:
    assert not isinstance(build_telemetry({}), CostFileTelemetry)
    assert not isinstance(create_app().state.telemetry, CostFileTelemetry)


def test_with_bench_out_the_service_telemetry_writes_cost_files(tmp_path: Path) -> None:
    telemetry = build_telemetry({"BENCH_OUT": str(tmp_path)})
    assert isinstance(telemetry, CostFileTelemetry)
    app = create_app(telemetry=telemetry)
    assert app.state.telemetry is telemetry
    harness = Harness(make_config(providers=("alpha",)), {"alpha": ScriptedProvider("alpha", [good("alpha")])},
                      ManualClock(), app.state.telemetry)
    call = run(harness.generate(request())).call
    (written,) = (tmp_path / "cost").glob("*.json")
    assert json.loads(written.read_text()) == call.to_dict()


def test_a_given_telemetry_is_used_as_is() -> None:
    given = InMemoryTelemetry()
    assert create_app(telemetry=given).state.telemetry is given


def test_the_default_app_reads_bench_out_from_the_process_environment(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("BENCH_OUT", str(tmp_path))
    assert isinstance(create_app().state.telemetry, CostFileTelemetry)
    monkeypatch.delenv("BENCH_OUT")
    assert not isinstance(create_app().state.telemetry, CostFileTelemetry)
