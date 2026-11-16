"""A mix keeps the requested signal-to-noise ratio even for loud speech (VOI-17, REQ-VOI-007; Codex P2 on #200).

Clipping the sum would change the noise and distort the speech, so the audio would no longer be the condition it is labelled
as. When the sum would not fit, both components are scaled by one common factor, which keeps their ratio.
"""

import math
import random
import struct

import pytest

from noema_model.voice.simulation.noise import NoiseProfile, mix_at_snr, mix_parts_at_snr, pcm_samples, rms

RATE = 16000


def tone(amplitude: int, ms: int = 500, hz: int = 220) -> bytes:
    count = RATE * ms // 1000
    return struct.pack(f"<{count}h", *[int(amplitude * math.sin(2 * math.pi * hz * i / RATE)) for i in range(count)])


@pytest.mark.parametrize("snr_db", [-6, 0, 10])
def test_loud_speech_still_has_the_requested_snr_between_the_mixed_components(snr_db: int) -> None:
    parts = mix_parts_at_snr(tone(32000), NoiseProfile.STREET, snr_db, RATE, random.Random(3))
    measured = 20 * math.log10(rms(parts.speech) / rms(parts.noise))
    assert measured == pytest.approx(snr_db, abs=0.5)


def test_the_sum_of_the_parts_is_the_mix_and_fits_in_16_bits() -> None:
    speech = tone(32000)
    parts = mix_parts_at_snr(speech, NoiseProfile.STREET, -6, RATE, random.Random(3))
    mixed = pcm_samples(mix_at_snr(speech, NoiseProfile.STREET, -6, RATE, random.Random(3)))
    assert mixed == [max(-32768, min(32767, s + n)) for s, n in zip(parts.speech, parts.noise, strict=True)]
    assert all(-32768 <= v <= 32767 for v in mixed)


def test_loud_speech_is_scaled_down_and_says_by_how_much() -> None:
    parts = mix_parts_at_snr(tone(32000), NoiseProfile.STREET, -6, RATE, random.Random(3))
    assert 0.0 < parts.scale < 1.0


def test_speech_that_fits_is_not_scaled_at_all() -> None:
    speech = tone(8000)
    parts = mix_parts_at_snr(speech, NoiseProfile.MARKET, 20, RATE, random.Random(3))
    assert parts.scale == 1.0
    assert parts.speech == pcm_samples(speech)
