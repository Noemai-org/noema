"""Cleanup diagnostics go through structured telemetry with the originating correlation id, and an abandoned close of an `EndpointGate` can be retried
(VOI-02, REQ-VOI-004, REQ-OBS-001, AGENT-13; Codex on #474: P1 at 4f520682 "route cleanup diagnostics through structured, correlated telemetry", P2 at bdf93be7
"avoid poisoning the endpoint gate with a premature close"). Scripted sources (SYNTHETIC; the AGENT-12 exception of noema#412 covers the SpeechProvider seam).
"""

import asyncio
import gc
import weakref
from collections.abc import AsyncIterator

import pytest

from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.speech.base import AudioChunk, FinalTranscript, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError
from noema_model.voice.endpoint import EndpointDetector, until_end_of_speech
from noema_model.voice.pipeline import transcribe_utterance
from tests.support import run
from tests.support_voice import CAPS, REQUEST, VAD, Source, loud_then_silent

GUARD_S = 5.0
EVENT = "voice_audio_cleanup"
ABANDONED = "VOI_AUDIO_READ_ABANDONED"
CLOSE_FAILED = "VOI_AUDIO_DEFERRED_CLOSE_FAILED"
CLOSE_ABANDONED = "VOI_AUDIO_CLOSE_ABANDONED"


class Reader:
    name = "reader"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        async for _ in audio:
            pass
        yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)


class NeverEnding(Source):
    def __init__(self) -> None:
        super().__init__([])

    async def __anext__(self) -> AudioChunk:
        while True:
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                continue  # resists cancellation for ever


class NeverEndingStallingClose(NeverEnding):
    async def aclose(self) -> None:
        await asyncio.Event().wait()


class EndsLateThenFailsToClose(Source):
    def __init__(self) -> None:
        super().__init__([])

    async def __anext__(self) -> AudioChunk:
        try:
            await asyncio.Event().wait()
        except asyncio.CancelledError:
            await asyncio.sleep(0.2)
        raise StopAsyncIteration

    async def aclose(self) -> None:
        raise RuntimeError("secret-marker-that-must-not-be-emitted")


async def call(source: object, correlation_id: str, telemetry: InMemoryTelemetry, hold_s: float = 0.1) -> None:
    with pytest.raises(ProviderError):
        await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=hold_s,  # type: ignore[arg-type]
                                   on_partial=lambda _: None, telemetry=telemetry, correlation_id=correlation_id)


def test_an_abandoned_read_is_reported_as_a_structured_event_with_the_correlation_id() -> None:
    telemetry = InMemoryTelemetry()
    run(asyncio.wait_for(call(NeverEnding(), "corr-1", telemetry), timeout=GUARD_S))
    run(asyncio.sleep(0))
    assert (EVENT, "corr-1", {"code": ABANDONED}) in telemetry.events


def test_two_concurrent_calls_each_carry_their_own_correlation_id() -> None:
    telemetry = InMemoryTelemetry()

    async def scenario() -> None:
        await asyncio.gather(call(NeverEnding(), "corr-A", telemetry), call(NeverEnding(), "corr-B", telemetry))
        await asyncio.sleep(0.3)

    run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert sorted((cid, f["code"]) for name, cid, f in telemetry.events if name == EVENT) == [("corr-A", ABANDONED), ("corr-B", ABANDONED)]


def test_a_failed_deferred_close_is_reported_with_the_code_only_never_the_error_text() -> None:
    telemetry = InMemoryTelemetry()

    async def scenario() -> None:
        await call(EndsLateThenFailsToClose(), "corr-9", telemetry, hold_s=2.0)
        await asyncio.sleep(0.6)

    run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert telemetry.events == [(EVENT, "corr-9", {"code": CLOSE_FAILED})]
    assert "secret-marker" not in repr(telemetry.events)


def test_a_stalled_close_is_cancelled_after_its_bound_and_reported_and_the_source_is_freed() -> None:
    telemetry = InMemoryTelemetry()

    async def scenario() -> bool:
        source = NeverEndingStallingClose()
        ref = weakref.ref(source)
        await call(source, "corr-7", telemetry, hold_s=0.05)
        del source
        await asyncio.sleep(0.5)
        gc.collect()
        gc.collect()
        return ref() is None

    assert run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert [(cid, f["code"]) for name, cid, f in telemetry.events if name == EVENT] == [("corr-7", ABANDONED), ("corr-7", CLOSE_ABANDONED)]


class ResistantGenerator:
    """A real async generator whose read resists cancellation for 0.4 s and then yields a valid chunk. It cannot be closed while a read of it runs."""

    def __init__(self) -> None:
        self.closed = False
        self._gen = self._run()

    async def _run(self) -> AsyncIterator[AudioChunk]:
        try:
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                await asyncio.sleep(0.4)
            yield loud_then_silent()[0]
            await asyncio.Event().wait()
        finally:
            self.closed = True

    def __aiter__(self) -> "ResistantGenerator":
        return self

    async def __anext__(self) -> AudioChunk:
        return await self._gen.__anext__()

    async def aclose(self) -> None:
        await self._gen.aclose()


def test_a_gate_whose_close_failed_while_a_read_ran_is_closed_when_the_read_ends() -> None:
    source = ResistantGenerator()
    gate = until_end_of_speech(source, EndpointDetector(VAD))  # type: ignore[arg-type]
    telemetry = InMemoryTelemetry()

    async def scenario() -> tuple[bool, bool]:
        await call(gate, "corr-3", telemetry, hold_s=0.1)  # abandon at ~0.15 s: the close fails because the generator is still reading
        closed_at_return = source.closed
        await asyncio.sleep(0.8)  # the read ends at ~0.4 s; the gate's close must be tried again, not served from the failed first attempt
        return closed_at_return, source.closed

    closed_at_return, closed_later = run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert not closed_at_return
    assert closed_later  # Codex P2 at bdf93be7: EndpointGate cached the failed close and never closed the source
