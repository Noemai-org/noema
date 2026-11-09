"""The service reads no benchmark variable (Architect's ruling A55 of 8 Nov 2026 on noema#293, SR's condition): its telemetry is its own, and the bench stack hands in
cost-file telemetry from outside, from benchmark/model_cost. REQ-NFR-007, REQ-TST-006, INV-22."""

import importlib
import json
import sys
from pathlib import Path

import pytest

from noema_model.harness import Harness
from noema_model.main import build_telemetry, create_app
from noema_model.platform.telemetry import COST_EVENT, InMemoryTelemetry
from tests.support import ManualClock, ScriptedProvider, make_config, run
from tests.test_harness_call import good, request

REPO = Path(__file__).resolve().parents[3]


def bench_module(name: str):  # type: ignore[no-untyped-def]
    """benchmark/model_cost as the bench stack mounts it: a package named model_cost on the path."""
    path = str(REPO / "benchmark")
    if path not in sys.path:
        sys.path.insert(0, path)
    return importlib.import_module(f"model_cost.{name}")


def one_call(telemetry) -> object:  # type: ignore[no-untyped-def]
    harness = Harness(make_config(providers=("alpha",)), {"alpha": ScriptedProvider("alpha", [good("alpha")])}, ManualClock(), telemetry)
    return run(harness.generate(request())).call


def test_the_service_telemetry_ignores_bench_out_and_writes_no_file(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("BENCH_OUT", str(tmp_path))
    for telemetry in (build_telemetry(), create_app().state.telemetry, create_app(environ={"BENCH_OUT": str(tmp_path)}).state.telemetry):
        one_call(telemetry)
    assert not list(tmp_path.iterdir())


def test_a_given_telemetry_is_used_as_is() -> None:
    given = InMemoryTelemetry()
    assert create_app(telemetry=given).state.telemetry is given


def test_the_runtime_package_holds_no_cost_file_writer() -> None:
    import noema_model.platform.telemetry as runtime

    assert not any(name for name in dir(runtime) if "CostFile" in name or "cost_files" in name)


def test_the_bench_entry_point_hands_the_service_a_cost_file_writer(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("BENCH_OUT", str(tmp_path))
    sys.modules.pop("model_cost.model_app", None)
    entry = bench_module("model_app")
    call = one_call(entry.app.state.telemetry)
    (written,) = (tmp_path / "cost").glob("*.json")
    assert json.loads(written.read_text()) == call.to_dict()  # type: ignore[attr-defined]


def test_the_bench_entry_point_will_not_start_without_bench_out(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("BENCH_OUT", raising=False)
    sys.modules.pop("model_cost.model_app", None)
    with pytest.raises(KeyError):
        bench_module("model_app")


def test_the_writer_and_the_service_agree_on_the_cost_event_name() -> None:
    assert bench_module("cost_files").COST_EVENT == COST_EVENT


def test_the_bench_compose_override_mounts_benchmark_output_and_the_writer_code_only() -> None:
    text = (REPO / "deploy" / "compose" / "bench-model.yml").read_text()
    live = [line for line in text.splitlines() if line.strip() and not line.lstrip().startswith("#")]
    volumes = [line.strip() for line in live if line.lstrip().startswith("- ") and ":" in line]
    assert volumes == ["- ${NOEMA_BENCH_OUT:?set by make bench-model-up}:/bench-out", "- ../../benchmark/model_cost:/bench/model_cost:ro"]
    assert not any("held" in line.lower() for line in live)
    assert any("model_cost.model_app:app" in line for line in live)


# --- the harness on the app (REQ-INT-002) ---------------------------------------------------------------------------------

def test_readyz_follows_the_harness_configuration(tmp_path: Path) -> None:
    from fastapi.testclient import TestClient
    from tests.support import raw_config

    from noema_model.harness.assemble import ENV_CONFIG, ENV_LLM_POOL

    bare = create_app(environ={})
    assert bare.state.harness is None
    answer = TestClient(bare).get("/readyz")
    assert answer.status_code == 503 and ENV_CONFIG in answer.json()["reason"]
    assert TestClient(bare).get("/healthz").status_code == 200  # alive is not ready

    path = tmp_path / "harness.json"
    path.write_text(json.dumps(raw_config(providers=("alpha",))))
    ready = create_app(environ={ENV_CONFIG: str(path), ENV_LLM_POOL: "2"})
    assert isinstance(ready.state.harness, Harness)
    assert TestClient(ready).get("/readyz").status_code == 200

    path.write_text("{}")
    broken = create_app(environ={ENV_CONFIG: str(path)})
    assert broken.state.harness is None and TestClient(broken).get("/readyz").status_code == 503


def test_an_explicit_readiness_check_still_wins() -> None:
    from fastapi.testclient import TestClient

    assert TestClient(create_app(lambda: (True, ""), environ={})).get("/readyz").status_code == 200
