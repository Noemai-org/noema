"""The streaming voice pipeline: audio in, one final utterance out (VOI-02, REQ-VOI-001, REQ-AMB-007, EC-V04).

Partial transcripts exist for the screen. They go to a display callback and nowhere else. The only value this module
hands to the gateway is a FinalUtterance, and the only way to make one is from a FinalTranscript, so a partial cannot set
a slot by any path through this code (PERD §35.3).

Silence is a failure, not a result (INV-07): a stream that ends with no final, a final with no text, a provider error, a
provider that fails while closing and a deadline all raise ProviderError, and the caller fails over or offers text. Nothing
here retries or fails over (VOI-10). A display callback that raises is a bug in the caller, not a provider fault: it
propagates as it is (after the audio is released) and is never turned into a ProviderError.
Raw audio is never stored here; the audio iterator is consumed and closed, and the result carries none of it (AGENT-13).

The provider runs in its own task, so a deadline or a caller's cancellation never waits for the provider's own shutdown
work: a provider that blocks while it is being cancelled is abandoned after `cleanup_s`. It is given a lease on the audio,
not the audio source, and the lease is revoked at the end of the call, so an abandoned task cannot keep the microphone
reachable. Anything the provider has itself buffered is outside this module's control.

A provider that swallows its cancellation and carries on is told once more, in the only way left: when the call ends (or the
deadline passes) a `_Gate` is closed. After that no partial reaches the caller's display callback, and the abandoned task
returns and closes its stream at its next event, so a late partial can never land in another utterance or session
(Security Reviewer F1 on #193, AGENT-13).
"""

import asyncio
import logging
import weakref
from collections.abc import AsyncIterator, Callable
from dataclasses import dataclass
from typing import Any

