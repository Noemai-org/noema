"""Barge-in releases both streams within a bound (VOI-10, REQ-VOI-004, AGENT-13, AGENT-16; Codex P1 and the Security Reviewer's blocking finding 1 on noema#621 at 11a1d089).

Both streams are closed side by side within `close_s`; a close that never finishes or ignores its cancellation is cancelled, reported with a fixed code and let go of, the other
stream's close is still attempted, the result says `release_failed`, and the caller's cancellation does not skip the release. The streams are SYNTHETIC scripted stand-ins under
the recorded AGENT-12 exception of noema#412; a hung close ends by itself after 1.5 s so that a failing test cannot hang the loop.
"""

import asyncio
import time
from collections.abc import Sequence

import pytest

from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.speech.base import AudioChunk
from noema_model.voice.bargein import BargeInMonitor, BargeInResult, play_until_barge_in
from noema_model.voice.endpoint import VadConfig
from tests.support import run
from tests.support_voice import chunk

VAD = VadConfig(speech_rms=300, min_speech_ms=40, hangover_ms=100)
GUARD_S = 8.0


class Handle:
    """An audio handle. `close` is one of: "ok", "hang" (never returns, honours cancellation), "stubborn" (ignores cancellation for 1.5 s, then ends)."""

    def __init__(self, levels: Sequence[int], close: str = "ok", delay_s: float = 0.0) -> None:
        self._levels = iter(enumerate(levels))
        self._close = close
        self._delay_s = delay_s
        self.closed = False

    def __aiter__(self) -> "Handle":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._delay_s:
            await asyncio.sleep(self._delay_s)  # a real suspension point, so a cancellation can land during playback
        try:
            i, level = next(self._levels)
        except StopIteration:
            raise StopAsyncIteration from None
        return chunk(i, level)

    async def aclose(self) -> None:
        self.closed = True
        if self._close == "hang":
            await asyncio.Event().wait()
        if self._close == "stubborn":
            end = asyncio.get_running_loop().time() + 1.5
            while asyncio.get_running_loop().time() < end:
                try:
                    await asyncio.sleep(0.05)
                except asyncio.CancelledError:
                    continue


async def barge(answer: Handle, microphone: Handle, telemetry: InMemoryTelemetry | None = None, close_s: float = 0.1) -> BargeInResult:
    return await play_until_barge_in(answer, microphone, BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, close_s=close_s, telemetry=telemetry, correlation_id="corr-b")


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
