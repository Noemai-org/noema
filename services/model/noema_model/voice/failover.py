"""Provider failover inside one utterance (VOI-10, REQ-VOI-002, PERD §35.3: "ASR timeout: fail over to the next speech
provider; then offer text").

The audio of the utterance is kept in memory only for the duration of the call (REQ-VOI-004). When a provider fails, the next
one is given the whole utterance from its first chunk, then the live remainder, so no provider ever transcribes half of it.
If the utterance has outgrown the replay buffer, failover is impossible and the person is offered text instead: a provider
that heard only the end of a sentence could settle a different amount.

Every ProviderError fails over, including a stream that ends with no final, because a partial is never a result. No provider
is tried twice. Each failure is listed in the result so the caller can record `PROVIDER_FAILOVER`; nothing here writes the
Record.
"""

from collections.abc import AsyncGenerator, Callable, Sequence
from dataclasses import dataclass

from noema_model.providers.speech.base import AudioChunk, SpeechProvider, StreamRequest
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.pipeline import FinalUtterance, transcribe_utterance


@dataclass(frozen=True, slots=True)
class FailoverStep:
    provider_name: str
    failure: ProviderFailure


@dataclass(frozen=True, slots=True)
class FailoverResult:
    utterance: FinalUtterance | None
    steps: tuple[FailoverStep, ...]  # one per provider that was tried and failed, in order
    offer_text: bool
    replay_overflow: bool  # True when a provider was left untried because the utterance no longer fit the buffer


class _ReplayBuffer:
    def __init__(self, source: AsyncGenerator[AudioChunk, None], limit: int) -> None:
        self._source = source
        self._limit = limit
        self._chunks: list[AudioChunk] = []
        self._exhausted = False
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
                item = await self._source.__anext__()
            except StopAsyncIteration:
                self._exhausted = True
                return
            if len(self._chunks) >= self._limit:
                self.overflowed = True  # this chunk is delivered now but cannot be replayed
            else:
                self._chunks.append(item)
                index += 1
            yield item

    async def close(self) -> None:
        await self._source.aclose()


async def transcribe_with_failover(
    providers: Sequence[SpeechProvider],
    request: StreamRequest,
    audio: AsyncGenerator[AudioChunk, None],
    *,
    deadline_s: float,
    on_partial: Callable[[str], None],
    max_buffered_chunks: int,
) -> FailoverResult:
    """Try `providers` in order until one settles a final transcript. `deadline_s` bounds each provider separately."""
    if not providers:
        raise ValueError("no speech providers configured")
    if max_buffered_chunks < 1:
        raise ValueError("the replay buffer must hold at least one chunk")
    buffer = _ReplayBuffer(audio, max_buffered_chunks)
    steps: list[FailoverStep] = []
    try:
        for attempt, provider in enumerate(providers):
            if attempt > 0 and buffer.overflowed:
                return FailoverResult(None, tuple(steps), True, True)
            try:
                utterance = await transcribe_utterance(
                    provider, request, buffer.stream(), deadline_s=deadline_s, on_partial=on_partial
                )
            except ProviderError as error:
                steps.append(FailoverStep(provider.name, error.kind))
                continue
            return FailoverResult(utterance, tuple(steps), False, False)
        return FailoverResult(None, tuple(steps), True, False)
    finally:
        await buffer.close()
