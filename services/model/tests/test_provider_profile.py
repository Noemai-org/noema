"""UND-15 (A635, A619): the profile switch and the demo-profile refusal of the recorded-replay transport.

REQ-INT-002, REQ-TST-004; INV-29, AGENT-12, AGENT-16, AGENT-25. The row's Done-when: 'a test that the service refuses to start with this provider in the demo
profile; a request with no recording fails closed'. The replay transport is available in the bench and test profiles only; every other profile, demo first
among them, refuses it; an unknown profile refuses the start; nothing defaults to replay; a replay that cannot be built never falls back to a live transport.
All fixtures are SYNTHETIC; no credential, URL or reply text is read into a refusal.
"""

import ast
import json
import socket
from pathlib import Path
from typing import Any

import pytest

from noema_model.harness import assemble_harness
from noema_model.harness.assemble import ENV_CONFIG, ENV_LLM_POOL, ENV_MASKING_LIMITS
from noema_model.main import create_app
from noema_model.platform.config import ConfigError
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm import profile as profile_module
from noema_model.providers.llm.base import ProviderError
from noema_model.providers.llm.profile import (
    ENV_PROFILE,
    ENV_REPLAY_FIXTURES,
    REPLAY_EVIDENCE_LABEL,
    ProfileRefused,
    select_transport,
)
from noema_model.providers.llm.replay import ReplayMiss, ReplayTransport
from noema_model.providers.llm.transport import UrllibTransport
from tests.support import ManualClock, raw_config, run
from tests.test_replay_transport import key_of, write_dir

URL = "https://provider.invalid/v1/x"
SECRET_DIR_NAME = "fixtures-dir-name-that-must-not-appear"


def fixtures(tmp_path: Path) -> Path:
    return write_dir(tmp_path / SECRET_DIR_NAME, [b'{"request": 1}'])


def live_factory(built: list[str]) -> Any:
    def factory() -> UrllibTransport:
        built.append("live")
        return UrllibTransport(pool_size=1)

    return factory


# ---- the closed set of profiles ------------------------------------------------------------------------------------------------------------------------------------------------


@pytest.mark.parametrize("value", ["prod", "production", "live", "", " bench", "bench ", "BENCH", "Demo", "bench,test", "../bench", "bench\n"])
def test_an_unknown_profile_refuses_the_start_whether_or_not_a_replay_is_asked_for(tmp_path: Path, value: str) -> None:
    for env in ({ENV_PROFILE: value}, {ENV_PROFILE: value, ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))}):
        built: list[str] = []
        with pytest.raises(ProfileRefused) as exc:
            select_transport(env, live_factory(built))
        assert exc.value.code == "UNKNOWN_PROFILE" and built == []


@pytest.mark.parametrize("name", ["dev", "ci", "bench", "demo", "test"])
def test_every_named_profile_without_a_replay_setting_is_the_live_transport_as_before(name: str) -> None:
    built: list[str] = []
    chosen = select_transport({ENV_PROFILE: name}, live_factory(built))
    assert isinstance(chosen.transport, UrllibTransport) and chosen.replay is False and chosen.label is None and built == ["live"]


def test_no_profile_and_no_replay_setting_is_the_live_transport_as_before_nothing_defaults_to_replay() -> None:
    built: list[str] = []
    chosen = select_transport({}, live_factory(built))
    assert isinstance(chosen.transport, UrllibTransport) and chosen.replay is False and built == ["live"]


# ---- replay: bench and test only -----------------------------------------------------------------------------------------------------------------------------------------------


@pytest.mark.parametrize("name", ["bench", "test"])
def test_replay_is_selected_by_configuration_in_the_bench_and_test_profiles_and_never_builds_a_live_transport(tmp_path: Path, name: str) -> None:
    built: list[str] = []
    chosen = select_transport({ENV_PROFILE: name, ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))}, live_factory(built))
    assert isinstance(chosen.transport, ReplayTransport) and chosen.replay is True and built == []
    assert chosen.label == REPLAY_EVIDENCE_LABEL == "Understanding: recorded replay of live output"


