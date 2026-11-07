"""A provider timeout must not close the live audio source (VOI-10, REQ-VOI-002; Codex P2 on #218).

When a provider's deadline expires while it waits for the next live chunk, the cancellation must not reach the microphone:
the next provider has to receive the whole utterance, not the part heard before the stall.
"""

import asyncio
from collections.abc import AsyncGenerator, AsyncIterator

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
from noema_model.providers.speech.errors import ProviderFailure
from noema_model.voice.failover import FailoverResult, transcribe_with_failover
from tests.support import run

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)


def caps(name: str) -> ProviderCapabilities:
    return ProviderCapabilities(name, PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, Support.YES, "v1", True)


def chunk(i: int) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, b"\x01\x00" * 320)


async def live_source(gate: asyncio.Event) -> AsyncGenerator[AudioChunk, None]:
    """A microphone that has one chunk ready and the next one only after `gate` is set."""
    yield chunk(0)
    await gate.wait()
    yield chunk(1)


class Reader:
    def __init__(self, name: str, wants: int, gate: asyncio.Event | None = None) -> None:
        self.name = name
        self.capabilities = caps(name)
        self._wants, self._gate = wants, gate
        self.received: list[int] = []

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        if self._gate is not None:
            self._gate.set()  # the microphone's next chunk becomes available as this provider starts
        async for item in audio:
            self.received.append(item.sequence)
            if len(self.received) >= self._wants:
                break
        yield FinalTranscript("send fifty thousand", (TokenConfidence("send", 0.95),), 0.95, None, None)


def test_a_provider_timeout_while_waiting_for_live_audio_does_not_truncate_the_next_providers_utterance() -> None:
    async def call() -> tuple[Reader, Reader, FailoverResult]:
        gate = asyncio.Event()
        first, second = Reader("a", wants=2), Reader("b", wants=2, gate=gate)
        result = await transcribe_with_failover(
            [first, second], REQUEST, live_source(gate), deadline_s=0.05, on_partial=lambda _: None, max_buffered_chunks=10
        )
        return first, second, result

    first, second, result = run(call())
    assert first.received == [0]  # it was waiting for chunk 1 when its deadline expired
    assert second.received == [0, 1]  # the whole utterance, including the chunk that arrived after the stall
    assert result.utterance is not None and result.utterance.provider_name == "b"
    assert [(s.provider_name, s.failure) for s in result.steps] == [("a", ProviderFailure.TIMEOUT)]


def test_when_the_source_really_ends_early_the_providers_still_see_what_there_was() -> None:
    async def short() -> AsyncGenerator[AudioChunk, None]:
        yield chunk(0)

    async def call() -> Reader:
        reader = Reader("a", wants=5)
        await transcribe_with_failover([reader], REQUEST, short(), deadline_s=1.0, on_partial=lambda _: None, max_buffered_chunks=10)
        return reader

    assert run(call()).received == [0]
