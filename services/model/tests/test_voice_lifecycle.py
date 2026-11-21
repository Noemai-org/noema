"""Audio-source lifecycle tests (VOI-02, REQ-VOI-004, AGENT-13; Codex finding P1 on #193).

A live microphone must not stay open after an utterance ends, however it ends: the endpoint is found, the provider fails,
the provider goes silent, or closing one stream itself fails.
"""

import asyncio
from collections.abc import AsyncIterator

import pytest

from noema_model.providers.speech.base import AudioChunk, FinalTranscript, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.endpoint import EndpointDetector, until_end_of_speech
from noema_model.voice.pipeline import transcribe_utterance
from tests.support import run
from tests.support_voice import CAPS, REQUEST, VAD, Source, loud_then_silent


class Failing:
    name = "failing"
    capabilities = CAPS

    def __init__(self, error: Exception | None = None, hang: bool = False) -> None:
        self._error, self._hang = error, hang

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        async for _ in audio:
            break
        if self._hang:
            await asyncio.Event().wait()
        if self._error is not None:
            raise self._error
        yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)


class BadClose:
    """Yields a final, then fails while it is being closed."""

    name = "badclose"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        try:
            yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)
        finally:
            raise RuntimeError("close failed")


async def collect(stream: AsyncIterator[AudioChunk]) -> list[AudioChunk]:
    return [item async for item in stream]


def test_the_gate_closes_the_underlying_source_when_the_utterance_ends_early() -> None:
    source = Source(loud_then_silent())
    run(collect(until_end_of_speech(source.stream(), EndpointDetector(VAD))))
    assert source.closed


def test_the_gate_closes_the_underlying_source_when_the_consumer_stops_early() -> None:
    source = Source(loud_then_silent())

    async def take_one() -> None:
        gated = until_end_of_speech(source.stream(), EndpointDetector(VAD))
        await gated.__anext__()
        await gated.aclose()

    run(take_one())
    assert source.closed


@pytest.mark.parametrize("kind", [ProviderFailure.TIMEOUT, ProviderFailure.UNAVAILABLE])
def test_a_provider_failure_closes_the_underlying_audio_source(kind: ProviderFailure) -> None:
    source = Source(loud_then_silent())
    gated = until_end_of_speech(source.stream(), EndpointDetector(VAD))
    with pytest.raises(ProviderError):
        run(transcribe_utterance(Failing(ProviderError(kind)), REQUEST, gated, deadline_s=5.0, on_partial=lambda _: None))
    assert source.closed


def test_a_provider_that_goes_silent_closes_the_underlying_audio_source_at_the_deadline() -> None:
    source = Source(loud_then_silent())
    gated = until_end_of_speech(source.stream(), EndpointDetector(VAD))
    with pytest.raises(ProviderError) as caught:
        run(transcribe_utterance(Failing(hang=True), REQUEST, gated, deadline_s=0.05, on_partial=lambda _: None))
    assert caught.value.kind is ProviderFailure.TIMEOUT
    assert source.closed


def test_a_failure_while_closing_the_provider_stream_still_closes_the_audio_source() -> None:
    source = Source(loud_then_silent())
    gated = until_end_of_speech(source.stream(), EndpointDetector(VAD))
    with pytest.raises(ProviderError) as caught:
        run(transcribe_utterance(BadClose(), REQUEST, gated, deadline_s=5.0, on_partial=lambda _: None))
    assert caught.value.kind is ProviderFailure.UNAVAILABLE  # typed, so a failover on ProviderError catches it (C2 P2)
    assert source.closed