def test_the_demo_profile_refuses_the_replay_transport(tmp_path: Path) -> None:
    built: list[str] = []
    with pytest.raises(ProfileRefused) as exc:
        select_transport({ENV_PROFILE: "demo", ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))}, live_factory(built))
    assert exc.value.code == "DEMO_REFUSES_REPLAY" and built == []


@pytest.mark.parametrize("name", ["dev", "ci"])
def test_every_other_profile_refuses_the_replay_transport_too(tmp_path: Path, name: str) -> None:
    with pytest.raises(ProfileRefused) as exc:
        select_transport({ENV_PROFILE: name, ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))}, live_factory([]))
    assert exc.value.code == "REPLAY_ONLY_IN_BENCH_AND_TEST"


def test_a_replay_setting_with_no_profile_is_refused_nothing_infers_a_profile(tmp_path: Path) -> None:
    with pytest.raises(ProfileRefused) as exc:
        select_transport({ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))}, live_factory([]))
    assert exc.value.code == "PROFILE_REQUIRED_FOR_REPLAY"


@pytest.mark.parametrize("name", ["bench", "test"])
def test_fixtures_that_cannot_be_read_refuse_the_start_and_never_fall_back_to_a_live_transport(tmp_path: Path, name: str) -> None:
    built: list[str] = []
    for value in (str(tmp_path / "absent"), "", str(tmp_path)):  # a missing path, an empty setting, a directory that is not the recorder's output
        with pytest.raises(ProfileRefused) as exc:
            select_transport({ENV_PROFILE: name, ENV_REPLAY_FIXTURES: value}, live_factory(built))
        assert exc.value.code == "REPLAY_FIXTURES_REFUSED"
    assert built == []


def test_a_refusal_is_a_config_error_and_its_text_names_no_value(tmp_path: Path) -> None:
    root = fixtures(tmp_path)
    cases = [
        {ENV_PROFILE: "demo", ENV_REPLAY_FIXTURES: str(root)},
        {ENV_PROFILE: "prod-secret-profile-value"},
        {ENV_REPLAY_FIXTURES: str(root)},
        {ENV_PROFILE: "bench", ENV_REPLAY_FIXTURES: str(tmp_path / "gone")},
    ]
    for env in cases:
        with pytest.raises(ProfileRefused) as exc:
            select_transport(env, live_factory([]))
        text = str(exc.value)
        assert isinstance(exc.value, ConfigError) and exc.value.code in text
        for value in env.values():
            assert value not in text
        assert SECRET_DIR_NAME not in text and "prod-secret" not in text


def test_a_request_with_no_recording_fails_closed_through_the_selected_transport_and_opens_no_connection(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    def no_network(*_a: object, **_k: object) -> None:
        raise AssertionError("a socket was opened")

    monkeypatch.setattr(socket.socket, "connect", no_network)  # as test_replay_transport does: the event loop's own socket pair stays usable
    monkeypatch.setattr(socket, "create_connection", no_network)
    chosen = select_transport({ENV_PROFILE: "test", ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))}, live_factory([]))
    with pytest.raises(ReplayMiss) as miss:
        run(chosen.transport.post(URL, {}, b'{"request": "not recorded"}', 5.0, 1 << 20))
    assert isinstance(miss.value, ProviderError) and miss.value.code == "UNKNOWN_REQUEST"
    answered = run(chosen.transport.post(URL, {}, b'{"request": 1}', 5.0, 1 << 20))
    assert answered.status == 200 and key_of(b'{"request": 1}').startswith("sha256:")


# ---- the service: it refuses to start with the replay in the demo profile -------------------------------------------------------------------------------------------------------


