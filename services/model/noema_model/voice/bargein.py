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
import weakref
from collections.abc import Callable
from dataclasses import dataclass, replace

from noema_model.platform.telemetry import Telemetry
from noema_model.providers.speech.base import AudioChunk
from noema_model.voice.endpoint import AudioSource, EndpointDetector, EndpointState, VadConfig
from noema_model.voice.pipeline import Report, _mark_retrieved, begin_close, cleanup_reporter, close_when_read_ends, release_unused


@dataclass(frozen=True, slots=True)
class BargeInResult:
    barged_in: bool
    stopped_after_ms: int | None  # milliseconds of speech heard when the answer was stopped
    within_limit: bool | None
    release_failed: bool = False  # True when a stream could not be released within its bound: abandoned and reported, never a clean success
    stalled: bool = False  # True when a stream stopped producing frames for `read_s`: the call ended there, both streams were released, and nothing was decided


class BargeInMonitor:
    def __init__(self, config: VadConfig, *, max_stop_ms: int) -> None:
        if max_stop_ms < 1:
            raise ValueError("the barge-in stop limit must be positive")
        if config.min_speech_ms > max_stop_ms:
            raise ValueError("min_speech_ms is longer than the barge-in stop limit")
        self._detector = EndpointDetector(config, recording_limited=False)  # listening while an answer plays is not a recording: no 30 s maximum (A715; Codex P2 on noema#1009)
        self._max_stop_ms = max_stop_ms

    def feed(self, chunk: AudioChunk) -> bool:
        """True once the person's speech is established."""
        return self._detector.feed(chunk) is not EndpointState.WAITING

    def result(self, barged_in: bool) -> BargeInResult:
        if not barged_in:
            return BargeInResult(False, None, None)
        heard = round(self._detector.speech_ms)
        return BargeInResult(True, heard, heard <= self._max_stop_ms)


@dataclass(frozen=True, slots=True)
class PlaybackBounds:
    """The three bounds of a barge-in call, all from configuration (AGENT-16: none has a default), validated when they are built, so the call itself can never be handed an invalid one while it
    holds live streams (Codex P2 on #621: a configuration error must not be able to retain the microphone).

    `read_s`: how long either stream may go without producing a frame. `close_s`: how long a close may take, and how long a stalled read may be waited for before the release goes on without it.
    `hold_s`: how long the release of a source whose read will not end is kept alive, before it is abandoned and reported (see `close_when_read_ends`)."""

    read_s: float
    close_s: float
    hold_s: float

    def __post_init__(self) -> None:
        for bound in (self.read_s, self.close_s, self.hold_s):
            if isinstance(bound, bool) or not (math.isfinite(bound) and bound > 0):
                raise ValueError("every playback bound must be a positive finite number from configuration")


async def _read(source: AudioSource, read_s: float, outstanding: list[tuple[AudioSource, "asyncio.Future[AudioChunk]"]]) -> AudioChunk:
    """One read in a task of its own, so that a source whose cancellation handler waits (an `EndpointGate` shields its close from a cancelled reader) cannot hold this call past `read_s`.
    At the bound, or if the caller is cancelled, the read is cancelled and recorded in `outstanding` with its source: it may still be running, and a source with a running read cannot be
    closed (an async generator raises), so the release waits for that read and closes the source after it (Codex P1 and P2 on #621)."""
    reading = asyncio.ensure_future(source.__anext__())
    reading.add_done_callback(_mark_retrieved)
    try:
        done, _ = await asyncio.wait({reading}, timeout=read_s)
    except asyncio.CancelledError:
        reading.cancel()
        outstanding.append((source, reading))
        raise
    if not done:
        reading.cancel()
        outstanding.append((source, reading))
        raise TimeoutError
    return reading.result()


