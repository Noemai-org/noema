"""Cleanup is bounded and always releases the audio source (VOI-02, REQ-VOI-004, AGENT-13; Codex P1 and P2 on #193).

A provider can refuse before it returns a stream, or stall while it is being closed. Neither may hold the result back or
leave a microphone open.
"""

import asyncio
from collections.abc import AsyncIterator

import pytest

from noema_model.providers.speech.base import AudioChunk, FinalTranscript, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.endpoint import AudioSource, EndpointDetector, until_end_of_speech
from noema_model.voice.pipeline import transcribe_utterance
from tests.support import run
from tests.support_voice import CAPS, REQUEST, VAD, Source, loud_then_silent

GUARD_S = 5.0  # a hang is a test failure, not a stuck CI job


class RefusesBeforeAStream:
    """transcribe() is a plain method: an adapter may legitimately refuse before it returns an iterator."""

    name = "refuses"
    capabilities = CAPS

    def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        raise ProviderError(ProviderFailure.UNAVAILABLE)


class StallsWhileClosing:
    """Yields a final at once, then never finishes closing."""

    name = "stalls"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        try:
            yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)
        finally:
            await asyncio.Event().wait()


def gated(source: Source) -> AudioSource:
    return until_end_of_speech(source.stream(), EndpointDetector(VAD))


def test_a_provider_that_refuses_before_returning_a_stream_still_releases_the_source() -> None:
    source = Source(loud_then_silent())
    with pytest.raises(ProviderError) as caught:
        run(transcribe_utterance(RefusesBeforeAStream(), REQUEST, gated(source), deadline_s=1.0, on_partial=lambda _: None))
    assert caught.value.kind is ProviderFailure.UNAVAILABLE
    assert source.closed


def test_a_provider_close_that_never_returns_cannot_hold_the_result_or_the_microphone() -> None:
    source = Source(loud_then_silent())

    async def call() -> str:
        result = await asyncio.wait_for(
            transcribe_utterance(StallsWhileClosing(), REQUEST, gated(source), deadline_s=0.05, on_partial=lambda _: None),
            timeout=GUARD_S,
        )
        return result.text

    assert run(call()) == "send ten"
    assert source.closed
