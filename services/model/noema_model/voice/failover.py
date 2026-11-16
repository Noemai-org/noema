"""Provider failover inside one utterance (VOI-10, REQ-VOI-002, PERD §35.3: "ASR timeout: fail over to the next speech
provider; then offer text").

The audio of the utterance is kept in memory only for the duration of the call (REQ-VOI-004). When a provider fails, the next
one is given the whole utterance from its first chunk, then the live remainder, so no provider ever transcribes half of it.
If the utterance has outgrown the replay buffer, failover is impossible and the person is offered text instead: a provider
that heard only the end of a sentence could settle a different amount.

Only a provider that is qualified and whose data terms are confirmed ever hears the audio (REQ-INT-006, PERD R-18, A180): one whose capabilities say `qualified` is
false, or whose `retention_terms` is not YES, is skipped before it is called, the skip is listed in the result (`FailoverStep.skipped`) so the caller can record it, and
a call with no eligible provider offers text. The microphone is released either way.

The live source is released within `cleanup_s`: a read that resists cancellation is closed over when it ends, a close that never finishes is
abandoned, and either way the result has no utterance, offers text and sets `release_failed` (a microphone still open is never a success).
A read that completes as its provider is cancelled stays for the next reader, so no chunk is lost (Codex on #218). The source must support weak references (see
`AudioSource`); one that does not is refused with `TypeError`, and still closed.

Every ProviderError fails over, including a stream that ends with no final, because a partial is never a result. No provider
is tried twice. Each failure is listed in the result so the caller can record `PROVIDER_FAILOVER`; nothing here writes the
Record.
"""

import asyncio
from collections.abc import AsyncGenerator, Callable, Sequence
from dataclasses import dataclass

from noema_model.platform.telemetry import Telemetry
from noema_model.providers.speech.base import AudioChunk, SpeechProvider, StreamRequest, Support
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.endpoint import AudioSource
from noema_model.voice.pipeline import (
    FinalUtterance,
    Report,
    begin_close,
    cleanup_reporter,
    close_when_read_ends,
    refuse_unweakable,
    transcribe_utterance,
)


@dataclass(frozen=True, slots=True)
class FailoverStep:
    provider_name: str
    failure: ProviderFailure
    skipped: bool = False  # True when the provider was not tried at all: it is not qualified, or its data terms are not confirmed (it never heard a chunk)


@dataclass(frozen=True, slots=True)
class FailoverResult:
    utterance: FinalUtterance | None
    steps: tuple[FailoverStep, ...]  # one per provider that was tried and failed, or skipped, in order
    offer_text: bool
    replay_overflow: bool  # True when a provider was left untried because the utterance no longer fit the buffer
    release_failed: bool = False  # True when the live source could not be released in time; then there is no utterance, only the offer of text


def eligible(provider: SpeechProvider) -> bool:
    """May this provider hear the audio? Qualified, and its data terms confirmed (REQ-INT-006, PERD R-18). An unverified or refused term is never a yes."""
    capabilities = provider.capabilities
    return capabilities.qualified is True and capabilities.retention_terms is Support.YES


