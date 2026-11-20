"""Barge-in releases both streams within a bound (VOI-10, REQ-VOI-004, AGENT-13, AGENT-16; Codex P1 and the Security Reviewer's blocking finding 1 on noema#621 at 11a1d089).

Both streams are closed side by side within `close_s`; a close that never finishes or ignores its cancellation is cancelled, reported with a fixed code and let go of, the other
stream's close is still attempted, the result says `release_failed`, and the caller's cancellation does not skip the release. The streams are SYNTHETIC scripted stand-ins under
the recorded AGENT-12 exception of noema#412; a hung close ends by itself after 1.5 s so that a failing test cannot hang the loop.
"""

import asyncio
import time
from collections.abc import AsyncIterator, Sequence

import pytest

from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.speech.base import AudioChunk
from noema_model.voice.bargein import BargeInMonitor, BargeInResult, PlaybackBounds, play_until_barge_in
from noema_model.voice.endpoint import EndpointDetector, EndpointGate, VadConfig
from tests.support import run
from tests.support_voice import chunk

VAD = VadConfig(speech_rms=300, min_speech_ms=40, hangover_ms=100)
GUARD_S = 8.0


class Handle:
    """An audio handle. `close` is one of: "ok", "hang" (never returns, honours cancellation), "grace" (catches its cancellation and finishes 30 ms later), "stubborn" (ignores cancellation for 1.5 s, then ends)."""

    def __init__(self, levels: Sequence[int], close: str = "ok", delay_s: float = 0.0, stall_after: int | None = None) -> None:
        self._levels = iter(enumerate(levels))
        self._close = close
        self._delay_s = delay_s
        self._stall_after = stall_after  # after this many frames the next read never returns
        self._served = 0
        self.closed = False
        self.finished = False  # the close ran to its end

    def __aiter__(self) -> "Handle":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._stall_after is not None and self._served >= self._stall_after:
            await asyncio.Event().wait()  # a stream that stops producing frames
        self._served += 1
        if self._delay_s:
            await asyncio.sleep(self._delay_s)  # a real suspension point, so a cancellation can land during playback
        try:
            i, level = next(self._levels)
        except StopIteration:
            raise StopAsyncIteration from None
        return chunk(i, level)

    async def aclose(self) -> None:
        self.closed = True
        if self._close == "slow":
            await asyncio.sleep(0.02)  # a real close takes a few turns of the loop
            self.finished = True
            return
        if self._close == "hang":
            await asyncio.Event().wait()
        if self._close == "grace":
            try:
                await asyncio.Event().wait()
            except asyncio.CancelledError:
                await asyncio.sleep(0.03)  # catches its cancellation and finishes a moment later, inside the cancellation grace period
                return
        if self._close == "stubborn":
            end = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < end:
                try:
                    await asyncio.sleep(0.05)
                except asyncio.CancelledError:
                    continue


async def barge(answer: Handle, microphone: Handle, telemetry: InMemoryTelemetry | None = None, close_s: float = 0.1) -> BargeInResult:
    return await play_until_barge_in(answer, microphone, BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, bounds=PlaybackBounds(read_s=5.0, close_s=close_s, hold_s=1.0), telemetry=telemetry, correlation_id="corr-b")


@pytest.mark.parametrize("which", ["answer", "microphone"])
@pytest.mark.parametrize("mode", ["hang", "stubborn"])
def test_a_close_that_never_finishes_cannot_hold_the_call_and_the_other_stream_is_closed(which: str, mode: str) -> None:
    answer = Handle([1000] * 6, close=mode if which == "answer" else "ok")
    microphone = Handle([5] * 2000, close=mode if which == "microphone" else "ok")
    telemetry = InMemoryTelemetry()
    started = time.monotonic()

    async def call() -> BargeInResult:
        result = await asyncio.wait_for(barge(answer, microphone, telemetry), timeout=GUARD_S)
        await asyncio.sleep(1.8)  # let a stubborn close end by itself so the loop can be torn down
        return result

    result = run(call())
    assert time.monotonic() - started < 4.0
    assert answer.closed and microphone.closed  # the microphone close is attempted even when the answer's stalls, and the other way round
    assert result.release_failed  # a stream that could not be released in time is never a clean success
    assert [f["code"] for n, c, f in telemetry.events if n == "voice_audio_cleanup" and c == "corr-b"] == ["VOI_AUDIO_CLOSE_ABANDONED"]


def test_a_caller_that_cancels_during_barge_in_still_gets_both_streams_closed() -> None:
    answer, microphone = Handle([1000] * 600, delay_s=0.01), Handle([5] * 100000)

    async def call() -> None:
        task = asyncio.ensure_future(barge(answer, microphone, close_s=0.5))
        await asyncio.sleep(0.05)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task

    run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert answer.closed and microphone.closed


def test_a_clean_close_is_not_a_release_failure() -> None:
    answer, microphone = Handle([1000] * 6), Handle([5] * 2000)
    result = run(barge(answer, microphone))
    assert answer.closed and microphone.closed and not result.release_failed


# ---- Codex P1 on noema#621 at 6d084abd: a stream that stops producing frames reaches the bounded cleanup --------------------------------------------------------------


