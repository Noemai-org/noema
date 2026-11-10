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
    """What the provider reads from. Revoking it ends the stream and drops the only reference the provider holds."""

    def __init__(self, source: AudioSource) -> None:
        self._source: AudioSource | None = source

    def __aiter__(self) -> "_AudioLease":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._source is None:
            raise StopAsyncIteration
        return await self._source.__anext__()

    def revoke(self) -> None:
        self._source = None


async def transcribe_utterance(
    provider: SpeechProvider,
    request: StreamRequest,
    audio: AudioSource,
    *,
    deadline_s: float,
    on_partial: Callable[[str], None],
    cleanup_s: float | None = None,
) -> FinalUtterance:
    """Stream `audio` to `provider` and return its final transcript. `deadline_s` bounds the whole call.

    Cleanup is bounded separately by `cleanup_s` (default: `deadline_s`): a provider that stalls while closing, or while it
    is being cancelled, cannot hold the result back or keep the audio source open. The audio source is released whatever
    happens, including when the provider refuses before it returns a stream.
    """
    bound_s = deadline_s if cleanup_s is None else cleanup_s
    gate = _Gate()
    lease = _AudioLease(audio)
    outcome: asyncio.Future[FinalUtterance] = asyncio.get_running_loop().create_future()
    outcome.add_done_callback(_mark_retrieved)
    task = asyncio.ensure_future(_consume(provider, request, lease, on_partial, outcome, gate))
    task.add_done_callback(_mark_retrieved)
    try:
        watched: set[asyncio.Future[Any]] = {outcome, task}
        await asyncio.wait(watched, timeout=deadline_s, return_when=asyncio.FIRST_COMPLETED)
        if not outcome.done():
            gate.closed = True  # the deadline has passed: nothing more may reach the caller
            await _stop(task, bound_s)
            raise ProviderError(ProviderFailure.TIMEOUT)
        utterance = outcome.result()  # a failure of the stream is raised here
        await asyncio.wait({task}, timeout=bound_s)  # the provider's own shutdown, bounded
        if task.done() and not task.cancelled() and task.exception() is not None:
            raise ProviderError(ProviderFailure.UNAVAILABLE) from task.exception()
        if not task.done():
            await _stop(task, bound_s)
        return utterance
    finally:
        gate.closed = True
        await _stop(task, bound_s)
        lease.revoke()
        await audio.aclose()


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


async def _stop(task: "asyncio.Future[None]", bound_s: float) -> None:
    """Cancel `task` and give it `bound_s` to finish. One that does not is abandoned, never awaited further."""
    if task.done():
        return
    task.cancel()
    await asyncio.wait({task}, timeout=bound_s)


def _mark_retrieved(task: "asyncio.Future[Any]") -> None:
    """A task nobody awaits again (an abandoned one, or one whose outcome already stands) must not log an unretrieved
    exception when it finally ends."""
    if not task.cancelled():
        task.exception()
