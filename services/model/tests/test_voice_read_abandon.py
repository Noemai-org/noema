"""A read that never ends is abandoned after a bound, loudly and without audio in the log (VOI-02, REQ-VOI-004, AGENT-13; Security Reviewer on #474 at 40f5cb04).

When the provider's read of the audio source resists cancellation past the cleanup bound, the pipeline cannot close the source while that read runs, so it
closes it when the read ends. SR's blocking finding: a read that NEVER ends kept the source and its buffer reachable from the pipeline for the life of the
process, with no second bound, and repeated calls accumulated. Now the pipeline holds the deferred close for one more bound, then drops every reference it
holds and logs a fixed code. What the abandoned read itself holds is that read's own business: once nothing else points at it, it is collected, and the
test proves it with weak references (SYNTHETIC scripted source; the AGENT-12 exception of noema#412 covers the SpeechProvider seam).
"""

import asyncio
import gc
import logging
import weakref
from collections.abc import AsyncIterator

import pytest

from noema_model.providers.speech.base import AudioChunk, FinalTranscript, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError
from noema_model.voice.pipeline import transcribe_utterance
from tests.support import run
from tests.support_voice import CAPS, REQUEST, Source

GUARD_S = 5.0
LOGGER = "noema_model.voice.pipeline"
ABANDONED = "VOI_AUDIO_READ_ABANDONED"
CLOSE_FAILED = "VOI_AUDIO_DEFERRED_CLOSE_FAILED"


class Buffer:
    """Stands for the chunk a source has buffered; weak-referenceable, unlike the slotted AudioChunk."""


class NeverEnding(Source):
    def __init__(self) -> None:
        super().__init__([])
        self.buffer = Buffer()

    async def __anext__(self) -> AudioChunk:
        while True:
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                continue  # resists cancellation for ever

    async def aclose(self) -> None:
        self.closed = True


class EndsLateThenFailsToClose(Source):
    def __init__(self) -> None:
        super().__init__([])

    async def __anext__(self) -> AudioChunk:
        try:
            await asyncio.Event().wait()
        except asyncio.CancelledError:
            await asyncio.sleep(0.2)  # resists for a while, then ends
        raise StopAsyncIteration

    async def aclose(self) -> None:
        raise RuntimeError("secret-marker-that-must-not-be-logged")


class Reader:
    name = "reader"
    capabilities = CAPS

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        async for _ in audio:
            pass
        yield FinalTranscript("send ten", (TokenConfidence("send", 0.9),), 0.9, None, None)


def test_a_read_that_never_ends_is_abandoned_after_the_hold_and_the_source_and_its_buffer_are_freed(caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level(logging.ERROR, logger=LOGGER)

    async def scenario() -> tuple[bool, bool]:
        source = NeverEnding()
        refs = weakref.ref(source), weakref.ref(source.buffer)
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=0.1, on_partial=lambda _: None)
        del source
        await asyncio.sleep(0.3)  # past the hold, which is one more cleanup bound
        gc.collect()
        gc.collect()
        return refs[0]() is None, refs[1]() is None

    source_freed, buffer_freed = run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert source_freed and buffer_freed
    assert [r.getMessage() for r in caplog.records if r.name == LOGGER] == [ABANDONED]  # one fixed code, no audio, no repr


def test_repeated_calls_with_never_ending_reads_do_not_accumulate_references() -> None:
    async def scenario() -> list[bool]:
        refs = []
        for _ in range(5):
            source = NeverEnding()
            refs.append(weakref.ref(source))
            with pytest.raises(ProviderError):
                await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.02, cleanup_s=0.02, hold_s=0.05, on_partial=lambda _: None)
            del source
        await asyncio.sleep(0.2)
        gc.collect()
        gc.collect()
        return [r() is None for r in refs]

    assert all(run(asyncio.wait_for(scenario(), timeout=GUARD_S)))


def test_a_deferred_close_that_fails_is_logged_with_a_fixed_code_and_never_with_the_error_text(caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level(logging.ERROR, logger=LOGGER)
    source = EndsLateThenFailsToClose()

    async def scenario() -> None:
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.1, hold_s=2.0, on_partial=lambda _: None)
        await asyncio.sleep(0.6)  # the read ends 0.2 s after it was cancelled; the deferred close then fails

    run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    messages = [r.getMessage() for r in caplog.records if r.name == LOGGER]
    assert messages == [CLOSE_FAILED]
    assert all("secret-marker" not in r.getMessage() and not r.exc_info for r in caplog.records)


