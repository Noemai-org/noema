"""Barge-in: the person starts speaking over the answer, so the answer stops (VOI-10, PERD §35.3, EC-V05).

The monitor reuses the end-of-speech detector, so a click or a cough shorter than `min_speech_ms` does not cut the answer
off. The stop limit (PERD §35.3 [REC], 300 ms) is an argument, never a constant (AGENT-16), and a configuration whose own
speech threshold exceeds it is refused, because it could never meet it.

Known gap: the microphone also hears the answer being played. Without echo cancellation that echo can look like speech.
This module does not handle it; the playback loop is frame-synchronous (one microphone frame per answer frame) as a
deterministic stand-in for the real one.
"""

from collections.abc import Callable
from dataclasses import dataclass

from noema_model.providers.speech.base import AudioChunk
from noema_model.voice.endpoint import AudioSource, EndpointDetector, EndpointState, VadConfig


@dataclass(frozen=True, slots=True)
class BargeInResult:
    barged_in: bool
    stopped_after_ms: int | None  # milliseconds of speech heard when the answer was stopped
    within_limit: bool | None


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


async def play_until_barge_in(
    answer: AudioSource,
    microphone: AudioSource,
    monitor: BargeInMonitor,
    play: Callable[[AudioChunk], None],
) -> BargeInResult:
    """Play the answer chunk by chunk, reading one microphone frame after each, and stop when the person speaks."""
    try:
        async for out in answer:
            play(out)
            try:
                incoming = await microphone.__anext__()
            except StopAsyncIteration:
                continue  # no more microphone frames: the answer plays on
            if monitor.feed(incoming):
                return monitor.result(True)
        return monitor.result(False)
    finally:
        try:
            await answer.aclose()
        finally:
            await microphone.aclose()  # still runs if closing the answer failed, so the microphone never stays open
