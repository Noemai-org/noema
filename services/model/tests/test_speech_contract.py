"""Speech provider interface tests (VOI-01, REQ-VOI-002, REQ-VOI-003, REQ-INT-006).

No vendor document, recorded sample or live endpoint has been read for Aethex AI, Indigenius or N-ATLAS, so there is no
per-provider sample to play. These tests pin what is true without one: the neutral types, the stream-order contract
every adapter must pass, the capability matrix and its strict reader, and that the three scaffolds refuse rather than
guess. The recorded-sample tests arrive with each adapter's wire decode.
"""

import copy
import json
import math
from collections.abc import AsyncIterator, Callable
from pathlib import Path
from typing import Any

import pytest

from noema_model.platform.config import ConfigError
from noema_model.providers.llm.base import AdapterSettings, ProviderError, ProviderFailure
from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    FinalTranscript,
    Locale,
    PartialTranscript,
    PipelineMode,
    SpeechProvider,
    StreamRequest,
    Support,
    TokenConfidence,
    validate_event_order,
)
from noema_model.providers.speech.matrix import MATRIX_PATH, load_matrix, parse_matrix
from noema_model.providers.speech.registry import SPEECH_KINDS, build_speech_provider
from tests.support import run

SETTINGS = AdapterSettings(endpoint="https://vendor.example.invalid/", credential="CRED-VALUE", api_version=None,
                           timeout_ms=1500, max_response_bytes=65536)
REQUEST = StreamRequest(model_version="pinned-model-2026-01-01", locale_hint=Locale.PCM)


def final(text: str = "send five thousand") -> FinalTranscript:
    return FinalTranscript(text=text, tokens=(TokenConfidence("send", 0.9),), utterance_confidence=0.9,
                           detected_locale=Locale.EN_NG, model_version_reported=None)


def document() -> dict[str, Any]:
    loaded: dict[str, Any] = json.loads(MATRIX_PATH.read_text(encoding="utf-8"))
    return copy.deepcopy(loaded)


# ---- the neutral types -------------------------------------------------------------------------------------------


@pytest.mark.parametrize("bad", [-0.01, 1.01, math.nan, math.inf, True])
def test_a_confidence_outside_zero_to_one_is_malformed_not_clamped(bad: float) -> None:
    with pytest.raises(ProviderError) as caught:
        TokenConfidence("send", bad)
    assert caught.value.kind is ProviderFailure.MALFORMED
    with pytest.raises(ProviderError):
        FinalTranscript("x", (), bad, None, None)


def test_a_missing_confidence_stays_missing_and_is_never_turned_into_a_number() -> None:
    token = TokenConfidence("send", None)
    assert token.confidence is None
    assert final().utterance_confidence == 0.9
    assert FinalTranscript("x", (token,), None, None, None).utterance_confidence is None


def test_the_edges_zero_and_one_are_valid_confidences() -> None:
    assert TokenConfidence("a", 0.0).confidence == 0.0
    assert TokenConfidence("a", 1.0).confidence == 1.0


def test_raw_audio_bytes_never_appear_in_a_repr() -> None:
    chunk = AudioChunk(sequence=1, codec=AudioCodec.OPUS, sample_rate_hz=16000, data=b"RAW-AUDIO-MARKER")
    assert "RAW-AUDIO-MARKER" not in repr(chunk)
    assert "RAW-AUDIO-MARKER" not in str(chunk)


def test_a_partial_transcript_is_not_a_final_and_has_no_confidences() -> None:
    partial = PartialTranscript(sequence=1, text="send fi")
    assert not isinstance(partial, FinalTranscript)
    assert not hasattr(partial, "tokens")
    assert not hasattr(partial, "utterance_confidence")


# ---- the stream-order contract -------------------------------------------------------------------------------------


def test_partials_then_one_final_is_a_valid_stream() -> None:
    validate_event_order([PartialTranscript(0, "s"), PartialTranscript(1, "send"), final()])


def test_a_final_alone_partials_alone_and_nothing_pass_the_order_check() -> None:
    validate_event_order([final()])
    validate_event_order([PartialTranscript(0, "s")])  # ending with no final is judged by the caller, as a failure
    validate_event_order([])


@pytest.mark.parametrize(
    "events",
    [
        [final(), PartialTranscript(1, "late")],
        [final(), final()],
        [PartialTranscript(1, "a"), PartialTranscript(1, "again")],
        [PartialTranscript(2, "a"), PartialTranscript(1, "back")],
    ],
    ids=["partial-after-final", "two-finals", "repeated-sequence", "sequence-goes-back"],
)
def test_a_stream_that_breaks_the_order_is_malformed(events: list[Any]) -> None:
    with pytest.raises(ProviderError) as caught:
        validate_event_order(events)
    assert caught.value.kind is ProviderFailure.MALFORMED


# ---- the capability matrix -----------------------------------------------------------------------------------------


def test_the_committed_matrix_covers_the_three_providers_and_claims_nothing() -> None:
    matrix = load_matrix()
    assert set(matrix) == {"aethex_ai", "indigenius", "n_atlas"}
    for caps in matrix.values():
        assert caps.mode is PipelineMode.TRANSCRIBE_ONLY
        assert caps.streaming_asr is Support.UNVERIFIED
        assert caps.retention_terms is Support.UNVERIFIED
        assert {locale for locale, _ in caps.asr_locales} == set(Locale)
        assert {locale for locale, _ in caps.tts_locales} == set(Locale)
        assert all(support is Support.UNVERIFIED for _, support in caps.asr_locales + caps.tts_locales)
        assert caps.latency_ms_p95 is None
        assert caps.pinned_model_version is None
        assert caps.qualified is False


