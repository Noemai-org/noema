"""STT frame limits on the endpoint detector (VOI-02, REQ-VOI-001, REQ-VOI-004, AGENT-13, AGENT-16; ruling A191, committed in docs#169).

A191: STT frames are capped at 32,000 bytes, and only 16 kHz mono PCM16 is accepted, as configuration data. The Security Reviewer asked for this on #474 (a frame of any size was
read whole into the energy measure, and any sample rate was accepted). Before this change `EndpointDetector.feed` checked only that the rate was at least 1, the data
non-empty and whole 16-bit samples. A refused frame ends the gate and closes the source, like every other invalid frame. AudioChunk carries no channel count, so mono is the
contract of the source, not something the detector can see. All audio here is SYNTHETIC (AGENT-25).
"""

import asyncio

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec
from noema_model.voice.endpoint import EndpointDetector, FrameLimits, until_end_of_speech
from tests.support import run
from tests.support_voice import VAD, Source, loud_then_silent


def pcm(n_bytes: int, rate: int = 16000) -> AudioChunk:
    return AudioChunk(0, AudioCodec.PCM_S16LE, rate, b"\x10\x00" * (n_bytes // 2))


def test_the_default_limits_are_the_values_of_a191() -> None:
    limits = FrameLimits()
    assert limits.max_chunk_bytes == 32_000
    assert limits.sample_rate_hz == 16_000


def test_a_frame_of_exactly_the_cap_is_accepted() -> None:
    EndpointDetector(VAD).feed(pcm(32_000))


@pytest.mark.parametrize("n_bytes", [32_002, 64_000, 1_000_000])
def test_a_frame_over_the_cap_is_refused(n_bytes: int) -> None:
    with pytest.raises(ValueError, match="frame"):
        EndpointDetector(VAD).feed(pcm(n_bytes))


@pytest.mark.parametrize("rate", [8_000, 15_999, 16_001, 22_050, 44_100, 48_000])
def test_any_sample_rate_but_16_khz_is_refused(rate: int) -> None:
    with pytest.raises(ValueError, match="16"):
        EndpointDetector(VAD).feed(pcm(640, rate))


def test_16_khz_is_accepted() -> None:
    EndpointDetector(VAD).feed(pcm(640, 16_000))


def test_the_limits_are_data_the_caller_can_pass() -> None:
    small = EndpointDetector(VAD, FrameLimits(max_chunk_bytes=1_000))
    small.feed(pcm(1_000))
    with pytest.raises(ValueError, match="frame"):
        small.feed(pcm(1_002))


@pytest.mark.parametrize("kwargs", [{"max_chunk_bytes": 0}, {"max_chunk_bytes": -2}, {"max_chunk_bytes": 3}, {"sample_rate_hz": 0}, {"sample_rate_hz": -16_000}])
def test_limits_that_make_no_sense_are_refused(kwargs: dict[str, int]) -> None:
    with pytest.raises(ValueError):
        FrameLimits(**kwargs)


@pytest.mark.parametrize("bad", [pcm(32_002), pcm(640, 48_000)], ids=["oversize", "wrong-rate"])
def test_a_refused_frame_closes_the_source_and_ends_the_gate(bad: AudioChunk) -> None:
    source = Source([bad, *loud_then_silent()])
    gate = until_end_of_speech(source.stream(), EndpointDetector(VAD))

    async def scenario() -> bool:
        with pytest.raises(ValueError):
            await gate.__anext__()
        assert source.closed  # a consumer iterating the gate directly has no other cleanup
        with pytest.raises(StopAsyncIteration):
            await gate.__anext__()
        return True

    assert run(asyncio.wait_for(scenario(), timeout=5.0))
