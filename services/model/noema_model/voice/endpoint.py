"""End-of-speech detection on decoded PCM (VOI-02, REQ-VOI-001).

A deterministic energy detector: no clock, no I/O, time measured only from the samples it is fed (INV-21). It decides when
to stop sending audio so the provider can settle a final transcript. It never decides what was said.

Its thresholds are arguments. They are tuned in the lab and in the field (VOI-16, REQ-VOI-005), so none is written here
(AGENT-16). It measures PCM only: the phone sends Opus (PERD §35.1) and decoding that needs a codec dependency, which
needs an AGENT-14 entry first. Opus audio is refused rather than guessed at.
"""

import math
import sys
from array import array
from collections.abc import AsyncGenerator, AsyncIterator
from dataclasses import dataclass
from enum import StrEnum

from noema_model.providers.speech.base import AudioChunk, AudioCodec

_BYTES_PER_SAMPLE = 2
_MS_PER_S = 1000


@dataclass(frozen=True, slots=True)
class VadConfig:
    speech_rms: int  # RMS of 16-bit samples at or above which a chunk counts as speech
    min_speech_ms: int  # speech needed before an utterance has started: a click is not speech
    hangover_ms: int  # silence after speech that ends the utterance

    def __post_init__(self) -> None:
        if min(self.speech_rms, self.min_speech_ms, self.hangover_ms) < 1:
            raise ValueError("VAD thresholds must be positive")


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
    def __init__(self, config: VadConfig) -> None:
        self._config = config
        self._speech_ms = 0.0
        self._silence_ms = 0.0
        self._state = EndpointState.WAITING

    def feed(self, chunk: AudioChunk) -> EndpointState:
        if chunk.codec is not AudioCodec.PCM_S16LE:
            raise ValueError("end-of-speech detection needs decoded PCM")
        if chunk.sample_rate_hz < 1 or not chunk.data or len(chunk.data) % _BYTES_PER_SAMPLE:
            raise ValueError("audio chunk is empty or not whole 16-bit samples")
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


async def until_end_of_speech(audio: AsyncIterator[AudioChunk], detector: EndpointDetector) -> AsyncGenerator[AudioChunk, None]:
    """Pass audio through up to and including the chunk that ends the utterance, then stop."""
    async for chunk in audio:
        state = detector.feed(chunk)
        yield chunk
        if state is EndpointState.ENDED:
            return
