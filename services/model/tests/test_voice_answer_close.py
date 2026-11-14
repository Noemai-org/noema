"""The synthesis iterator is closed on every exit path (VOI-04, REQ-VOI-002; Codex P2 on #213 at cf3f197).

A synthesizer may return a closeable custom async iterator rather than an async generator. `async for` gives no cleanup
guarantee for it, so a timeout, a cancellation or an error could leave its transport open even though the text fallback was
delivered. The iterator is retained and closed within a bound, so a stalled close cannot delay the text either.
"""

import asyncio

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    Locale,
    PipelineMode,
    ProviderCapabilities,
    Support,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.tts import SynthesisRequest
from noema_model.voice.answer import DeliveryMode, deliver_answer
from tests.support import run

GUARD_S = 5.0  # a hang is a test failure, not a stuck CI job
CAPS = ProviderCapabilities("fake", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), tuple((loc, Support.YES) for loc in Locale),
                            None, Support.YES, "v1", True)
REQUEST = SynthesisRequest(text="We found your transfer.", locale=Locale.PCM, model_version="v1")


def chunk(i: int) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, b"\x01\x00" * 160)


class Stream:
    """A closeable custom async iterator (not an async generator): nothing closes it unless the caller calls aclose()."""

    def __init__(self, chunks: int = 0, then: str = "end", close_hangs: bool = False) -> None:
        self._left, self._then, self._close_hangs = chunks, then, close_hangs
        self.closed = False
        self._n = 0

    def __aiter__(self) -> "Stream":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._left > 0:
            self._left -= 1
            self._n += 1
            return chunk(self._n)
        if self._then == "hang":
            await asyncio.Event().wait()
        if self._then == "error":
            raise ProviderError(ProviderFailure.UNAVAILABLE)
        raise StopAsyncIteration

    async def aclose(self) -> None:
        self.closed = True
        if self._close_hangs:
            await asyncio.Event().wait()


class Synth:
    name = "fake"
    capabilities = CAPS

    def __init__(self, stream: Stream) -> None:
        self.stream = stream

    def synthesize(self, request: SynthesisRequest) -> Stream:
        return self.stream


def deliver(stream: Stream, deadline_s: float = 0.05):  # type: ignore[no-untyped-def]
    return run(asyncio.wait_for(deliver_answer(Synth(stream), REQUEST, deadline_s=deadline_s), timeout=GUARD_S))


def test_a_timeout_closes_the_custom_iterator_and_still_delivers_the_text() -> None:
    stream = Stream(chunks=1, then="hang")
    result = deliver(stream)
    assert result.mode is DeliveryMode.TEXT_ONLY and result.failure is ProviderFailure.TIMEOUT and result.text == REQUEST.text
    assert stream.closed


def test_a_provider_error_closes_the_custom_iterator() -> None:
    stream = Stream(chunks=1, then="error")
    result = deliver(stream)
    assert result.failure is ProviderFailure.UNAVAILABLE
    assert stream.closed


def test_a_completed_synthesis_closes_the_custom_iterator() -> None:
    stream = Stream(chunks=2)
    result = deliver(stream)
    assert result.mode is DeliveryMode.SPEAK and len(result.audio) == 2
    assert stream.closed


def test_a_caller_cancellation_closes_the_custom_iterator() -> None:
    stream = Stream(chunks=1, then="hang")

    async def cancelled() -> None:
        task = asyncio.ensure_future(deliver_answer(Synth(stream), REQUEST, deadline_s=30.0))
        await asyncio.sleep(0.05)  # let the synthesis start and block
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        await asyncio.sleep(0.2)  # the bounded cleanup runs as its own step

    run(asyncio.wait_for(cancelled(), timeout=GUARD_S))
    assert stream.closed


def test_a_close_that_never_returns_cannot_delay_the_text_fallback() -> None:
    stream = Stream(chunks=1, then="hang", close_hangs=True)
    result = deliver(stream)
    assert result.mode is DeliveryMode.TEXT_ONLY and result.failure is ProviderFailure.TIMEOUT
    assert stream.closed


# ---- Codex P2 on #213 at f7e1fd7: a TimeoutError raised by the iterator's own close is the provider's failure, not the cleanup bound expiring ---------------


class TimeoutOnClose(Stream):
    """Yields valid audio, then raises TimeoutError from its own aclose() (the provider's shutdown timing out, not our bound)."""

    async def aclose(self) -> None:
        self.closed = True
        raise TimeoutError("the provider's shutdown timed out")


class OtherErrorOnClose(Stream):
    async def aclose(self) -> None:
        self.closed = True
        raise RuntimeError("the provider's shutdown failed")


def test_a_timeout_error_raised_by_the_close_itself_is_reported_not_discarded() -> None:
    result = deliver(TimeoutOnClose(chunks=2), deadline_s=5.0)
    assert result.mode is DeliveryMode.TEXT_ONLY and result.text == REQUEST.text and result.audio == ()
    assert result.failure is ProviderFailure.UNAVAILABLE  # the shutdown failure is recorded, as for any other exception from aclose()
    assert result.error_type == "TimeoutError"


def test_any_other_error_raised_by_the_close_is_still_reported() -> None:
    result = deliver(OtherErrorOnClose(chunks=2), deadline_s=5.0)
    assert result.failure is ProviderFailure.UNAVAILABLE and result.error_type == "RuntimeError"


def test_a_close_that_never_returns_is_still_abandoned_after_the_bound_and_the_audio_is_kept() -> None:
    stream = Stream(chunks=2, close_hangs=True)
    result = run(asyncio.wait_for(deliver_answer(Synth(stream), REQUEST, deadline_s=5.0, cleanup_s=0.05), timeout=GUARD_S))
    assert stream.closed
    assert result.mode is DeliveryMode.SPEAK and len(result.audio) == 2 and result.failure is None  # the bound expiring is not a provider failure
