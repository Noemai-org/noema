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


# ---- Security Reviewer F1 on #193 at f36d0d5 (AGENT-13, REQ-VOI-004) and the conditions of the AGENT-12 exception ----------------------------------
#
# An abandoned provider that swallows its cancellation kept calling the display callback after the call had returned, so a late
# partial could land in the next utterance or in another session. The conditions of the exception granted on noema#412 say a
# fault test in this seam must assert that the audio is released and that no text is delivered after the call returns, not only
# that an exception was raised. The providers are scripted and SYNTHETIC; the time in them is real.

PERIOD_S = 0.02
SETTLE_S = 0.4  # many provider periods: a late delivery would show up here
MAX_LATE = 30  # 0.6 s of late partials at most


class Scripted:
    """One provider, many faults. `mode` picks the behaviour after an opening partial."""

    capabilities = CAPS

    def __init__(self, mode: str) -> None:
        self.name = f"scripted-{mode}"
        self._mode = mode
        self.stream_closed = False

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        try:
            yield PartialTranscript(0, "send")
            mode = self._mode
            if mode == "error":
                raise ProviderError(ProviderFailure.UNAVAILABLE)
            if mode == "no_final":
                return
            if mode == "backwards":
                yield PartialTranscript(0, "again")
                return
            if mode == "hang":
                await asyncio.Event().wait()
            if mode == "stall_on_cancel":
                try:
                    await asyncio.Event().wait()
                finally:
                    await asyncio.Event().wait()
            if mode == "swallows_cancellation":
                for sequence in range(1, MAX_LATE + 1):  # bounded, so an unfixed pipeline fails the test instead of hanging the loop
                    try:
                        await asyncio.sleep(PERIOD_S)
                    except asyncio.CancelledError:
                        pass  # the fault: cancellation is swallowed and the provider carries on
                    yield PartialTranscript(sequence, f"late {sequence}")
            if mode == "close_failure":
                try:
                    yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)
                finally:
                    raise RuntimeError("close failed")
            if mode == "stall_closing":
                try:
                    yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)
                finally:
                    await asyncio.Event().wait()
        finally:
            self.stream_closed = True


FAULT_FAMILIES = [
    ("error", ProviderFailure.UNAVAILABLE),
    ("no_final", ProviderFailure.MALFORMED),
    ("backwards", ProviderFailure.MALFORMED),
    ("hang", ProviderFailure.TIMEOUT),
    ("stall_on_cancel", ProviderFailure.TIMEOUT),
    ("swallows_cancellation", ProviderFailure.TIMEOUT),
    ("close_failure", ProviderFailure.UNAVAILABLE),
    ("stall_closing", None),  # a valid final is returned; the stalled shutdown is abandoned
]


@pytest.mark.parametrize("mode,expected", FAULT_FAMILIES)
def test_every_fault_family_releases_the_audio_and_delivers_no_text_after_the_call_returns(
        mode: str, expected: ProviderFailure | None) -> None:
    delivered: list[str] = []

    async def call() -> tuple[bool, int, int]:
        source = Source(loud_then_silent())
        provider = Scripted(mode)
        try:
            result = await asyncio.wait_for(
                transcribe_utterance(provider, REQUEST, gated(source), deadline_s=DEADLINE_S, cleanup_s=DEADLINE_S,
                                     on_partial=delivered.append),
                timeout=GUARD_S,
            )
            assert expected is None and result.text == "send ten"
        except ProviderError as error:
            assert error.kind is expected
        released = source.closed
        at_return = len(delivered)
        await asyncio.sleep(SETTLE_S)  # real time, during which an abandoned provider could still deliver
        return released, at_return, len(delivered)

    released, at_return, later = run(call())
    assert released
    assert later == at_return, f"{later - at_return} text(s) were delivered after the call returned"


def test_a_provider_that_swallows_cancellation_is_told_to_stop_and_ends_at_its_next_event() -> None:
    """SR's repro: the abandoned task must stop, not only stop delivering."""

    async def call() -> bool:
        provider = Scripted("swallows_cancellation")
        with pytest.raises(ProviderError):
            await asyncio.wait_for(
                transcribe_utterance(provider, REQUEST, gated(Source(loud_then_silent())), deadline_s=DEADLINE_S,
                                     cleanup_s=DEADLINE_S, on_partial=lambda _: None),
                timeout=GUARD_S,
            )
        await asyncio.sleep(SETTLE_S)
        return provider.stream_closed

    assert run(call())


# ---- Codex P1 on #193 at cdadcc8: the cleanup itself must survive a cancellation (AGENT-13, REQ-VOI-004) -------------------------------------------


def test_cancelling_the_caller_while_the_provider_shuts_down_still_releases_the_audio() -> None:
    """The caller is cancelled once (a client goes away), the provider's shutdown stalls, and the caller is cancelled again
    while the cleanup waits for it. The lease must still be revoked and the microphone closed."""
    source = Source(loud_then_silent())

    async def scenario() -> bool:
        seen_partial = asyncio.Event()
        call = asyncio.ensure_future(
            transcribe_utterance(StallsOnCancel(), REQUEST, gated(source), deadline_s=GUARD_S, on_partial=lambda _: seen_partial.set(),
                                 cleanup_s=GUARD_S))
        await asyncio.wait_for(seen_partial.wait(), timeout=GUARD_S)
        call.cancel()  # the first cancellation: the cleanup starts and waits on the provider's stalled shutdown
        await asyncio.sleep(0.05)
        assert not call.done()  # it really is waiting for the provider
        call.cancel()  # the second cancellation lands inside the cleanup's wait
        with pytest.raises(asyncio.CancelledError):
            await asyncio.wait_for(call, timeout=GUARD_S)
        return source.closed

    assert run(scenario())