class _ReplayBuffer:
    def __init__(self, source: AudioSource, limit: int) -> None:
        self._source = source
        self._limit = limit
        self._chunks: list[AudioChunk] = []
        self._exhausted = False
        self._pending: asyncio.Future[AudioChunk] | None = None
        self.overflowed = False

    async def stream(self) -> AsyncGenerator[AudioChunk, None]:
        """The buffered chunks, then the live ones. Only one reader at a time."""
        index = 0
        while True:
            if index < len(self._chunks):
                index += 1
                yield self._chunks[index - 1]
                continue
            if self._exhausted:
                return
            try:
                item = await self._next()
            except StopAsyncIteration:
                self._exhausted = True
                return
            if len(self._chunks) >= self._limit:
                self.overflowed = True  # this chunk is delivered now but cannot be replayed
            else:
                self._chunks.append(item)
                index += 1
            yield item

    async def _next(self) -> AudioChunk:
        """The next live chunk. The read runs as its own task and is shielded, so a provider's deadline cancels only its wait,
        never the read: cancelling a read inside an async generator would end the generator, and the next provider would
        hear a truncated utterance as a complete one (Codex P2 on #218). The read is forgotten only once its result has been
        handed on: a provider cancelled in the very turn the read completed leaves it for the next reader, so no chunk is lost."""
        if self._pending is None:
            self._pending = asyncio.ensure_future(self._source.__anext__())
        pending = self._pending
        try:
            item = await asyncio.shield(pending)
        except asyncio.CancelledError:
            raise  # the read, finished or not, stays for the next reader
        except BaseException:
            self._pending = None  # the read ended with its end or its error, which this reader receives
            raise
        self._pending = None
        return item

    async def close(self, bound_s: float, hold_s: float, report: Report) -> bool:
        """Release the live source within the bounds. Returns True when it could not be released in time: a read that resists cancellation is closed over
        when it ends, after a second bound (the source cannot be closed while it runs), and a close that does not finish is cancelled at its bound, reported and
        let go of, the same way `transcribe_utterance` does it (Codex P1 on #218; Codex on #474)."""
        pending, self._pending = self._pending, None
        if pending is not None and not pending.done():
            pending.cancel()
            await asyncio.wait({pending}, timeout=bound_s)
            if not pending.done():
                close_when_read_ends(pending, self._source, hold_s, bound_s, report)
                return True
        closing = begin_close(self._source, bound_s, report)
        await asyncio.wait({closing}, timeout=bound_s)
        if not closing.done():
            return True
        return closing.cancelled() or closing.exception() is not None  # cancelled at its bound means it did not finish


async def transcribe_with_failover(
    providers: Sequence[SpeechProvider],
    request: StreamRequest,
    audio: AudioSource,
    *,
    deadline_s: float,
    on_partial: Callable[[str], None],
    max_buffered_chunks: int,
    cleanup_s: float | None = None,
    hold_s: float | None = None,
    telemetry: Telemetry | None = None,
    correlation_id: str = "uncorrelated",
) -> FailoverResult:
    """Try `providers` in order until one settles a final transcript. `deadline_s` bounds each provider separately and `cleanup_s` (default: `deadline_s`)
    bounds each cleanup, including the release of the live source. If the source cannot be released in time the result has no utterance and offers text:
    a microphone that is still open is never a success (INV-07, AGENT-13). A provider that is not eligible (see `eligible`) is skipped untouched. Cleanup that
    happens after the call has returned is reported through `telemetry` on `correlation_id`, as for `transcribe_utterance`."""
    if not providers:
        raise ValueError("no speech providers configured")
    if max_buffered_chunks < 1:
        raise ValueError("the replay buffer must hold at least one chunk")
    bound_s = deadline_s if cleanup_s is None else cleanup_s
    hold = 10 * bound_s if hold_s is None else hold_s
    await refuse_unweakable(audio, bound_s)
    report = cleanup_reporter(telemetry, correlation_id)
    buffer = _ReplayBuffer(audio, max_buffered_chunks)
    steps: list[FailoverStep] = []
    result: FailoverResult | None = None
    cancelled = False
    tried = 0
    try:
        for provider in providers:
            if not eligible(provider):
                steps.append(FailoverStep(provider.name, ProviderFailure.REJECTED, skipped=True))
                continue
            if tried > 0 and buffer.overflowed:
                result = FailoverResult(None, tuple(steps), True, True)
                break
            tried += 1
            try:
                utterance = await transcribe_utterance(
                    provider, request, buffer.stream(), deadline_s=deadline_s, on_partial=on_partial, cleanup_s=bound_s,
                    hold_s=hold, telemetry=telemetry, correlation_id=correlation_id,
                )
            except ProviderError as error:
                steps.append(FailoverStep(provider.name, error.kind))
                continue
            result = FailoverResult(utterance, tuple(steps), False, False)
            break
        else:
            result = FailoverResult(None, tuple(steps), True, False)
    finally:
        # Reached on a return and on an error alike; the caller's cancellation does not skip it. Bounded, so it never holds the call.
        release = asyncio.ensure_future(buffer.close(bound_s, hold, report))
        while not release.done():
            try:
                await asyncio.shield(release)
            except asyncio.CancelledError:
                cancelled = True  # keep waiting: the release is bounded and must not be skipped; the cancellation is re-raised below
        failed = release.result()
        if cancelled:
            raise asyncio.CancelledError
    assert result is not None
    if failed:
        return FailoverResult(None, result.steps, True, result.replay_overflow, release_failed=True)
    return result

