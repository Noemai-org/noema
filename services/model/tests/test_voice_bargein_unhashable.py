"""An audio source need not be hashable (VOI-10, REQ-VOI-004, AGENT-13; Codex P2 on #621 at d07c012d). Tracking outstanding reads must not key them by the source: a mutable dataclass supports weak references but is unhashable, and the TypeError ended the cleanup before either stream was closed. The streams are SYNTHETIC scripted stand-ins under the recorded AGENT-12 exception of noema#412 (the SpeechProvider seam; tests under `services/model/tests/` only)."""

import asyncio
from dataclasses import dataclass

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec
from noema_model.voice.bargein import BargeInMonitor, PlaybackBounds, play_until_barge_in
from noema_model.voice.endpoint import VadConfig
from tests.support import run

VAD = VadConfig(speech_rms=300, min_speech_ms=40, hangover_ms=100)
BOUNDS = PlaybackBounds(read_s=0.02, close_s=0.2, hold_s=0.5)


def chunk(i: int, level: int) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, level.to_bytes(2, "little", signed=True) * 320)


@dataclass  # eq=True and not frozen: unhashable, yet it supports weak references
class Handle:
    levels: list[int]
    stall_after: int | None = None
    closed: bool = False
    index: int = 0

    def __aiter__(self) -> "Handle":
        return self

    async def __anext__(self) -> AudioChunk:
        if self.stall_after is not None and self.index >= self.stall_after:
            await asyncio.sleep(3600)  # a read that stalls; the bound cancels it
        if self.index >= len(self.levels):
            raise StopAsyncIteration
        self.index += 1
        return chunk(self.index, self.levels[self.index - 1])

    async def aclose(self) -> None:
        self.closed = True


def test_the_handle_really_is_unhashable() -> None:
    with pytest.raises(TypeError):
        hash(Handle([1]))


@pytest.mark.parametrize("which", ["microphone", "answer"])
def test_a_stalled_read_of_an_unhashable_source_still_closes_both_streams(which: str) -> None:
    answer = Handle([1000] * 6, stall_after=1 if which == "answer" else None)
    microphone = Handle([5] * 2000, stall_after=1 if which == "microphone" else None)
    result = run(play_until_barge_in(answer, microphone, BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, bounds=BOUNDS))
    assert answer.closed and microphone.closed
    assert result.stalled
