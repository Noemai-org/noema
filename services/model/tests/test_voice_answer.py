"""Answer delivery tests (VOI-04, REQ-VOI-002, PERD §35.3 "TTS outage": display text in the overlay).

The text of an answer is always delivered; speech is an addition that is used only where a provider is known to cover the
locale and only while it works. A synthesizer here is a scripted fake: no vendor wire format is assumed.
"""

import asyncio
from collections.abc import AsyncIterator, Sequence

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    Locale,
    PipelineMode,
    ProviderCapabilities,
    Support,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.tts import SpeechSynthesizer, SynthesisRequest
from noema_model.voice.answer import DeliveryMode, deliver_answer, plan_delivery
from tests.support import run

LOCALES = tuple(Locale)


def caps(tts: Support, locale: Locale = Locale.PCM, name: str = "fake") -> ProviderCapabilities:
    cells = tuple((item, tts if item is locale else Support.UNVERIFIED) for item in LOCALES)
    return ProviderCapabilities(name, PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), cells, None, Support.YES, "v1", True)


def audio(count: int = 3) -> list[AudioChunk]:
    return [AudioChunk(i, AudioCodec.PCM_S16LE, 16000, b"\x01\x00" * 160) for i in range(count)]


class FakeSynth:
    def __init__(self, capabilities: ProviderCapabilities, chunks: Sequence[AudioChunk] = (), raises: Exception | None = None,
                 hang: bool = False) -> None:
        self.name = capabilities.name
        self.capabilities = capabilities
        self._chunks = chunks
        self._raises = raises
        self._hang = hang
        self.calls = 0

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        self.calls += 1
        for chunk in self._chunks:
            yield chunk
        if self._raises is not None:
            raise self._raises
        if self._hang:
            await asyncio.Event().wait()


REQUEST = SynthesisRequest(text="We found your transfer.", locale=Locale.PCM, model_version="pinned-1")


def test_the_fake_satisfies_the_interface() -> None:
    assert isinstance(FakeSynth(caps(Support.YES)), SpeechSynthesizer)


@pytest.mark.parametrize("support,expected", [(Support.YES, DeliveryMode.SPEAK), (Support.NO, DeliveryMode.TEXT_ONLY),
                                              (Support.UNVERIFIED, DeliveryMode.TEXT_ONLY)])
def test_speech_is_planned_only_where_the_matrix_says_yes(support: Support, expected: DeliveryMode) -> None:
    assert plan_delivery(Locale.PCM, caps(support)) is expected


def test_a_locale_missing_from_the_matrix_is_text_only() -> None:
    partial = ProviderCapabilities("p", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), ((Locale.EN_NG, Support.YES),), None,
                                   Support.YES, "v1", True)
    assert plan_delivery(Locale.YO, partial) is DeliveryMode.TEXT_ONLY


def test_an_unqualified_provider_is_never_used_for_speech_even_if_a_cell_says_yes() -> None:
    unqualified = ProviderCapabilities("p", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), ((Locale.PCM, Support.YES),), None,
                                       Support.UNVERIFIED, None, False)
    assert plan_delivery(Locale.PCM, unqualified) is DeliveryMode.TEXT_ONLY


def test_where_tts_is_not_covered_the_text_is_delivered_and_the_synthesizer_is_not_called() -> None:
    synth = FakeSynth(caps(Support.UNVERIFIED), audio())
    result = run(deliver_answer(synth, REQUEST, deadline_s=5.0))
    assert result.mode is DeliveryMode.TEXT_ONLY
    assert result.text == REQUEST.text
    assert result.audio == ()
    assert result.failure is None
    assert synth.calls == 0


def test_where_tts_works_the_text_and_the_audio_are_both_delivered() -> None:
    synth = FakeSynth(caps(Support.YES), audio(3))
    result = run(deliver_answer(synth, REQUEST, deadline_s=5.0))
    assert result.mode is DeliveryMode.SPEAK
    assert result.text == REQUEST.text
    assert [c.sequence for c in result.audio] == [0, 1, 2]
    assert result.failure is None


@pytest.mark.parametrize("kind", [ProviderFailure.TIMEOUT, ProviderFailure.UNAVAILABLE, ProviderFailure.RATE_LIMITED])
def test_a_tts_outage_falls_back_to_text_and_says_why(kind: ProviderFailure) -> None:
    synth = FakeSynth(caps(Support.YES), audio(2), raises=ProviderError(kind))
    result = run(deliver_answer(synth, REQUEST, deadline_s=5.0))
    assert result.mode is DeliveryMode.TEXT_ONLY
    assert result.text == REQUEST.text
    assert result.audio == ()  # half an answer in audio is never played
    assert result.failure is kind


def test_a_synthesizer_that_goes_silent_is_a_timeout_and_falls_back_to_text() -> None:
    result = run(deliver_answer(FakeSynth(caps(Support.YES), audio(1), hang=True), REQUEST, deadline_s=0.05))
    assert result.mode is DeliveryMode.TEXT_ONLY
    assert result.failure is ProviderFailure.TIMEOUT


def test_a_synthesizer_that_returns_no_audio_falls_back_to_text() -> None:
    result = run(deliver_answer(FakeSynth(caps(Support.YES), ()), REQUEST, deadline_s=5.0))
    assert result.mode is DeliveryMode.TEXT_ONLY
    assert result.failure is ProviderFailure.MALFORMED


def test_a_chunk_with_no_bytes_is_malformed_not_silence() -> None:
    empty = [AudioChunk(0, AudioCodec.PCM_S16LE, 16000, b"")]
    result = run(deliver_answer(FakeSynth(caps(Support.YES), empty), REQUEST, deadline_s=5.0))
    assert result.mode is DeliveryMode.TEXT_ONLY
    assert result.failure is ProviderFailure.MALFORMED


@pytest.mark.parametrize("text", ["", "   "])
def test_an_answer_with_no_text_is_refused(text: str) -> None:
    with pytest.raises(ValueError):
        SynthesisRequest(text=text, locale=Locale.PCM, model_version="v1")