# ---- Security Reviewer and Codex P2 on #474 at c8eff438: abandoning the wait must not leave the source open -------------------------------------------


CLOSE_ABANDONED = "VOI_AUDIO_CLOSE_ABANDONED"


class EndsLateAndRefusesCloseWhileReading(Source):
    """A generator-like source: its read ends 0.4 s after it is cancelled, and it cannot be closed while a read runs."""

    def __init__(self) -> None:
        super().__init__([])
        self.reading = False

    async def __anext__(self) -> AudioChunk:
        self.reading = True
        try:
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                await asyncio.sleep(0.4)
            raise StopAsyncIteration
        finally:
            self.reading = False

    async def aclose(self) -> None:
        if self.reading:
            raise RuntimeError("aclose(): asynchronous generator is already running")
        self.closed = True


class NeverEndingAndNeverFinishesClosing(NeverEnding):
    async def aclose(self) -> None:
        await asyncio.Event().wait()


def test_an_abandoned_read_gets_the_source_closed_at_the_abandon_and_nothing_stays_pinned() -> None:
    async def scenario() -> tuple[bool, bool]:
        source = NeverEnding()
        ref = weakref.ref(source)
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=0.1, on_partial=lambda _: None)
        await asyncio.sleep(0.3)  # past the hold: the abandon has happened
        closed = source.closed
        del source
        gc.collect()
        gc.collect()
        return closed, ref() is None

    closed, freed = run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert closed  # a source that is never closed is worse than a stuck read: the abandon closes it, it does not just forget it
    assert freed


def test_a_read_that_ends_after_the_hold_still_gets_the_source_closed_without_the_pipeline_pinning_it(caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level(logging.ERROR, logger=LOGGER)
    source = EndsLateAndRefusesCloseWhileReading()

    async def scenario() -> tuple[bool, bool]:
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=0.1, on_partial=lambda _: None)
        closed_at_return = source.closed
        await asyncio.sleep(0.8)  # the read ends 0.4 s after it was cancelled, which is after the hold; the source is closed then
        return closed_at_return, source.closed

    closed_at_return, closed_later = run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert not closed_at_return
    assert closed_later  # eventual closure is kept after the abandon (Codex P2 at c8eff438)
    messages = [r.getMessage() for r in caplog.records if r.name == LOGGER]
    assert ABANDONED in messages
    assert set(messages) <= {ABANDONED, CLOSE_FAILED}  # fixed codes only


def test_a_close_that_never_finishes_is_abandoned_after_a_bound_logged_and_the_source_is_freed(caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level(logging.ERROR, logger=LOGGER)

    async def scenario() -> bool:
        source = NeverEndingAndNeverFinishesClosing()
        ref = weakref.ref(source)
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=0.05, on_partial=lambda _: None)
        del source
        await asyncio.sleep(0.5)  # the hold, then one close bound
        gc.collect()
        gc.collect()
        return ref() is None

    assert run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert [r.getMessage() for r in caplog.records if r.name == LOGGER] == [ABANDONED, CLOSE_ABANDONED]  # Codex P2 on c97980da: no deferred close is held for ever


def test_a_read_that_ends_in_time_is_closed_once_and_nothing_is_logged(caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level(logging.ERROR, logger=LOGGER)
    source = EndsLateAndRefusesCloseWhileReading()

    async def scenario() -> None:
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=5.0, on_partial=lambda _: None)
        await asyncio.sleep(0.8)

    run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert source.closed
    assert [r for r in caplog.records if r.name == LOGGER] == []


# ---- Codex P2 on #474 at 642e9218: a read that ends while the first close is still pending must still get a retry if that close then fails ------------------


class FirstCloseStallsSecondWorks(Source):
    """The read resists cancellation for 1 s. The first close (at the abandon) stalls and is cancelled at its bound; a second close works."""

    def __init__(self) -> None:
        super().__init__([])
        self.calls = 0

    async def __anext__(self) -> AudioChunk:
        try:
            await asyncio.Event().wait()
        except asyncio.CancelledError:
            await asyncio.sleep(1.0)
        raise StopAsyncIteration

    async def aclose(self) -> None:
        self.calls += 1
        if self.calls == 1:
            await asyncio.Event().wait()
        self.closed = True


