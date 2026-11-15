"""Modular pipeline guard tests (VOI-18, REQ-VOI-006): a configuration check refuses a speech-to-speech provider in the
bench and demo profiles."""

import dataclasses

import pytest

from noema_model.platform.config import ConfigError
from noema_model.providers.speech.base import PipelineMode, ProviderCapabilities
from noema_model.providers.speech.guard import check_modular_pipeline
from noema_model.providers.speech.matrix import load_matrix

TRANSCRIBE = tuple(load_matrix().values())
S2S = dataclasses.replace(TRANSCRIBE[0], name="s2s_vendor", mode=PipelineMode.SPEECH_TO_SPEECH)


@pytest.mark.parametrize("profile", ["bench", "demo"])
def test_a_speech_to_speech_provider_is_refused_in_bench_and_demo(profile: str) -> None:
    with pytest.raises(ConfigError) as caught:
        check_modular_pipeline(profile, (*TRANSCRIBE, S2S))
    assert "s2s_vendor" in str(caught.value)
    assert profile in str(caught.value)


@pytest.mark.parametrize("profile", ["bench", "demo"])
def test_a_speech_to_speech_provider_is_refused_even_as_the_only_failover_target(profile: str) -> None:
    with pytest.raises(ConfigError):
        check_modular_pipeline(profile, (S2S,))


@pytest.mark.parametrize("profile", ["bench", "demo", "dev", "ci"])
def test_transcribe_only_providers_pass_in_every_profile(profile: str) -> None:
    check_modular_pipeline(profile, TRANSCRIBE)
    check_modular_pipeline(profile, ())


@pytest.mark.parametrize("profile", ["dev", "ci"])
def test_dev_and_ci_may_hold_a_speech_to_speech_provider_to_benchmark_it(profile: str) -> None:
    check_modular_pipeline(profile, (*TRANSCRIBE, S2S))


@pytest.mark.parametrize("profile", ["", "prod", "production", "Bench", "bench ", "staging"])
def test_an_unknown_profile_fails_closed_even_for_modular_providers(profile: str) -> None:
    with pytest.raises(ConfigError):
        check_modular_pipeline(profile, TRANSCRIBE)


def test_every_provider_in_the_committed_matrix_is_modular() -> None:
    assert all(caps.mode is PipelineMode.TRANSCRIBE_ONLY for caps in load_matrix().values())
    assert isinstance(S2S, ProviderCapabilities)
