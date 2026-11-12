"""The replay buffer's release is bounded, keeps a completed read, and never turns a failed release into a success (VOI-10, REQ-VOI-002, REQ-VOI-004, INV-07, AGENT-13).

Codex on #218: (P1) closing the live source after the last provider waited without a bound for a read that ignores cancellation or for the source's own `aclose()`;
(P2) a provider cancelled in the same turn its read completed lost that chunk, so the next provider heard a truncated utterance. Every stall here is a real asyncio wait.
The providers and sources are scripted stand-ins for a vendor (SYNTHETIC); their use is the recorded AGENT-12 exception on noema#412 (TST-04 audit row 4), which covers the
SpeechProvider stream seam. The first real adapter brings the hostility-proxy tests (DEBT-14).
"""

import asyncio

from noema_model.providers.speech.base import AudioChunk
from noema_model.voice.failover import FailoverResult, _ReplayBuffer, transcribe_with_failover
from tests.support import run
from tests.test_voice_failover import REQUEST, Fake, chunk, final

GUARD_S = 5.0


class StallingClose:
    """A live microphone: two chunks, then end; its `aclose()` never finishes until the test lets it."""

    def __init__(self) -> None:
        self.let_go = asyncio.Event()
        self.closed = False
        self._i = 0

    def __aiter__(self) -> "StallingClose":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._i >= 2:
            raise StopAsyncIteration
        self._i += 1
        return chunk(self._i - 1)

    async def aclose(self) -> None:
        await self.let_go.wait()
        self.closed = True


def test_a_source_whose_close_never_returns_cannot_hold_the_call_and_the_person_is_offered_text() -> None:
    source = StallingClose()

    async def call() -> FailoverResult | None:
        pending = asyncio.ensure_future(transcribe_with_failover(
            [Fake("a", 1, [final()])], REQUEST, source,  # type: ignore[arg-type]
            deadline_s=0.5, cleanup_s=0.1, on_partial=lambda _: None, max_buffered_chunks=10))
        try:
            done, _ = await asyncio.wait({pending}, timeout=GUARD_S)
            return pending.result() if done else None
        finally:
            source.let_go.set()
            await asyncio.gather(pending, return_exceptions=True)

    result = run(call())
    assert result is not None, "the call waited for the source's close"
    assert result.utterance is None and result.offer_text and result.release_failed  # a microphone that was not released is not a success


class ResistantRead:
    """A live microphone whose read ignores cancellation for `resist_s`, and which cannot be closed while a read is running."""

    def __init__(self, resist_s: float) -> None:
        self.resist_s = resist_s
        self.reading = False
        self.closed = False

    def __aiter__(self) -> "ResistantRead":
        return self

    async def __anext__(self) -> AudioChunk:
        self.reading = True
        try:
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                await asyncio.sleep(self.resist_s)
            raise StopAsyncIteration
        finally:
            self.reading = False

    async def aclose(self) -> None:
        if self.reading:
            raise RuntimeError("aclose(): asynchronous generator is already running")
        self.closed = True


def test_a_read_that_resists_cancellation_cannot_hold_the_call_and_the_source_is_closed_when_it_ends() -> None:
    source = ResistantRead(resist_s=0.4)

    async def call() -> tuple[FailoverResult, bool, bool]:
        result = await asyncio.wait_for(transcribe_with_failover(
            [Fake("a", 1, hang=True)], REQUEST, source,  # type: ignore[arg-type]
            deadline_s=0.05, cleanup_s=0.1, on_partial=lambda _: None, max_buffered_chunks=10), timeout=GUARD_S)
        at_return = source.closed
        await asyncio.sleep(0.8)
        return result, at_return, source.closed

    result, at_return, later = run(call())
    assert result.utterance is None and result.offer_text and result.release_failed
    assert not at_return and later  # closed once the read ended, not left open for good


def test_a_read_that_completes_in_the_turn_its_provider_is_cancelled_is_not_lost() -> None:
    """The provider's deadline cancels its wait in the same loop turn the read completes. The chunk must reach the next reader, not vanish."""

    async def scenario() -> list[int]:
        loop = asyncio.get_running_loop()
        gate: asyncio.Future[AudioChunk] = loop.create_future()
        calls = 0

        class Source:
            def __aiter__(self) -> "Source":
                return self

            async def __anext__(self) -> AudioChunk:
                nonlocal calls
                calls += 1
                if calls == 1:
                    return await gate
                raise StopAsyncIteration

            async def aclose(self) -> None:
                return None

        buffer = _ReplayBuffer(Source(), 10)  # type: ignore[arg-type]
        first = asyncio.ensure_future(anext(buffer.stream()))
        await asyncio.sleep(0)  # the first reader is now waiting for the read
        gate.set_result(chunk(0))
        loop.call_soon(first.cancel)  # runs after the read finishes and before the reader wakes
        await asyncio.gather(first, return_exceptions=True)
        return [item.sequence async for item in buffer.stream()]

    assert run(scenario()) == [0]