def harness_env(tmp_path: Path, extra: dict[str, str]) -> dict[str, str]:
    credential = tmp_path / "credential.txt"
    credential.write_text("synthetic-credential-value-1")
    raw = raw_config(providers=("alpha",))
    raw["providers"]["alpha"]["credential_file"] = str(credential)
    config = tmp_path / "harness.json"
    config.write_text(json.dumps(raw))
    limits = tmp_path / "masking_limits.json"
    limits.write_text(json.dumps({"schema_version": "1.0", "pack_id": "fintech", "label": "SYNTHETIC", "max_unmasked_digit_run": 4}))
    return {ENV_CONFIG: str(config), ENV_MASKING_LIMITS: str(limits), ENV_LLM_POOL: "2", **extra}


def test_the_service_assembles_a_harness_over_the_replay_transport_in_the_bench_profile(tmp_path: Path) -> None:
    env = harness_env(tmp_path, {ENV_PROFILE: "bench", ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))})
    telemetry = InMemoryTelemetry()
    result = assemble_harness(env, ManualClock(), telemetry)
    assert result.harness is not None and result.reason == ""
    selected = [e for e in telemetry.events if e[0] == "TRANSPORT_PROFILE_SELECTED"]
    assert len(selected) == 1 and selected[0][2] == {"profile": "bench", "transport": "replay"}


def test_the_service_refuses_to_start_with_the_replay_transport_in_the_demo_profile(tmp_path: Path) -> None:
    env = harness_env(tmp_path, {ENV_PROFILE: "demo", ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))})
    telemetry = InMemoryTelemetry()
    result = assemble_harness(env, ManualClock(), telemetry)
    assert result.harness is None and "DEMO_REFUSES_REPLAY" in result.reason
    assert str(tmp_path) not in result.reason and SECRET_DIR_NAME not in result.reason
    assert [e[0] for e in telemetry.events] == ["HARNESS_CONFIG_REJECTED"]


def test_the_app_is_not_ready_in_the_demo_profile_with_a_replay_setting_and_says_why(tmp_path: Path) -> None:
    from fastapi.testclient import TestClient

    env = harness_env(tmp_path, {ENV_PROFILE: "demo", ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))})
    client = TestClient(create_app(environ=env, telemetry=InMemoryTelemetry()))
    answer = client.get("/readyz")
    assert answer.status_code == 503 and "DEMO_REFUSES_REPLAY" in answer.json()["reason"]
    assert client.get("/healthz").status_code == 200


def test_an_unknown_profile_leaves_no_harness_even_with_no_replay_setting(tmp_path: Path) -> None:
    result = assemble_harness(harness_env(tmp_path, {ENV_PROFILE: "prod"}), ManualClock(), InMemoryTelemetry())
    assert result.harness is None and "UNKNOWN_PROFILE" in result.reason


def test_the_live_path_still_needs_its_pool_size_and_the_replay_path_does_not_build_a_pool(tmp_path: Path) -> None:
    live = harness_env(tmp_path, {ENV_PROFILE: "bench"})
    del live[ENV_LLM_POOL]
    assert assemble_harness(live, ManualClock(), InMemoryTelemetry()).harness is None
    replay = harness_env(tmp_path, {ENV_PROFILE: "bench", ENV_REPLAY_FIXTURES: str(fixtures(tmp_path))})
    del replay[ENV_LLM_POOL]
    assert assemble_harness(replay, ManualClock(), InMemoryTelemetry()).harness is not None


# ---- the module -----------------------------------------------------------------------------------------------------------------------------------------------------------------


def test_the_selection_reads_only_the_mapping_it_is_given() -> None:
    tree = ast.parse(Path(profile_module.__file__).read_text(encoding="utf-8"))
    for node in ast.walk(tree):
        if isinstance(node, ast.Attribute) and node.attr in {"environ", "getenv"}:
            raise AssertionError("the profile module reads the process environment")
        if isinstance(node, (ast.Import, ast.ImportFrom)):
            names = [a.name for a in node.names] + [getattr(node, "module", None) or ""]
            assert not any(n.split(".")[0] in {"os", "socket", "subprocess", "random", "time", "urllib", "http"} for n in names)
