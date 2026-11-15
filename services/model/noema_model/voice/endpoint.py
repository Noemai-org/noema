"""End-of-speech detection on decoded PCM (VOI-02, REQ-VOI-001).

A deterministic energy detector: no clock, no I/O, time measured only from the samples it is fed (INV-21). It decides when
to stop sending audio so the provider can settle a final transcript. It never decides what was said.

Its thresholds are arguments. They are tuned in the lab and in the field (VOI-16, REQ-VOI-005), so none is written here
(AGENT-16). It measures PCM only: the POC phone sends PCM16 at 16 kHz (ruling A19, accepted as F7; PERD §35.1 names Opus
and stays unchanged), and decoding Opus would need a codec dependency and an AGENT-14 entry first. Opus audio is refused
rather than guessed at.
"""

import asyncio
import math
import sys
from array import array
from collections.abc import AsyncIterator
from dataclasses import dataclass
from enum import StrEnum
from typing import Protocol

from noema_model.providers.speech.base import AudioChunk, AudioCodec

_BYTES_PER_SAMPLE = 2
_MS_PER_S = 1000
_A191_MAX_CHUNK_BYTES = 32_000  # ruling A191 (docs#169): a hard ceiling. The limits below are data that may be made stricter, never looser.
_A191_SAMPLE_RATE_HZ = 16_000


@dataclass(frozen=True, slots=True)
class VadConfig:
    speech_rms: int  # RMS of 16-bit samples at or above which a chunk counts as speech
    min_speech_ms: int  # speech needed before an utterance has started: a click is not speech
    hangover_ms: int  # silence after speech that ends the utterance

    def __post_init__(self) -> None:
        if min(self.speech_rms, self.min_speech_ms, self.hangover_ms) < 1:
            raise ValueError("VAD thresholds must be positive")


@dataclass(frozen=True, slots=True)
class FrameLimits:
    """What one audio frame may be, as data (ruling A191, committed in docs#169): at most 32,000 bytes, and 16 kHz only. Mono 16-bit PCM is the contract of the source;
    `AudioChunk` carries no channel count, so the detector cannot see it. A frame outside the limits is refused and ends the gate (AGENT-13, REQ-VOI-004). The defaults are
    the decided values. They are a ceiling: a caller or a pack may pass a stricter byte cap, never a higher one and never another rate; ruling A191 is the only authority for either."""

    max_chunk_bytes: int = _A191_MAX_CHUNK_BYTES
    sample_rate_hz: int = _A191_SAMPLE_RATE_HZ

    def __post_init__(self) -> None:
        if self.max_chunk_bytes < _BYTES_PER_SAMPLE or self.max_chunk_bytes % _BYTES_PER_SAMPLE:
            raise ValueError("frame limits must be a positive even byte cap")
        if self.max_chunk_bytes > _A191_MAX_CHUNK_BYTES:
            raise ValueError(f"ruling A191: the frame cap cannot be raised above {_A191_MAX_CHUNK_BYTES} bytes (a stricter cap is allowed)")
        if self.sample_rate_hz != _A191_SAMPLE_RATE_HZ:
            raise ValueError(f"ruling A191: only {_A191_SAMPLE_RATE_HZ} Hz audio is accepted")


class EndpointState(StrEnum):
    WAITING = "WAITING"
    SPEAKING = "SPEAKING"
    ENDED = "ENDED"


def _rms(data: bytes) -> float:
    samples = array("h")
    samples.frombytes(data)
    if sys.byteorder == "big":
        samples.byteswap()
    return math.sqrt(sum(s * s for s in samples) / len(samples))


class EndpointDetector:
    def __init__(self, config: VadConfig, limits: FrameLimits | None = None) -> None:
        self._config = config
        self._limits = FrameLimits() if limits is None else limits
        self._speech_ms = 0.0
        self._silence_ms = 0.0
        self._state = EndpointState.WAITING

    def feed(self, chunk: AudioChunk) -> EndpointState:
        if chunk.codec is not AudioCodec.PCM_S16LE:
            raise ValueError("end-of-speech detection needs decoded PCM")
        if chunk.sample_rate_hz < 1 or not chunk.data or len(chunk.data) % _BYTES_PER_SAMPLE:
            raise ValueError("audio chunk is empty or not whole 16-bit samples")
        if chunk.sample_rate_hz != self._limits.sample_rate_hz:
            raise ValueError(f"audio frame is not {self._limits.sample_rate_hz} Hz (ruling A191)")
        if len(chunk.data) > self._limits.max_chunk_bytes:
            raise ValueError(f"audio frame is over the {self._limits.max_chunk_bytes}-byte cap (ruling A191)")
        if self._state is EndpointState.ENDED:
            return self._state
        duration_ms = len(chunk.data) // _BYTES_PER_SAMPLE * _MS_PER_S / chunk.sample_rate_hz
        if _rms(chunk.data) >= self._config.speech_rms:
            self._speech_ms += duration_ms
            self._silence_ms = 0.0
        elif self._state is EndpointState.SPEAKING:
            self._silence_ms += duration_ms
        else:
            self._speech_ms = 0.0  # a click followed by silence never became speech
        if self._speech_ms >= self._config.min_speech_ms:
            self._state = EndpointState.SPEAKING
        if self._state is EndpointState.SPEAKING and self._silence_ms >= self._config.hangover_ms:
            self._state = EndpointState.ENDED
        return self._state


class AudioSource(Protocol):
    """An async stream of audio that can be closed. Async generators satisfy it."""

    def __aiter__(self) -> AsyncIterator[AudioChunk]: ...

    async def __anext__(self) -> AudioChunk: ...

    async def aclose(self) -> None: ...


class EndpointGate:
    """Passes audio through up to and including the chunk that ends the utterance, then stops.

    It is a class, not an async generator, so that `aclose()` closes the underlying source even if nothing was ever read
    from the gate: closing a generator that never started runs none of its cleanup, and a live microphone would stay open
    (Codex P1 on #193, REQ-VOI-004, AGENT-13).
    """

    def __init__(self, audio: AudioSource, detector: EndpointDetector) -> None:
        self._audio = audio
        self._detector = detector
        self._done = False
        self._closed = False
        self._closing: asyncio.Future[None] | None = None

    def __aiter__(self) -> "EndpointGate":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._done:
            await self.aclose()
            raise StopAsyncIteration
        try:
            chunk = await self._audio.__anext__()
        except StopAsyncIteration:
            await self.aclose()
            raise
        except BaseException:
            await self.aclose()
            raise
        if self._detector.feed(chunk) is EndpointState.ENDED:
            self._done = True
        return chunk

    async def aclose(self) -> None:
        """Close the source. The close runs as a task of its own that a cancelled caller does not take down, and `_closed` is set only once
        it has finished, so a second call resumes an interrupted close and never mistakes a started close for a finished one (Codex P1 on #193)."""
        self._done = True
        if self._closed:
            return
        if self._closing is None:
            self._closing = asyncio.ensure_future(self._audio.aclose())
        await asyncio.shield(self._closing)
        self._closed = True


def until_end_of_speech(audio: AudioSource, detector: EndpointDetector) -> EndpointGate:
    return EndpointGate(audio, detector)
