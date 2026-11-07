"""Audio-source lifecycle tests (VOI-02, REQ-VOI-004, AGENT-13; Codex finding P1 on #193).

A live microphone must not stay open after an utterance ends, however it ends: the endpoint is found, the provider fails,
the provider goes silent, or closing one stream itself fails.
"""

from collections.abc import AsyncIterator, Sequence

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    FinalTranscript,
    Locale,
    PipelineMode,
    ProviderCapabilities,
    SpeechEvent,
    StreamRequest,
    Support,
    TokenConfidence,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.endpoint import EndpointDetector, VadConfig, until_end_of_speech
from noema_model.voice.pipeline import transcribe_utterance
from tests.support import run

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)
CAPS = ProviderCapabilities("fake", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, Support.YES, "v1", True)
VAD = VadConfig(speech_rms=300, min_speech_ms=60, hangover_ms=100)


def chunk(i: int, level: int) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, level.to_bytes(2, "little", signed=True) * 320)


class Source:
    """A microphone stand-in with an explicit handle: `aclose()` releases it whether or not anything was read, as a real
    device handle does. (An async generator that never started has opened nothing, so it is not a faithful stand-in.)"""

    def __init__(self, chunks: Sequence[AudioChunk]) -> None:
        self._items = iter(chunks)
        self.closed = False

    def stream(self) -> "Source":
        return self

    def __aiter__(self) -> "Source":
        return self

    async def __anext__(self) -> AudioChunk:
        try:
            return next(self._items)
        except StopIteration:
            raise StopAsyncIteration from None

    async def aclose(self) -> None:
        self.closed = True


class Failing:
    name = "failing"
    capabilities = CAPS

    def __init__(self, error: Exception | None = None, hang: bool = False) -> None:
        self._error, self._hang = error, hang

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        async for _ in audio:
            break
        if self._hang:
            import asyncio

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


def loud_then_silent() -> list[AudioChunk]:
    return [chunk(i, 2000) for i in range(4)] + [chunk(4 + i, 5) for i in range(5)] + [chunk(9, 2000)] * 3


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
    with pytest.raises(RuntimeError):
        run(transcribe_utterance(BadClose(), REQUEST, gated, deadline_s=5.0, on_partial=lambda _: None))
    assert source.closed
