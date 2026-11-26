"""The 30 s maximum recording, enforced host-side (VOI-02, REQ-VOI-001, REQ-VOI-004, AGENT-13, AGENT-16; ruling A715).

A715: the maximum push-to-talk recording is 30 s as a UI setting, and Voice enforces the same limit host-side. The value is OPEN for the Founders (OD-P-122), so it is
data in `FrameLimits` with the decided value as its default and as a ceiling, the same shape as A191's frame cap: a caller or a pack may make it stricter, never longer.
Time is measured only from the samples fed (INV-21): 16 kHz mono PCM16, 20 ms to a 640-byte chunk. The chunk that would take the recording past the limit is refused with the
same ValueError as every other invalid frame, which ends the gate and releases the source. All audio here is SYNTHETIC (AGENT-25)."""

import asyncio

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec
from noema_model.voice.endpoint import EndpointDetector, EndpointGate, FrameLimits
from tests.support_voice import VAD, Source, chunk

CHUNK_MS = 20


def quiet(i: int) -> AudioChunk:
    return chunk(i, 0)


def test_the_default_maximum_is_30_seconds() -> None:
    assert FrameLimits().max_recording_ms == 30_000


def test_exactly_30_seconds_is_accepted() -> None:
    det = EndpointDetector(VAD)
    for i in range(30_000 // CHUNK_MS):
        det.feed(quiet(i))


def test_the_chunk_that_passes_30_seconds_is_refused() -> None:
    det = EndpointDetector(VAD)
    for i in range(30_000 // CHUNK_MS):
        det.feed(quiet(i))
    with pytest.raises(ValueError, match="A715"):
        det.feed(quiet(9_999))


def test_long_speech_is_refused_the_same_way() -> None:
    det = EndpointDetector(VAD)
    with pytest.raises(ValueError, match="A715"):
        for i in range(30_000 // CHUNK_MS + 1):
            det.feed(chunk(i, 5_000))  # loud, so the utterance has started; hangover never arrives


@pytest.mark.parametrize("ms", [30_020, 60_000, 3_600_000])
def test_a_maximum_above_the_ruling_is_refused(ms: int) -> None:
    with pytest.raises(ValueError, match="A715"):
        FrameLimits(max_recording_ms=ms)


@pytest.mark.parametrize("ms", [0, -1, -30_000])
def test_a_maximum_that_makes_no_sense_is_refused(ms: int) -> None:
    with pytest.raises(ValueError):
        FrameLimits(max_recording_ms=ms)


def test_a_stricter_maximum_is_allowed_and_enforced() -> None:
    det = EndpointDetector(VAD, FrameLimits(max_recording_ms=1_000))
    for i in range(1_000 // CHUNK_MS):
        det.feed(quiet(i))
    with pytest.raises(ValueError, match="A715"):
        det.feed(quiet(99))


def test_a_detector_given_a_longer_maximum_cannot_be_built() -> None:
    with pytest.raises(ValueError):
        EndpointDetector(VAD, FrameLimits(max_recording_ms=45_000))


def test_the_time_is_the_samples_not_a_clock() -> None:
    # 1,501 chunks of 20 ms are over 30 s whatever the wall clock says; 1,500 are not
    det = EndpointDetector(VAD)
    for i in range(1_500):
        det.feed(quiet(i))
    with pytest.raises(ValueError):
        det.feed(quiet(1_500))


def test_the_limit_is_exact_for_chunks_of_any_size() -> None:
    # 5 ms chunks (160 bytes): 6,000 of them are exactly 30 s, one more is over; an odd size is counted in samples, not rounded
    det = EndpointDetector(VAD)
    small = AudioChunk(0, AudioCodec.PCM_S16LE, 16_000, b"\x00\x00" * 80)
    for _ in range(6_000):
        det.feed(small)
    with pytest.raises(ValueError, match="A715"):
        det.feed(AudioChunk(1, AudioCodec.PCM_S16LE, 16_000, b"\x00\x00"))  # a single sample over


def test_a_refused_chunk_ends_the_gate_and_releases_the_microphone() -> None:
    mic = Source([quiet(i) for i in range(1_501)])
    gate = EndpointGate(mic.stream(), EndpointDetector(VAD))

    async def drain() -> None:
        async for _ in gate:
            pass

    with pytest.raises(ValueError, match="A715"):
        asyncio.run(drain())
    assert mic.closed, "an over-long recording must not leave the microphone open"
