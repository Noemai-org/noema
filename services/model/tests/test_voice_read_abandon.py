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
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, on_partial=lambda _: None)
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
                await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.02, cleanup_s=0.02, on_partial=lambda _: None)
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
            await transcribe_utterance(Reader(), REQUEST, source, deadline_s=0.05, cleanup_s=0.1, on_partial=lambda _: None)
        await asyncio.sleep(0.6)  # the read ends 0.2 s after it was cancelled; the deferred close then fails

    run(asyncio.wait_for(scenario(), timeout=GUARD_S))
    messages = [r.getMessage() for r in caplog.records if r.name == LOGGER]
    assert messages == [CLOSE_FAILED]
    assert all("secret-marker" not in r.getMessage() and not r.exc_info for r in caplog.records)