from noema_model.providers.speech.base import (
    AudioChunk,
    FinalTranscript,
    Locale,
    SpeechEvent,
    SpeechProvider,
    StreamRequest,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.endpoint import AudioSource

logger = logging.getLogger(__name__)
READ_ABANDONED = "VOI_AUDIO_READ_ABANDONED"  # fixed codes only: nothing about the audio, the source or the error text is ever logged (AGENT-13)
DEFERRED_CLOSE_FAILED = "VOI_AUDIO_DEFERRED_CLOSE_FAILED"
CLOSE_ABANDONED = "VOI_AUDIO_CLOSE_ABANDONED"


@dataclass(frozen=True, slots=True)
class FinalUtterance:
    """What the gateway may build a canonical voice input from. A missing token confidence stays None; the slot rule
    (voice/confidence.py) counts it as low."""

    text: str
    token_confidences: tuple[float | None, ...]
    utterance_confidence: float | None
    provider_name: str
    model_version: str | None
    locale: Locale | None

    @classmethod
    def from_final(cls, provider_name: str, transcript: FinalTranscript) -> "FinalUtterance":
        if not isinstance(transcript, FinalTranscript):
            raise TypeError("a FinalUtterance is made only from a FinalTranscript")
        if not transcript.text.strip():
            raise ProviderError(ProviderFailure.MALFORMED)
        return cls(
            text=transcript.text,
            token_confidences=tuple(token.confidence for token in transcript.tokens),
            utterance_confidence=transcript.utterance_confidence,
            provider_name=provider_name,
            model_version=transcript.model_version_reported,
            locale=transcript.detected_locale,
        )


class _Gate:
    """Closed when the call ends or its deadline passes. Nothing is delivered to the caller after that."""

    def __init__(self) -> None:
        self.closed = False


class _AudioLease:
    """What the provider reads from. Every read runs as its own task that a cancelled provider cannot take down with it, and the lease
    remembers it, so the release can end an outstanding read before it closes the source (a generator cannot be closed while a read of
    it is running). Revoking the lease ends the stream and drops the only reference the provider holds."""

    def __init__(self, source: AudioSource) -> None:
        self._source: AudioSource | None = source
        self._pending: "asyncio.Future[AudioChunk] | None" = None

    def __aiter__(self) -> "_AudioLease":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._source is None:
            raise StopAsyncIteration
        read = asyncio.ensure_future(self._source.__anext__())
        read.add_done_callback(_mark_retrieved)
        self._pending = read
        try:
            return await asyncio.shield(read)
        finally:
            if self._pending is read and read.done():
                self._pending = None

    async def revoke(self, bound_s: float) -> "asyncio.Future[AudioChunk] | None":
        """End the lease and any read still running, giving the read `bound_s` to end. Returns the read if it is STILL running after that (a read that
        resists cancellation): the source cannot be closed while it runs, so the caller must close it when that read ends."""
        self._source = None
        read, self._pending = self._pending, None
        if read is not None and not read.done():
            read.cancel()
            await asyncio.wait({read}, timeout=bound_s)
            if not read.done():
                return read
        return None


_DEFERRED: set["asyncio.Future[None]"] = set()  # closes waiting for a read to end; held so they are not collected before they run


def _begin_close(audio: AudioSource, close_s: float) -> "asyncio.Future[None]":
    """Close the source in its own task, with a bound: a close that never finishes is cancelled after `close_s` and logged, so no deferred close is
    held for ever (Codex P2 on #474). A failed close is logged with a fixed code, never with the error text (it may carry data)."""
    closing = asyncio.ensure_future(audio.aclose())
    _DEFERRED.add(closing)

    def stalled() -> None:
        if not closing.done():
            logger.error(CLOSE_ABANDONED)
            closing.cancel()

    timer = asyncio.get_running_loop().call_later(close_s, stalled)

    def done(_: "asyncio.Future[None]") -> None:
        timer.cancel()
        _DEFERRED.discard(closing)
        if not closing.cancelled() and closing.exception() is not None:
            logger.error(DEFERRED_CLOSE_FAILED)

    closing.add_done_callback(done)
    return closing


def close_when_read_ends(read: "asyncio.Future[AudioChunk]", audio: AudioSource, hold_s: float, close_s: float | None = None) -> None:
    """A read that outlived the bounded wait keeps the source open for now. Close it as soon as that read ends.

    The wait has a second bound, `hold_s`. If the read is still running then, the pipeline stops waiting and does three things: it logs
    `VOI_AUDIO_READ_ABANDONED` once; it tries to close the source now, bounded by `close_s` (default `hold_s`), because closing is what interrupts a stuck read
    on a real source; and it leaves a callback on the read that holds the source only by a weak reference, so that if that close failed, the read later ends
    and the source is still alive, it is closed then. The pipeline therefore pins nothing for the life of the process, repeated calls do not accumulate,
    and a source is not left open just because its read was slow (Security Reviewer and Codex on #474).

    The limit: this bounds what the PIPELINE holds. A read that is really stuck (a socket, an executor thread, a timer) is held by the loop or the
    executor, not by this module, and still pins whatever it references until it ends.
    """
    window = hold_s if close_s is None else close_s
    timer: asyncio.TimerHandle | None = None

    def start(_: "asyncio.Future[Any]") -> None:
        if timer is not None:
            timer.cancel()
        _begin_close(audio, window)

    def abandon() -> None:
        if read.done():
            return  # it ended in time: start() has run or is queued, and nothing was abandoned
        read.remove_done_callback(start)
        logger.error(READ_ABANDONED)
        try:
            weak: "weakref.ReferenceType[AudioSource] | None" = weakref.ref(audio)
        except TypeError:
            weak = None  # a source that cannot be weakly referenced is closed now and not remembered
        attempt = _begin_close(audio, window)

        def late(_: "asyncio.Future[Any]") -> None:
            source = weak() if weak is not None else None
            if source is None:
                return
            if attempt.done() and (attempt.cancelled() or attempt.exception() is not None):
                _begin_close(source, window)  # the close at the abandon did not succeed; the read has ended, so try once more

        read.add_done_callback(late)

    timer = asyncio.get_running_loop().call_later(hold_s, abandon)
    read.add_done_callback(start)


async def transcribe_utterance(
    provider: SpeechProvider,
    request: StreamRequest,
    audio: AudioSource,
    *,
    deadline_s: float,
    on_partial: Callable[[str], None],
    cleanup_s: float | None = None,
    hold_s: float | None = None,
) -> FinalUtterance:
    """Stream `audio` to `provider` and return its final transcript. `deadline_s` bounds the whole call.

    Cleanup is bounded separately by `cleanup_s` (default: `deadline_s`), and a read that still runs after that is waited for `hold_s` more (default: ten times `cleanup_s`) and then abandoned and logged: a provider that stalls while closing, or while it
    is being cancelled, cannot hold the result back or keep the audio source open. The audio source is released whatever
    happens, including when the provider refuses before it returns a stream.
    """
    bound_s = deadline_s if cleanup_s is None else cleanup_s
    hold = 10 * bound_s if hold_s is None else hold_s  # how long a deferred close waits for a read that outlived the bound before it is abandoned
    gate = _Gate()
    lease = _AudioLease(audio)
    outcome: asyncio.Future[FinalUtterance] = asyncio.get_running_loop().create_future()
    outcome.add_done_callback(_mark_retrieved)
    task = asyncio.ensure_future(_consume(provider, request, lease, on_partial, outcome, gate))
    task.add_done_callback(_mark_retrieved)
    utterance: FinalUtterance | None = None
    error: BaseException | None = None
    try:
        watched: set[asyncio.Future[Any]] = {outcome, task}
        await asyncio.wait(watched, timeout=deadline_s, return_when=asyncio.FIRST_COMPLETED)
        if not outcome.done():
            raise ProviderError(ProviderFailure.TIMEOUT)
        utterance = outcome.result()  # a failure of the stream is raised here
        await asyncio.wait({task}, timeout=bound_s)  # the provider's own shutdown, bounded
    except BaseException as caught:  # held while the audio is released, then raised again unchanged
        error = caught
    failure = await _release(task, lease, audio, gate, bound_s, hold)
    if error is not None:
        raise error
    if failure is not None or utterance is None:
        raise ProviderError(ProviderFailure.UNAVAILABLE) from failure
    return utterance


async def _release(task: "asyncio.Future[None]", lease: _AudioLease, audio: AudioSource, gate: _Gate, bound_s: float, hold_s: float) -> BaseException | None:
    """Release everything the call holds, and return the shutdown failure it saw, if any. It survives a cancellation of the caller: the
    work is bounded (three waits of at most `bound_s`), so it is finished and the cancellation is raised afterwards."""
    work = asyncio.ensure_future(_release_work(task, lease, audio, gate, bound_s, hold_s))
    cancelled = False
    while not work.done():
        try:
            await asyncio.shield(work)
        except asyncio.CancelledError:
            cancelled = True  # keep waiting: the release is bounded and must not be skipped
    failure = work.result()
    if cancelled:
        raise asyncio.CancelledError
    return failure


async def _release_work(task: "asyncio.Future[None]", lease: _AudioLease, audio: AudioSource, gate: _Gate, bound_s: float, hold_s: float) -> BaseException | None:
    gate.closed = True
    if not task.done():
        task.cancel()
    running = await lease.revoke(bound_s)  # no read of the source may be running when it is closed
    failure: BaseException | None = None
    if running is not None:
        close_when_read_ends(running, audio, hold_s, bound_s)  # it cannot be closed while the read runs; it is closed when the read ends
        failure = TimeoutError("an audio read did not stop in time; the source is closed when it ends")
    else:
        closing = asyncio.ensure_future(audio.aclose())  # its own task: a source that never finishes closing is abandoned after `bound_s`
        closing.add_done_callback(_mark_retrieved)
        await asyncio.wait({closing}, timeout=bound_s)
        if not closing.done():
            failure = TimeoutError("the audio source did not finish closing")
        elif not closing.cancelled() and closing.exception() is not None:
            failure = closing.exception()
    if not task.done():
        await asyncio.wait({task}, timeout=bound_s)
    if task.done() and not task.cancelled() and task.exception() is not None:
        failure = failure or task.exception()  # a shutdown error seen after the bounded wait
    return failure


async def _consume(
    provider: SpeechProvider,
    request: StreamRequest,
    audio: AsyncIterator[AudioChunk],
    on_partial: Callable[[str], None],
    outcome: "asyncio.Future[FinalUtterance]",
    gate: _Gate,
) -> None:
    """Run the provider's stream. The outcome is set as soon as it is known, before the stream is closed, so a slow
    shutdown never delays the result."""
    stream: AsyncIterator[SpeechEvent] | None = None
    try:
        stream = provider.transcribe(request, audio)
        last_partial = -1
        async for event in stream:
            if gate.closed:
                return  # abandoned by the caller: deliver nothing, and let the finally close the stream
            if isinstance(event, FinalTranscript):
                outcome.set_result(FinalUtterance.from_final(provider.name, event))
                break
            if event.sequence <= last_partial:
                raise ProviderError(ProviderFailure.MALFORMED)
            last_partial = event.sequence
            if not gate.closed:
                on_partial(event.text)
        else:
            raise ProviderError(ProviderFailure.MALFORMED)  # the stream ended with no final
    except asyncio.CancelledError:
        raise
    except BaseException as error:
        if outcome.done():
            raise  # the outcome stands; a later failure is the provider's shutdown failing
        outcome.set_exception(error)
        return
    finally:
        close = getattr(stream, "aclose", None)
        if close is not None:
            await close()


def _mark_retrieved(task: "asyncio.Future[Any]") -> None:
    """A task nobody awaits again (an abandoned one, or one whose outcome already stands) must not log an unretrieved
    exception when it finally ends."""
    if not task.cancelled():
        task.exception()