class FirstCloseFailsLateSecondWorks(FirstCloseStallsSecondWorks):
    async def aclose(self) -> None:
        self.calls += 1
        if self.calls == 1:
            await asyncio.sleep(0.8)  # still pending when the read ends, then fails
            raise RuntimeError("first close failed")
        self.closed = True


@pytest.mark.parametrize("source_type", [FirstCloseStallsSecondWorks, FirstCloseFailsLateSecondWorks])
def test_a_first_close_that_fails_after_the_read_ended_is_retried(source_type: type[FirstCloseStallsSecondWorks]) -> None:
    source = source_type()

    async def scenario() -> None:
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.5, hold_s=0.1, on_partial=lambda _: None)
        await asyncio.sleep(2.5)  # the read ends at about 1.05 s, while the first close is pending; the first close then fails or is cancelled

    run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert source.closed  # the retry ran although the read had ended before the first close finished
    assert source.calls == 2  # one retry, not a loop



# ---- Codex P2 on #474 at a6d5de2a: a source that cannot be weakly referenced, and a close that suppresses its cancellation ------------------------------------


class Slotted:
    """A source that cannot be weakly referenced (`__slots__` with no `__weakref__`). Its read resists cancellation for 0.4 s; it cannot be closed while a read runs."""

    __slots__ = ("reading", "closed", "calls")

    def __init__(self) -> None:
        self.reading = False
        self.closed = False
        self.calls = 0

    def __aiter__(self) -> "Slotted":
        return self

    async def __anext__(self) -> AudioChunk:
        self.reading = True
        try:
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                await asyncio.sleep(0.4)
            raise StopAsyncIteration
        finally:
            self.reading = False

    async def aclose(self) -> None:
        self.calls += 1
        if self.reading:
            raise RuntimeError("aclose(): asynchronous generator is already running")
        self.closed = True


class SuppressesCancellationOnClose(EndsLateAndRefusesCloseWhileReading):
    """Its read ends 0.4 s after it is cancelled; its close catches CancelledError and carries on: cancelling it does not stop it. The close ends by itself after 1 s,
    so a test that fails cannot hang the loop."""

    async def aclose(self) -> None:
        loop = asyncio.get_running_loop()
        end = loop.time() + 1.0
        while loop.time() < end:
            try:
                await asyncio.sleep(0.05)
            except asyncio.CancelledError:
                continue


def test_a_source_that_cannot_be_weakly_referenced_still_gets_its_retry_after_the_read_ends() -> None:
    source = Slotted()
    with pytest.raises(TypeError):
        weakref.ref(source)  # the precondition of the case

    async def scenario() -> None:
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=0.1, on_partial=lambda _: None)
        await asyncio.sleep(1.0)  # the abandon at ~0.15 s closes too early (the read is running); the read ends at ~0.45 s

    run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert source.closed  # Codex P2: with no weak reference the retry had no source to close, and the microphone stayed open
    assert source.calls == 2  # one retry, not a loop


def test_a_close_that_suppresses_cancellation_is_not_held_by_the_pipeline_for_ever(caplog: pytest.LogCaptureFixture) -> None:
    from noema_model.voice import pipeline

    caplog.set_level(logging.ERROR, logger=LOGGER)
    before = set(pipeline._DEFERRED)

    async def scenario() -> set[asyncio.Future[None]]:
        with pytest.raises(ProviderError):
            await transcribe_utterance(Reader(), REQUEST, SuppressesCancellationOnClose(), deadline_s=0.05, cleanup_s=0.05, hold_s=0.05, on_partial=lambda _: None)
        await asyncio.sleep(0.5)  # past the hold and one close bound: the close was cancelled and ignored it, and is still running
        held = set(pipeline._DEFERRED) - before
        gc.collect()
        gc.collect()
        await asyncio.sleep(2.0)  # let the closes end by themselves so the loop can be torn down
        return held

    held = run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    assert held == set()  # the pipeline lets go of a close that will not stop; it does not hold it, and its source, for the life of the process
    assert [r.getMessage() for r in caplog.records if r.name == LOGGER].count(CLOSE_ABANDONED_CODE) >= 1


CLOSE_ABANDONED_CODE = "VOI_AUDIO_CLOSE_ABANDONED"
