"""Provider cancellation is bounded and leaves no audio behind (VOI-02, REQ-VOI-004, INV-07, AGENT-13).

Codex P1 on #193 at c674bbf: a deadline cancels a provider that is waiting for its next event, but a provider whose
shutdown work blocks (an `await` in the generator's `finally`) kept the caller waiting for that work, so the deadline
did not hold and the microphone stayed open. Security Reviewer note on #193: a stalled provider task that is abandoned
must not keep the audio source reachable. C2 P2/P3 on #193: a display callback that raises must still release the audio.

The stall here is a real asyncio wait that really never finishes, not a raised exception standing in for one. The provider
is still a scripted stand-in for a vendor: see the recorded exception request on #193 (AGENT-12).
"""

import asyncio
import gc
import weakref
from collections.abc import AsyncIterator

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    FinalTranscript,
    PartialTranscript,
    SpeechEvent,
    StreamRequest,
    TokenConfidence,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.endpoint import AudioSource, EndpointDetector, until_end_of_speech
from noema_model.voice.pipeline import transcribe_utterance
from tests.support import run
from tests.support_voice import CAPS, REQUEST, VAD, Source, loud_then_silent

GUARD_S = 5.0  # a hang is a test failure, not a stuck CI job
DEADLINE_S = 0.05


class StallsOnCancel:
    """Yields a partial, then waits for its next event. When it is cancelled it awaits shutdown work that never ends."""

    name = "stalls-on-cancel"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        try:
            yield PartialTranscript(0, "send ten")
            await asyncio.Event().wait()
        finally:
            await asyncio.Event().wait()


class StallsWhileClosing:
    """Yields a final at once, then never finishes closing."""

    name = "stalls-closing"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        try:
            yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)
        finally:
            await asyncio.Event().wait()


class OneFinal:
    name = "one-final"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        async for _ in audio:
            break
        yield PartialTranscript(0, "send")
        yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)


def gated(source: Source) -> AudioSource:
    return until_end_of_speech(source.stream(), EndpointDetector(VAD))


def test_a_provider_that_blocks_while_being_cancelled_cannot_hold_the_deadline_or_the_microphone() -> None:
    source = Source(loud_then_silent())

    async def call() -> ProviderError:
        try:
            await asyncio.wait_for(
                transcribe_utterance(StallsOnCancel(), REQUEST, gated(source), deadline_s=DEADLINE_S, on_partial=lambda _: None),
                timeout=GUARD_S,
            )
        except ProviderError as error:
            return error
        raise AssertionError("expected a ProviderError")

    error = run(call())
    assert error.kind is ProviderFailure.TIMEOUT
    assert source.closed


def test_an_abandoned_provider_task_cannot_keep_the_audio_source_reachable() -> None:
    async def call() -> bool:
        source = Source(loud_then_silent())
        reference = weakref.ref(source)
        result = await asyncio.wait_for(
            transcribe_utterance(StallsWhileClosing(), REQUEST, gated(source), deadline_s=DEADLINE_S, on_partial=lambda _: None),
            timeout=GUARD_S,
        )
        assert result.text == "send ten"
        del source
        gc.collect()
        return reference() is None  # the stalled provider's task is still pending while this is checked

    assert run(call())


def test_a_failure_in_the_display_callback_still_releases_the_audio_source() -> None:
    source = Source(loud_then_silent())

    def display(_: str) -> None:
        raise RuntimeError("the screen failed")

    with pytest.raises(RuntimeError):
        run(transcribe_utterance(OneFinal(), REQUEST, gated(source), deadline_s=GUARD_S, on_partial=display))
    assert source.closed
