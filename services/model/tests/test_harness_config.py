"""Harness configuration is strict data: nothing defaulted, nothing unpinned, nothing plaintext (AGENT-16, REQ-INT-002)."""

import copy
from typing import Any

import pytest

from noema_model.harness.config import build_providers, check_pinned, load_harness_config
from noema_model.platform.config import ConfigError
from noema_model.providers.llm.base import HttpResponse
from tests.support import FIXTURES, RecordingTransport, make_config, raw_config


def broken(mutate: Any) -> dict[str, Any]:
    raw = copy.deepcopy(raw_config())
    mutate(raw)
    return raw


def test_a_valid_config_loads() -> None:
    config = make_config()
    assert set(config.providers) == {"alpha", "beta"}
    assert config.routes["slot_values"].providers_for(None) == ("alpha", "beta")
    assert config.data_limits.cap_for("utterance") == 2000 and config.data_limits.cap_for("other") == 256


@pytest.mark.parametrize(
    "mutate",
    [
        lambda r: r.pop("breaker"),
        lambda r: r["breaker"].pop("cooldown_ms"),
        lambda r: r["masking"].pop("account_run_min_digits"),
        lambda r: r.update(surprise=1),
        lambda r: r["providers"]["alpha"].update(surprise=1),
        lambda r: r["providers"]["alpha"].pop("timeout_ms"),
        lambda r: r["providers"]["alpha"].update(timeout_ms=0),
        lambda r: r["providers"]["alpha"].update(timeout_ms=True),
        lambda r: r["providers"]["alpha"].update(endpoint="http://alpha.example.invalid"),
        lambda r: r["providers"]["alpha"].update(kind="nonesuch"),
        lambda r: r["providers"]["alpha"].update(model_version="latest"),
        lambda r: r["providers"]["alpha"].update(model_version="model-alpha-latest-2026"),
        lambda r: r["providers"]["alpha"].update(model_version="nodigitshere"),
        lambda r: r["providers"]["alpha"].update(model_version=""),
        lambda r: r["providers"]["alpha"]["rate"].update(as_of="yesterday"),
        lambda r: r["routes"]["slot_values"].update(default=["nobody"]),
        lambda r: r["routes"]["slot_values"].update(default=["alpha", "alpha"]),
        lambda r: r["routes"]["slot_values"].update(default=[]),
        lambda r: r["routes"]["slot_values"]["by_locale"].update({"NOT A LOCALE": ["alpha"]}),
        lambda r: r["providers"].clear(),
    ],
)
def test_a_bad_config_is_refused(mutate: Any) -> None:
    with pytest.raises(ConfigError):
        load_harness_config(broken(mutate))


def test_anthropic_needs_an_api_version() -> None:
    raw = raw_config()
    raw["providers"]["alpha"]["kind"] = "anthropic_messages"
    with pytest.raises(ConfigError):
        load_harness_config(raw)


@pytest.mark.parametrize("good", ["claude-x-20251001", "gpt-4o-2024-08-06", "gemini-2.5-pro-001", "m1"])
def test_pinned_versions_pass(good: str) -> None:
    check_pinned(good)


# A digit-bearing alias such as "gpt-4o" cannot be told from a pin by its spelling. The call-time rule catches it: the
# provider reports the snapshot it ran, and a reply whose version differs from the pin is refused (test_harness_call).
@pytest.mark.parametrize("bad", ["latest", "model-default-1", "stable-2", "", " x1", "x/../1"])
def test_unpinned_or_odd_versions_fail(bad: str) -> None:
    with pytest.raises(ConfigError):
        check_pinned(bad)


def test_providers_are_built_with_the_mounted_credential() -> None:
    providers = build_providers(make_config(), RecordingTransport(HttpResponse(200, b"{}")))
    assert set(providers) == {"alpha", "beta"}


def test_a_missing_or_empty_credential_file_stops_startup(tmp_path: Any) -> None:
    raw = raw_config(providers=("alpha",))
    raw["providers"]["alpha"]["credential_file"] = str(tmp_path / "absent")
    with pytest.raises(ConfigError):
        build_providers(load_harness_config(raw), RecordingTransport(HttpResponse(200, b"{}")))
    empty = tmp_path / "empty"
    empty.write_text("\n")
    raw["providers"]["alpha"]["credential_file"] = str(empty)
    with pytest.raises(ConfigError):
        build_providers(load_harness_config(raw), RecordingTransport(HttpResponse(200, b"{}")))
    assert (FIXTURES / "credential.txt").exists()
