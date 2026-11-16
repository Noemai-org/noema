"""Barge-in: the person starts speaking over the answer, so the answer stops (VOI-10, PERD §35.3, EC-V05).

The monitor reuses the end-of-speech detector, so a click or a cough shorter than `min_speech_ms` does not cut the answer
off. The stop limit (PERD §35.3 [REC], 300 ms) is an argument, never a constant (AGENT-16), and a configuration whose own
speech threshold exceeds it is refused, because it could never meet it.

Both streams are released within `close_s`, from configuration, and the two closes run side by side, so a close that stalls or ignores its cancellation can never keep the
other stream open: the microphone close is attempted even if the answer's never returns (Codex P1 and the Security Reviewer on noema#621). A close that does not finish is
cancelled at its bound, reported with a fixed code and let go of, exactly as `transcribe_utterance` does (noema#474); the result then says `release_failed`, because a
stream that could not be released in time is never a clean success. A close that raises is raised to the caller after both were attempted. The caller's cancellation does not skip
the release.

Known gap: the microphone also hears the answer being played. Without echo cancellation that echo can look like speech.
This module does not handle it; the playback loop is frame-synchronous (one microphone frame per answer frame) as a
deterministic stand-in for the real one. It is inside the AGENT-12 seam of noema#412 because it consumes the same `AudioSource` the speech provider reads, in the same package.
"""

import asyncio
import math
from collections.abc import Callable
from dataclasses import dataclass, replace

from noema_model.platform.telemetry import Telemetry
from noema_model.providers.speech.base import AudioChunk
from noema_model.voice.endpoint import AudioSource, EndpointDetector, EndpointState, VadConfig
from noema_model.voice.pipeline import Report, begin_close, cleanup_reporter


@dataclass(frozen=True, slots=True)
class BargeInResult:
    barged_in: bool
    stopped_after_ms: int | None  # milliseconds of speech heard when the answer was stopped
    within_limit: bool | None
    release_failed: bool = False  # True when a stream could not be released within its bound: abandoned and reported, never a clean success


class BargeInMonitor:
    def __init__(self, config: VadConfig, *, max_stop_ms: int) -> None:
        if max_stop_ms < 1:
            raise ValueError("the barge-in stop limit must be positive")
        if config.min_speech_ms > max_stop_ms:
            raise ValueError("min_speech_ms is longer than the barge-in stop limit")
        self._detector = EndpointDetector(config)
        self._max_stop_ms = max_stop_ms

    def feed(self, chunk: AudioChunk) -> bool:
        """True once the person's speech is established."""
        return self._detector.feed(chunk) is not EndpointState.WAITING

    def result(self, barged_in: bool) -> BargeInResult:
        if not barged_in:
            return BargeInResult(False, None, None)
        heard = round(self._detector.speech_ms)
        return BargeInResult(True, heard, heard <= self._max_stop_ms)


async def _play(answer: AudioSource, microphone: AudioSource, monitor: BargeInMonitor, play: Callable[[AudioChunk], None]) -> BargeInResult:
    async for out in answer:
        play(out)
        try:
            incoming = await microphone.__anext__()
        except StopAsyncIteration:
            continue  # no more microphone frames: the answer plays on
        if monitor.feed(incoming):
            return monitor.result(True)
    return monitor.result(False)


async def _release(answer: AudioSource, microphone: AudioSource, close_s: float, report: Report) -> tuple[bool, BaseException | None]:
    """Close both streams side by side, each within `close_s`. Returns (some close did not finish, the first error a close raised). Bounded: at most twice `close_s`, the second
    wait being only for the cancellation of a close that reached its bound to take effect."""
    closes = {begin_close(answer, close_s, report), begin_close(microphone, close_s, report)}
    _, pending = await asyncio.wait(closes, timeout=close_s)
    if pending:
        await asyncio.wait(pending, timeout=close_s)
    stalled = any(not close.done() or close.cancelled() for close in closes)
    error = next((e for close in closes if close.done() and not close.cancelled() and (e := close.exception()) is not None), None)
    return stalled, error


async def play_until_barge_in(
    answer: AudioSource,
    microphone: AudioSource,
    monitor: BargeInMonitor,
    play: Callable[[AudioChunk], None],
    *,
    close_s: float,
    telemetry: Telemetry | None = None,
    correlation_id: str = "uncorrelated",
) -> BargeInResult:
    """Play the answer chunk by chunk, reading one microphone frame after each, and stop when the person speaks. `close_s` bounds the release of each stream and has no default
    (AGENT-16: a bound comes from configuration)."""
    if not (math.isfinite(close_s) and close_s > 0):
        raise ValueError("the close bound must be a positive finite number from configuration")
    report = cleanup_reporter(telemetry, correlation_id)
    outcome: BargeInResult | None = None
    try:
        outcome = await _play(answer, microphone, monitor, play)
    finally:
        release = asyncio.ensure_future(_release(answer, microphone, close_s, report))
        cancelled = False
        while not release.done():
            try:
                await asyncio.shield(release)
            except asyncio.CancelledError:
                cancelled = True  # keep waiting: the release is bounded and must not be skipped; the cancellation is raised below
        stalled, error = release.result()
        if cancelled:
            raise asyncio.CancelledError
    if error is not None:
        raise error
    assert outcome is not None
    return replace(outcome, release_failed=stalled)
