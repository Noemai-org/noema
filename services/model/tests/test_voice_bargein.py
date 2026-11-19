"""Barge-in tests: the answer stops where the person's speech is established (VOI-10, REQ-VOI-002, PERD §35.3, EC-V05).

The audio is synthetic and the streams are SYNTHETIC scripted stand-ins under the recorded AGENT-12 exception of noema#412 (the SpeechProvider seam, tests under
`services/model/tests/` only). The release of both streams within a bound is tested in `test_voice_bargein_bounded.py` and `test_voice_bargein_close.py`.
"""

from collections.abc import AsyncGenerator, Sequence

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec
from noema_model.voice.bargein import BargeInMonitor, BargeInResult, PlaybackBounds, play_until_barge_in
from noema_model.voice.endpoint import VadConfig
from tests.support import run

VAD = VadConfig(speech_rms=300, min_speech_ms=40, hangover_ms=100)


def chunk(i: int, level: int = 500) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, level.to_bytes(2, "little", signed=True) * 320)


async def audio_of(chunks: Sequence[AudioChunk]) -> AsyncGenerator[AudioChunk, None]:
    for item in chunks:
        yield item


def frames(levels: Sequence[int]) -> list[AudioChunk]:
    return [chunk(i, level) for i, level in enumerate(levels)]


def play(tts_frames: int, inbound_levels: Sequence[int], max_stop_ms: int = 300) -> tuple[BargeInResult, list[int]]:
    played: list[int] = []
    result = run(play_until_barge_in(audio_of([chunk(i, 1000) for i in range(tts_frames)]), audio_of(frames(inbound_levels)),
                                     BargeInMonitor(VAD, max_stop_ms=max_stop_ms), lambda c: played.append(c.sequence), bounds=PlaybackBounds(read_s=1.0, close_s=1.0, hold_s=1.0)))
    return result, played


def test_tts_plays_to_the_end_when_the_person_stays_quiet() -> None:
    result, played = play(6, [5] * 6)
    assert result.barged_in is False and played == list(range(6))


def test_tts_stops_at_the_frame_where_speech_is_established() -> None:
    result, played = play(10, [5, 5, 2000, 2000, 2000, 2000, 2000, 2000, 2000, 2000])
    assert result.barged_in is True
    assert played == [0, 1, 2, 3]  # 40 ms of speech needs two 20 ms frames: frames 2 and 3
    assert result.stopped_after_ms == 40


def test_a_click_does_not_stop_the_answer() -> None:
    result, played = play(8, [5, 2000, 5, 5, 5, 5, 5, 5])
    assert result.barged_in is False and played == list(range(8))


def test_a_barge_in_that_would_take_longer_than_the_limit_is_a_configuration_error() -> None:
    with pytest.raises(ValueError):
        BargeInMonitor(VadConfig(speech_rms=300, min_speech_ms=400, hangover_ms=100), max_stop_ms=300)


def test_the_limit_must_be_positive() -> None:
    with pytest.raises(ValueError):
        BargeInMonitor(VAD, max_stop_ms=0)


def test_the_close_bound_must_be_positive_and_finite() -> None:
    for bad in (0.0, -1.0, float("inf"), float("nan")):
        with pytest.raises(ValueError):
            run(play_until_barge_in(audio_of([chunk(0)]), audio_of([chunk(0)]), BargeInMonitor(VAD, max_stop_ms=300), lambda _: None, bounds=PlaybackBounds(read_s=1.0, close_s=bad, hold_s=1.0)))
