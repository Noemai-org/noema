"""Synthetic ambient noise and mixing at a requested signal-to-noise ratio (VOI-17, REQ-VOI-007).

The profiles are stand-ins for a street, a market and a generator, built from sums of noise and tones. They are not
measurements of Lagos; the dB-meter readings of the field protocol (VOI-08) are the measurements.
"""

import math
import random
from array import array
from collections.abc import Sequence
from enum import StrEnum

_INT16_MAX = 32767
_INT16_MIN = -32768
_BYTES_PER_SAMPLE = 2
_MARKET_VOICES = 6
_GENERATOR_HARMONICS = (100.0, 200.0, 300.0)
_HORN_HZ = 400.0
_HORN_MS = 300
_LITTLE_ENDIAN_ONE = b"\x01\x00"


class NoiseProfile(StrEnum):
    STREET = "STREET"
    MARKET = "MARKET"
    GENERATOR = "GENERATOR"


def rms(values: Sequence[float]) -> float:
    if not values:
        raise ValueError("no samples")
    return math.sqrt(sum(v * v for v in values) / len(values))


def _street(count: int, rate: int, rng: random.Random) -> list[float]:
    level = 0.0
    out: list[float] = []
    for _ in range(count):
        level = 0.995 * level + rng.gauss(0.0, 0.1)  # leaky integration gives low-frequency traffic rumble
        out.append(level)
    horn_len = rate * _HORN_MS // 1000
    for _ in range(max(1, count // (rate * 2))):  # a horn about every two seconds, at random places
        start = rng.randrange(max(1, count - horn_len))
        for i in range(min(horn_len, count - start)):
            envelope = math.sin(math.pi * i / horn_len)
            out[start + i] += 2.0 * envelope * math.sin(2 * math.pi * _HORN_HZ * i / rate)
    return out


def _market(count: int, rate: int, rng: random.Random) -> list[float]:
    out = [0.0] * count
    for _ in range(_MARKET_VOICES):  # babble: several band-limited voices, each swelling and fading
        phase = rng.uniform(0, 2 * math.pi)
        mod_hz = rng.uniform(2.0, 5.0)
        level = 0.0
        for i in range(count):
            level = 0.8 * level + 0.2 * rng.gauss(0.0, 1.0)
            out[i] += level * (0.5 + 0.5 * math.sin(2 * math.pi * mod_hz * i / rate + phase))
    return out


def _generator(count: int, rate: int, rng: random.Random) -> list[float]:
    phases = [rng.uniform(0, 2 * math.pi) for _ in _GENERATOR_HARMONICS]
    return [
        sum(math.sin(2 * math.pi * hz * i / rate + p) for hz, p in zip(_GENERATOR_HARMONICS, phases, strict=True))
        + rng.gauss(0.0, 0.2)
        for i in range(count)
    ]


def synth_noise(profile: NoiseProfile, count: int, rate: int, rng: random.Random) -> list[float]:
    """`count` samples of the profile, scaled to unit RMS so a mix ratio is exact."""
    if count < 1 or rate < 1:
        raise ValueError("count and rate must be positive")
    if profile is NoiseProfile.STREET:
        raw = _street(count, rate, rng)
    elif profile is NoiseProfile.MARKET:
        raw = _market(count, rate, rng)
    else:
        raw = _generator(count, rate, rng)
    scale = rms(raw)
    if scale == 0.0:
        raise ValueError("degenerate noise")
    return [v / scale for v in raw]


def _is_little_endian() -> bool:
    return array("h", [1]).tobytes() == _LITTLE_ENDIAN_ONE


def pcm_samples(data: bytes) -> list[int]:
    """Little-endian 16-bit PCM to integers."""
    if not data or len(data) % _BYTES_PER_SAMPLE:
        raise ValueError("audio is empty or not whole 16-bit samples")
    samples = array("h")
    samples.frombytes(data)
    if not _is_little_endian():
        samples.byteswap()
    return list(samples)


def pcm_bytes(samples: Sequence[int]) -> bytes:
    packed = array("h", samples)
    if not _is_little_endian():
        packed.byteswap()
    return packed.tobytes()


def mix_at_snr(speech: bytes, profile: NoiseProfile, snr_db: float, rate: int, rng: random.Random) -> bytes:
    """Little-endian 16-bit PCM `speech` plus the profile's noise so that 20*log10(rms(speech)/rms(noise)) == snr_db."""
    signal = pcm_samples(speech)
    level = rms(signal)
    if level == 0.0:
        raise ValueError("silent speech has no signal-to-noise ratio")
    gain = level / 10 ** (snr_db / 20)
    noise = synth_noise(profile, len(signal), rate, rng)
    return pcm_bytes([max(_INT16_MIN, min(_INT16_MAX, round(s + gain * n))) for s, n in zip(signal, noise, strict=True)])
