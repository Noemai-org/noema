"""The service builds its harness from a mounted configuration file (REQ-INT-002, REQ-NFR-007, INV-29).

Faults are real files: a missing, unreadable, invalid or credential-less configuration leaves the service not ready, never
half-built. The provider wire is the recorded transport of the contract tests (not run: no access to a live provider)."""

import json
from pathlib import Path
from typing import Any

import pytest

from noema_model.harness import Harness, assemble_harness
from noema_model.harness.assemble import ENV_CONFIG
from noema_model.platform.telemetry import InMemoryTelemetry, cost_files_from_environment
from noema_model.providers.llm.base import HttpResponse
from tests.support import ManualClock, RecordingTransport, raw_config, run
from tests.test_harness_call import request, version


def write(tmp_path: Path, body: Any) -> dict[str, str]:
    path = tmp_path / "harness.json"
    path.write_text(body if isinstance(body, str) else json.dumps(body))
    return {ENV_CONFIG: str(path)}


def chat_reply(model: str) -> HttpResponse:
    body = {
        "model": model,
        "choices": [{"index": 0, "message": {"role": "assistant", "content": json.dumps({"surface": "20k", "confidence": 0.9}),
                                              "refusal": None}, "finish_reason": "stop"}],
        "usage": {"prompt_tokens": 100, "completion_tokens": 20},
    }
    return HttpResponse(200, json.dumps(body).encode())


def test_without_a_configuration_path_there_is_no_harness_and_the_reason_says_so() -> None:
    result = assemble_harness({}, ManualClock(), InMemoryTelemetry())
    assert result.harness is None and ENV_CONFIG in result.reason


@pytest.mark.parametrize("body", ["not json", "[]", "{}", json.dumps({"providers": {}})])
def test_an_invalid_configuration_leaves_no_harness(tmp_path: Path, body: str) -> None:
    telemetry = InMemoryTelemetry()
    result = assemble_harness(write(tmp_path, body), ManualClock(), telemetry)
    assert result.harness is None and result.reason
    assert [e[0] for e in telemetry.events] == ["HARNESS_CONFIG_REJECTED"]


def test_a_missing_file_leaves_no_harness(tmp_path: Path) -> None:
    result = assemble_harness({ENV_CONFIG: str(tmp_path / "absent.json")}, ManualClock(), InMemoryTelemetry())
    assert result.harness is None and "unreadable" in result.reason


def test_a_provider_without_a_credential_never_starts(tmp_path: Path) -> None:
    raw = raw_config(providers=("alpha",))
    empty = tmp_path / "empty.txt"
    empty.write_text("")
    raw["providers"]["alpha"]["credential_file"] = str(empty)
    result = assemble_harness(write(tmp_path, raw), ManualClock(), InMemoryTelemetry())
    assert result.harness is None and "credential_file" in result.reason


def test_the_reason_never_carries_a_credential(tmp_path: Path) -> None:
    secret = tmp_path / "cred.txt"
    secret.write_text("sk-synthetic-not-a-real-key")
    raw = raw_config(providers=("alpha",))
    raw["providers"]["alpha"]["credential_file"] = str(secret)
    raw["providers"]["alpha"]["timeout_ms"] = "slow"  # invalid on purpose
    result = assemble_harness(write(tmp_path, raw), ManualClock(), InMemoryTelemetry())
    assert result.harness is None and "sk-synthetic" not in result.reason


def test_a_valid_configuration_builds_a_harness_that_answers_and_writes_its_cost_file(tmp_path: Path) -> None:
    out = tmp_path / "bench"
    telemetry = cost_files_from_environment(InMemoryTelemetry(), {"BENCH_OUT": str(out)})
    transport = RecordingTransport(chat_reply(version("alpha")))
    result = assemble_harness(write(tmp_path, raw_config(providers=("alpha",))), ManualClock(), telemetry, transport)
    assert isinstance(result.harness, Harness) and result.reason == ""
    answer = run(result.harness.generate(request()))
    assert answer.value.surface == "20k" and transport.requests
    (written,) = (out / "cost").glob("*.json")
    assert json.loads(written.read_text()) == answer.call.to_dict()