@pytest.mark.parametrize("which", ["answer", "microphone"])
def test_a_stream_that_stops_producing_frames_ends_the_call_at_the_read_bound_and_both_streams_are_closed(which: str) -> None:
    answer = Handle([1000] * 50, stall_after=1 if which == "answer" else None)
    microphone = Handle([5] * 2000, stall_after=1 if which == "microphone" else None)
    started = time.monotonic()

    async def call() -> BargeInResult:
        return await asyncio.wait_for(play_until_barge_in(answer, microphone, BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, bounds=PlaybackBounds(read_s=0.1, close_s=0.2, hold_s=1.0)), timeout=GUARD_S)

    result = run(call())
    assert time.monotonic() - started < 1.5  # not "until external cancellation"
    assert answer.closed and microphone.closed  # the microphone is released although a stream stalled
    assert result.stalled and not result.barged_in



# ---- Codex P2s on noema#621 at ca81fecd ---------------------------------------------------------------------------------------------------------------------------------


def test_a_close_that_overran_its_bound_is_a_release_failure_even_if_it_finishes_in_the_cancellation_grace_period() -> None:
    answer, microphone = Handle([1000] * 6, close="grace"), Handle([5] * 2000)
    telemetry = InMemoryTelemetry()

    async def call() -> BargeInResult:
        return await barge(answer, microphone, telemetry, close_s=0.1)

    result = run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert answer.closed and microphone.closed
    assert [f["code"] for n, c, f in telemetry.events if n == "voice_audio_cleanup"] == ["VOI_AUDIO_CLOSE_ABANDONED"]  # it was cancelled at its bound ...
    assert result.release_failed  # ... so the release did not succeed, whether or not it ended a moment later


# ---- Codex P1 and P2 on noema#621 at e64e3380 ------------------------------------------------------------------------------------------------------------------------------


def test_a_stalled_read_through_an_endpoint_gate_whose_own_close_stalls_still_ends_the_call_at_the_read_bound() -> None:
    answer = Handle([1000] * 50)
    microphone = Handle([5] * 2000, close="hang", stall_after=0)  # a read that never returns and a close that never returns
    gate = EndpointGate(microphone, EndpointDetector(VAD))  # the gate shields its close from a cancelled reader
    started = time.monotonic()

    async def call() -> BargeInResult:
        return await play_until_barge_in(answer, gate, BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, bounds=PlaybackBounds(read_s=0.05, close_s=0.1, hold_s=1.0))

    result = run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert time.monotonic() - started < 2.0  # not "until the microphone's close is released"
    assert answer.closed and microphone.closed and result.stalled and result.release_failed



# ---- Codex P2 on noema#621 at 20b6d355: a timed-out read is still running when the release starts; the close waits for it and is not lost ------------------------------


async def slow_to_cancel_microphone(state: dict[str, bool]) -> "AsyncIterator[AudioChunk]":
    """An async generator whose read takes 50 ms to finish after it is cancelled and then yields: closing it while that read runs raises RuntimeError."""
    try:
        while True:
            try:
                await asyncio.sleep(10)  # a stall
            except asyncio.CancelledError:
                await asyncio.sleep(0.05)
                yield chunk(0, 5)
    finally:
        state["closed"] = True


def test_a_timed_out_read_that_ends_late_is_waited_for_and_the_stream_is_then_closed_not_left_open() -> None:
    state = {"closed": False}
    answer = Handle([1000] * 50)

    async def call() -> BargeInResult:
        return await play_until_barge_in(answer, slow_to_cancel_microphone(state), BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, bounds=PlaybackBounds(read_s=0.05, close_s=0.5, hold_s=1.0))  # type: ignore[arg-type]

    result = run(asyncio.wait_for(call(), timeout=GUARD_S))  # no RuntimeError from closing a generator that is still running
    assert result.stalled and answer.closed
    assert state["closed"]  # the microphone was closed once its read ended


# ---- Codex P2s on noema#621 at ca81fecd and 20b6d355: an invalid bound can never reach a call that holds live streams ----------------------------------------------------------
# (the tests that handed a call a bad bound and checked the release are replaced by this: `PlaybackBounds` refuses a bad bound when it is built, before any stream is acquired)


def test_a_bad_bound_cannot_be_built_so_a_call_never_holds_streams_with_one() -> None:
    for field in ("read_s", "close_s", "hold_s"):
        for bad in (0.0, -1.0, float("nan"), float("inf"), True):
            with pytest.raises(ValueError):
                PlaybackBounds(**({"read_s": 0.1, "close_s": 0.1, "hold_s": 0.1} | {field: bad}))


# ---- Codex P2 on noema#621 at c23a5f45: a source that cannot be weakly referenced is refused before playback and both streams are released ---------------------------------


class Slotted:
    """A source with __slots__ and no __weakref__ slot: the deferred-release retry cannot hold it weakly."""

    __slots__ = ("closed", "reads")

    def __init__(self) -> None:
        self.closed = False
        self.reads = 0

    def __aiter__(self) -> "Slotted":
        return self

    async def __anext__(self) -> AudioChunk:
        self.reads += 1
        return chunk(self.reads, 1000)

    async def aclose(self) -> None:
        self.closed = True


@pytest.mark.parametrize("which", ["answer", "microphone"])
def test_a_source_that_cannot_be_weakly_referenced_is_refused_before_playback_and_both_streams_are_released(which: str) -> None:
    other = Handle([1000] * 6)
    slotted = Slotted()
    answer, microphone = (slotted, other) if which == "answer" else (other, slotted)

    async def call() -> None:
        with pytest.raises(TypeError):
            await play_until_barge_in(answer, microphone, BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, bounds=PlaybackBounds(read_s=0.1, close_s=0.2, hold_s=0.2))
        await asyncio.sleep(0.05)

    run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert slotted.closed and slotted.reads == 0 and other.closed  # nothing was played and both were released