def mutate_missing_latency(doc: dict[str, Any]) -> None:
    del doc["providers"][0]["latency_ms_p95"]


def mutate_missing_pin(doc: dict[str, Any]) -> None:
    del doc["providers"][0]["pinned_model_version"]


def mutate_unknown_locale(doc: dict[str, Any]) -> None:
    doc["providers"][0]["asr_locales"]["fr"] = "YES"


def mutate_missing_locale(doc: dict[str, Any]) -> None:
    del doc["providers"][0]["tts_locales"]["ig"]


def mutate_unknown_key(doc: dict[str, Any]) -> None:
    doc["providers"][0]["notes"] = "x"


def mutate_unknown_support(doc: dict[str, Any]) -> None:
    doc["providers"][0]["streaming_asr"] = "MAYBE"


def mutate_unknown_mode(doc: dict[str, Any]) -> None:
    doc["providers"][0]["mode"] = "BOTH"


def mutate_duplicate(doc: dict[str, Any]) -> None:
    doc["providers"][1]["name"] = doc["providers"][0]["name"]


def mutate_qualified_without_pin(doc: dict[str, Any]) -> None:
    doc["providers"][0]["qualified"] = True
    doc["providers"][0]["retention_terms"] = "YES"


def mutate_qualified_without_terms(doc: dict[str, Any]) -> None:
    doc["providers"][0]["qualified"] = True
    doc["providers"][0]["pinned_model_version"] = "v1"


def mutate_empty(doc: dict[str, Any]) -> None:
    doc["providers"] = []


def mutate_version(doc: dict[str, Any]) -> None:
    doc["matrix_version"] = 2


def mutate_zero_latency(doc: dict[str, Any]) -> None:
    doc["providers"][0]["latency_ms_p95"] = 0


BROKEN: list[Callable[[dict[str, Any]], None]] = [
    mutate_missing_latency, mutate_missing_pin, mutate_unknown_locale, mutate_missing_locale, mutate_unknown_key,
    mutate_unknown_support, mutate_unknown_mode, mutate_duplicate, mutate_qualified_without_pin,
    mutate_qualified_without_terms, mutate_empty, mutate_version, mutate_zero_latency,
]


@pytest.mark.parametrize("mutate", BROKEN, ids=[m.__name__ for m in BROKEN])
def test_the_matrix_reader_refuses_anything_it_would_have_to_default(mutate: Callable[[dict[str, Any]], None]) -> None:
    doc = document()
    mutate(doc)
    with pytest.raises(ConfigError):
        parse_matrix(doc)


def test_a_provider_with_a_pin_and_confirmed_terms_may_be_qualified() -> None:
    doc = document()
    doc["providers"][0].update(qualified=True, retention_terms="YES", pinned_model_version="asr-2026-11-01",
                               latency_ms_p95=900)
    caps = parse_matrix(doc)["aethex_ai"]
    assert caps.qualified and caps.latency_ms_p95 == 900 and caps.pinned_model_version == "asr-2026-11-01"


def test_a_matrix_that_is_not_an_object_or_not_readable_is_a_config_error(tmp_path: Path) -> None:
    with pytest.raises(ConfigError):
        parse_matrix([])
    with pytest.raises(ConfigError):
        load_matrix(tmp_path / "missing.json")
    broken = tmp_path / "broken.json"
    broken.write_text("{not json", encoding="utf-8")
    with pytest.raises(ConfigError):
        load_matrix(broken)


# ---- the registry and the scaffolds --------------------------------------------------------------------------------


@pytest.mark.parametrize("kind", sorted(SPEECH_KINDS))
def test_each_kind_builds_from_its_matrix_row_and_satisfies_the_interface(kind: str) -> None:
    provider = build_speech_provider(kind, f"{kind}-primary", SETTINGS)
    assert isinstance(provider, SpeechProvider)
    assert provider.name == f"{kind}-primary"
    assert provider.capabilities.name == kind
    assert provider.capabilities.qualified is False


def test_an_unknown_kind_and_a_matrix_that_disagrees_with_the_kinds_are_config_errors() -> None:
    with pytest.raises(ConfigError):
        build_speech_provider("deepgram", "x", SETTINGS)
    doc = document()
    doc["providers"][2]["name"] = "elsewhere"
    with pytest.raises(ConfigError):
        build_speech_provider("aethex_ai", "x", SETTINGS, parse_matrix(doc))
    doc = document()
    del doc["providers"][2]
    with pytest.raises(ConfigError):
        build_speech_provider("aethex_ai", "x", SETTINGS, parse_matrix(doc))


@pytest.mark.parametrize("kind", sorted(SPEECH_KINDS))
def test_a_scaffold_refuses_locally_and_reads_no_audio(kind: str) -> None:
    provider = build_speech_provider(kind, "p", SETTINGS)
    pulled: list[int] = []

    async def audio() -> AsyncIterator[AudioChunk]:
        pulled.append(1)
        yield AudioChunk(0, AudioCodec.OPUS, 16000, b"x")

    async def drain() -> None:
        async for _ in provider.transcribe(REQUEST, audio()):
            raise AssertionError("a scaffold must not yield an event")

    with pytest.raises(ProviderError) as caught:
        run(drain())
    assert caught.value.kind is ProviderFailure.REJECTED
    assert caught.value.local is True
    assert pulled == []
    assert "CRED-VALUE" not in str(caught.value)