async def _play(answer: AudioSource, microphone: AudioSource, monitor: BargeInMonitor, play: Callable[[AudioChunk], None], read_s: float,
                outstanding: list[tuple[AudioSource, "asyncio.Future[AudioChunk]"]]) -> BargeInResult:
    """Each read of either stream is bounded by `read_s`; a stream that stops producing frames ends the call at that bound (it then reaches the bounded release), never waits for ever."""
    try:
        while True:
            try:
                out = await _read(answer, read_s, outstanding)
            except StopAsyncIteration:
                return monitor.result(False)
            play(out)
            try:
                incoming = await _read(microphone, read_s, outstanding)
            except StopAsyncIteration:
                continue  # no more microphone frames: the answer plays on
            if monitor.feed(incoming):
                return monitor.result(True)
    except TimeoutError:
        return replace(monitor.result(False), stalled=True)


async def _release(
    answer: AudioSource,
    microphone: AudioSource,
    bounds: PlaybackBounds,
    report: Report,
    outstanding: list[tuple[AudioSource, "asyncio.Future[AudioChunk]"]],
) -> tuple[bool, BaseException | None]:
    """Release both streams. A stream whose read is still running is first given `close_s` for that read to end (it was cancelled); if it does, it is closed with the other, side by side, each
    within `close_s`. If it does not, its close is handed to `close_when_read_ends` (closed the moment the read ends, abandoned and reported after `hold_s`) and the release counts as failed.
    Returns (some release did not complete within its bound, the first error a close raised). Bounded: about three times `close_s`, plus nothing for the handed-over source."""
    if outstanding:
        await asyncio.wait({read for _, read in outstanding}, timeout=bounds.close_s)  # pairs, not a mapping: a source need not be hashable (Codex P2 on #621 at d07c012d)
    deferred = [(source, read) for source, read in outstanding if not read.done()]
    for source, read in deferred:
        close_when_read_ends(read, source, bounds.hold_s, bounds.close_s, report)
    deferred_sources = {id(source) for source, _ in deferred}
    now = [source for source in (answer, microphone) if id(source) not in deferred_sources]
    closes = {begin_close(source, bounds.close_s, report) for source in now}
    stalled = bool(deferred)
    if closes:
        _, pending = await asyncio.wait(closes, timeout=bounds.close_s)
        if pending:
            await asyncio.wait(pending, timeout=bounds.close_s)
        stalled = stalled or bool(pending) or any(close.cancelled() for close in closes)  # a close that reached its bound failed, even if it ended during the cancellation grace
    error = next((e for close in closes if close.done() and not close.cancelled() and (e := close.exception()) is not None), None)
    return stalled, error


async def play_until_barge_in(
    answer: AudioSource,
    microphone: AudioSource,
    monitor: BargeInMonitor,
    play: Callable[[AudioChunk], None],
    *,
    bounds: PlaybackBounds,
    telemetry: Telemetry | None = None,
    correlation_id: str = "uncorrelated",
) -> BargeInResult:
    """Play the answer chunk by chunk, reading one microphone frame after each, and stop when the person speaks. The bounds are configuration, validated when they were built
    (`PlaybackBounds`), so this call never refuses with live streams in hand."""
    report = cleanup_reporter(telemetry, correlation_id)
    for source in (answer, microphone):
        try:
            weakref.ref(source)
        except TypeError:  # the retry of a close that failed while a read ran holds the source weakly (see `close_when_read_ends`); refuse now, and release both (Codex P2 on #621 at c23a5f45)
            await asyncio.gather(release_unused(answer, bounds.close_s, report), release_unused(microphone, bounds.close_s, report))
            raise TypeError("the audio sources must support weak references (a class with __slots__ needs a __weakref__ slot)") from None
    outstanding: list[tuple[AudioSource, asyncio.Future[AudioChunk]]] = []
    outcome: BargeInResult | None = None
    try:
        outcome = await _play(answer, microphone, monitor, play, bounds.read_s, outstanding)
    finally:
        release = asyncio.ensure_future(_release(answer, microphone, bounds, report, outstanding))
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
