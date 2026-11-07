"""Both streams are closed even when one fails to close (VOI-10, REQ-VOI-004, AGENT-13; Codex P2 on #218)."""

from collections.abc import AsyncGenerator

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec
from noema_model.voice.bargein import BargeInMonitor, play_until_barge_in
from noema_model.voice.endpoint import VadConfig
from tests.support import run

VAD = VadConfig(speech_rms=300, min_speech_ms=40, hangover_ms=100)


def chunk(i: int, level: int) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, level.to_bytes(2, "little", signed=True) * 320)


class Tracked:
    def __init__(self, levels: list[int], fail_on_close: bool = False) -> None:
        self._levels, self._fail = levels, fail_on_close
        self.closed = False

    async def stream(self) -> AsyncGenerator[AudioChunk, None]:
        try:
            for i, level in enumerate(self._levels):
                yield chunk(i, level)
        finally:
            self.closed = True
            if self._fail:
                raise RuntimeError("close failed")


def test_a_failure_closing_the_answer_still_closes_the_microphone() -> None:
    answer = Tracked([1000] * 6, fail_on_close=True)
    microphone = Tracked([5] * 2000)
    with pytest.raises(RuntimeError):
        run(play_until_barge_in(answer.stream(), microphone.stream(), BargeInMonitor(VAD, max_stop_ms=300), lambda _: None))
    assert answer.closed and microphone.closed


def test_a_failure_closing_the_microphone_still_closes_the_answer() -> None:
    answer = Tracked([1000] * 6)
    microphone = Tracked([5] * 2000, fail_on_close=True)
    with pytest.raises(RuntimeError):
        run(play_until_barge_in(answer.stream(), microphone.stream(), BargeInMonitor(VAD, max_stop_ms=300), lambda _: None))
    assert answer.closed and microphone.closed


def test_a_barge_in_closes_both_streams() -> None:
    answer = Tracked([1000] * 10)
    microphone = Tracked([5, 5, 2000, 2000, 2000, 2000, 2000, 2000, 2000, 2000])
    result = run(play_until_barge_in(answer.stream(), microphone.stream(), BargeInMonitor(VAD, max_stop_ms=300), lambda _: None))
    assert result.barged_in
    assert answer.closed and microphone.closed


def test_a_failing_play_callback_closes_both_streams() -> None:
    answer = Tracked([1000] * 6)
    microphone = Tracked([5] * 2000)

    def boom(_: AudioChunk) -> None:
        raise OSError("speaker gone")

    with pytest.raises(OSError):
        run(play_until_barge_in(answer.stream(), microphone.stream(), BargeInMonitor(VAD, max_stop_ms=300), boom))
    assert answer.closed and microphone.closed
