"""A low-quality microphone and a lossy, jittery 3G link (VOI-17, REQ-VOI-007, SoT §37.5).

Both are seeded and clock-free: arrival times are computed from the send schedule, never measured, so a run repeats
exactly (INV-21). The rates and ranges are arguments; this module holds no preset (AGENT-16).
"""

import math
import random
from collections.abc import Sequence
from dataclasses import dataclass

from noema_model.providers.speech.base import AudioChunk
from noema_model.voice.simulation.noise import pcm_bytes, pcm_samples

_MAX_BITS = 16
_INT16_MAX = 32767


def low_quality_mic(pcm: bytes, rate: int, *, cutoff_hz: int, bits: int, clip_level: int) -> bytes:
    """One-pole low-pass at `cutoff_hz`, then clipping at `clip_level`, then quantising to `bits` bits."""
    if not 0 < cutoff_hz < rate / 2:
        raise ValueError("cutoff must be above 0 and below the Nyquist frequency")
    if not 1 <= bits <= _MAX_BITS:
        raise ValueError("bits must be between 1 and 16")
    if not 1 <= clip_level <= _INT16_MAX:
        raise ValueError("clip level must be between 1 and 32767")
    samples = pcm_samples(pcm)
    dt = 1.0 / rate
    rc = 1.0 / (2 * math.pi * cutoff_hz)
    alpha = dt / (rc + dt)
    step = 2 ** (_MAX_BITS - bits)
    out: list[int] = []
    level = 0.0
    for sample in samples:
        level += alpha * (sample - level)
        clipped = max(-clip_level, min(clip_level, level))
        out.append(max(-_INT16_MAX, min(_INT16_MAX, round(clipped / step) * step)))
    return pcm_bytes(out)


@dataclass(frozen=True, slots=True)
class NetworkProfile:
    jitter_ms_max: int  # each frame is delayed by a uniform 0..jitter_ms_max
    loss_rate: float  # each frame is dropped independently with this probability
    frame_ms: int  # the sender's schedule: frame i is sent at i * frame_ms

    def __post_init__(self) -> None:
        if self.jitter_ms_max < 0 or self.frame_ms < 1 or not 0.0 <= self.loss_rate <= 1.0:
            raise ValueError("network profile out of range")


@dataclass(frozen=True, slots=True)
class Arrival:
    arrival_ms: int
    chunk: AudioChunk  # a lost frame arrives with no audio
    lost: bool


def simulate_network(chunks: Sequence[AudioChunk], profile: NetworkProfile, rng: random.Random) -> tuple[Arrival, ...]:
    """The chunks as the receiver sees them, ordered by arrival time. Jitter may reorder them; a lost frame keeps its
    sequence number and carries no bytes, so the receiver can tell loss from silence."""
    arrivals: list[Arrival] = []
    for index, chunk in enumerate(chunks):
        sent_ms = index * profile.frame_ms
        if rng.random() < profile.loss_rate:
            arrivals.append(Arrival(sent_ms, AudioChunk(chunk.sequence, chunk.codec, chunk.sample_rate_hz, b""), True))
            continue
        arrivals.append(Arrival(sent_ms + rng.randint(0, profile.jitter_ms_max), chunk, False))
    return tuple(sorted(arrivals, key=lambda arrival: arrival.arrival_ms))
