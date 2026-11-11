"""The release of the audio source is bounded, survives cancellation and does not race a provider's read (VOI-02, REQ-VOI-004, INV-07, AGENT-13).

Codex on #193 at fa31b82: (P1) a source whose `aclose()` never returns kept the call pending; (P1) a close interrupted by the
deadline's cancellation was never finished by the pipeline's own close; (P1) a provider read still running in a separate task made the
source's close fail untyped; (P2) a provider shutdown failure that surfaces after the bounded wait was missed. Each stall here is a real
asyncio wait that really never ends, not a raised exception standing in for one. The providers and sources are scripted stand-ins
(SYNTHETIC; the recorded AGENT-12 exception on noema#412).
"""

import asyncio
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
from noema_model.voice.endpoint import EndpointDetector, until_end_of_speech
from noema_model.voice.pipeline import transcribe_utterance
from tests.support import run
from tests.support_voice import CAPS, REQUEST, VAD, Source, loud_then_silent

GUARD_S = 5.0  # a hang is a test failure, not a stuck CI job
BOUND_S = 0.1


def final() -> FinalTranscript:
    return FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)


class OneFinal:
    name = "one-final"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        async for _ in audio:
            break
        yield final()


class StallingSource(Source):
    """A microphone whose release never finishes."""

    async def aclose(self) -> None:
        await asyncio.Event().wait()


class SlowSource(Source):
    """A microphone whose release takes a moment, and records only when it has really finished."""

    async def aclose(self) -> None:
        await asyncio.sleep(0.2)
        self.closed = True


def test_a_source_whose_close_never_returns_cannot_hold_the_call_and_is_a_failure() -> None:
    source = StallingSource(loud_then_silent())

    async def call() -> ProviderError:
        try:
            await asyncio.wait_for(
                transcribe_utterance(OneFinal(), REQUEST, until_end_of_speech(source.stream(), EndpointDetector(VAD)),
                                     deadline_s=BOUND_S, cleanup_s=BOUND_S, on_partial=lambda _: None),
                timeout=GUARD_S)
        except ProviderError as error:
            return error
        raise AssertionError("a microphone that was not released must not look like a success")

    assert run(call()).kind is ProviderFailure.UNAVAILABLE


def test_a_close_interrupted_by_the_deadline_is_finished_by_the_pipeline() -> None:
    """The gate closes the source inside the provider task. The deadline cancels that task mid-close; the pipeline's own close must
    resume the unfinished work, not be suppressed because a close was merely started."""
    source = SlowSource(loud_then_silent())

    class Stalls:
        name = "stalls"
        capabilities = CAPS

        async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
            async for _ in audio:
                pass  # runs the gate to its end of speech, which starts closing the slow source
            await asyncio.Event().wait()
            yield final()

    async def call() -> ProviderError:
        try:
            await asyncio.wait_for(
                transcribe_utterance(Stalls(), REQUEST, until_end_of_speech(source.stream(), EndpointDetector(VAD)),
                                     deadline_s=0.05, cleanup_s=GUARD_S, on_partial=lambda _: None),
                timeout=GUARD_S)
        except ProviderError as error:
            return error
        raise AssertionError("expected a ProviderError")

    error = run(call())
    assert error.kind is ProviderFailure.TIMEOUT
    assert source.closed


def test_a_provider_read_that_outlives_the_provider_does_not_make_the_close_fail_untyped() -> None:
    """A sender task of the adapter keeps reading the audio after the provider is cancelled. The source is an async generator, which
    cannot be closed while a read of it is running; the pipeline must end the read first."""
    async def mic() -> AsyncIterator[AudioChunk]:
        try:
            yield loud_then_silent()[0]
            await asyncio.Event().wait()
            yield loud_then_silent()[0]
        finally:
            state["finished"] = True

    state: dict[str, bool] = {"finished": False}
    source = mic()

    class SenderTask:
        name = "sender-task"
        capabilities = CAPS

        async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
            async def pump() -> None:
                async for _ in audio:
                    pass

            sender = asyncio.ensure_future(pump())  # survives the provider's own cancellation
            yield PartialTranscript(0, "send")
            await asyncio.shield(sender)
            yield final()

    async def call() -> ProviderError:
        try:
            await asyncio.wait_for(
                transcribe_utterance(SenderTask(), REQUEST, source, deadline_s=0.05, cleanup_s=0.2, on_partial=lambda _: None),  # type: ignore[arg-type]
                timeout=GUARD_S)
        except ProviderError as error:
            return error
        raise AssertionError("expected a ProviderError")

    error = run(call())
    assert error.kind is ProviderFailure.TIMEOUT  # not a RuntimeError from closing a generator that is still running
    assert state["finished"]


def test_a_shutdown_failure_that_surfaces_after_the_bounded_wait_is_not_missed() -> None:
    """The provider yields a valid final, takes longer than `cleanup_s` to shut down, and then raises when it is cancelled."""

    class FailsLate:
        name = "fails-late"
        capabilities = CAPS

        async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
            try:
                yield final()
            finally:
                try:
                    await asyncio.Event().wait()
                except asyncio.CancelledError:
                    raise RuntimeError("shutdown failed") from None

    source = Source(loud_then_silent())

    async def call() -> ProviderError:
        try:
            await asyncio.wait_for(
                transcribe_utterance(FailsLate(), REQUEST, until_end_of_speech(source.stream(), EndpointDetector(VAD)),
                                     deadline_s=GUARD_S, cleanup_s=BOUND_S, on_partial=lambda _: None),
                timeout=GUARD_S)
        except ProviderError as error:
            return error
        raise AssertionError("the call returned a successful utterance despite the failed shutdown")

    assert run(call()).kind is ProviderFailure.UNAVAILABLE
    assert source.closed


@pytest.mark.parametrize("cancel_after_s", [0.0, 0.02])
def test_the_gate_resumes_a_close_that_a_cancelled_caller_left_unfinished(cancel_after_s: float) -> None:
    source = SlowSource(loud_then_silent())
    gate = until_end_of_speech(source.stream(), EndpointDetector(VAD))

    async def scenario() -> bool:
        first = asyncio.ensure_future(gate.aclose())
        await asyncio.sleep(cancel_after_s)
        first.cancel()
        await asyncio.gather(first, return_exceptions=True)
        await asyncio.wait_for(gate.aclose(), timeout=GUARD_S)  # the second close must wait for, or redo, the unfinished close
        return source.closed

    assert run(scenario())
