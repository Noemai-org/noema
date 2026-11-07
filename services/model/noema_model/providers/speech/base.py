"""Provider-neutral streaming speech types: audio in, partial and final transcript events out (REQ-VOI-002, BD-16, INV-29).

An adapter turns audio chunks into events and nothing else. It does not decide that a transcript is usable, does not
retry and does not fail over: the voice pipeline does. Two rules are carried by the types themselves, so no caller can
forget them:

* A PartialTranscript has no confidences and is not a FinalTranscript. It is display text; it cannot set a slot
  (REQ-AMB-007, EC-V04).
* A confidence is a ratio in [0, 1] or None. None means the provider did not say, and downstream that counts as low
  (REQ-VOI-003). No type here defaults a confidence to a number.

Audio bytes are excluded from every repr, so a log line or a traceback cannot carry raw audio (AGENT-13, REQ-VOI-004).
"""

from collections.abc import AsyncIterator
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Protocol, runtime_checkable

from noema_model.providers.llm.base import ProviderError, ProviderFailure


class Locale(StrEnum):
    """The five locales of the voice track (SoT §32)."""

    EN_NG = "en-NG"
    PCM = "pcm"
    YO = "yo"
    HA = "ha"
    IG = "ig"


class AudioCodec(StrEnum):
    OPUS = "opus"
    PCM_S16LE = "pcm_s16le"


class Support(StrEnum):
    """What the capability matrix may say about a provider. UNVERIFIED is the only honest value until a test or a vendor
    document says otherwise (OQ-07); it is never treated as YES."""

    YES = "YES"
    NO = "NO"
    UNVERIFIED = "UNVERIFIED"


class PipelineMode(StrEnum):
    """TRANSCRIBE_ONLY providers return text; the reasoning step stays ours. SPEECH_TO_SPEECH providers would reason and
    speak in one model, which hides the transcript and its confidences (REQ-VOI-006)."""

    TRANSCRIBE_ONLY = "TRANSCRIBE_ONLY"
    SPEECH_TO_SPEECH = "SPEECH_TO_SPEECH"


@dataclass(frozen=True, slots=True)
class AudioChunk:
    sequence: int
    codec: AudioCodec
    sample_rate_hz: int
    data: bytes = field(repr=False)


@dataclass(frozen=True, slots=True)
class StreamRequest:
    model_version: str
    locale_hint: Locale | None


def check_ratio(value: float | None) -> None:
    if value is None:
        return
    if isinstance(value, bool) or not 0.0 <= value <= 1.0:
        raise ProviderError(ProviderFailure.MALFORMED)


@dataclass(frozen=True, slots=True)
class TokenConfidence:
    """One recognised token. `confidence` is None when the provider returned none for it."""

    text: str
    confidence: float | None
    start_ms: int | None = None
    end_ms: int | None = None

    def __post_init__(self) -> None:
        check_ratio(self.confidence)


@dataclass(frozen=True, slots=True)
class PartialTranscript:
    """Interim text for the screen. It carries no confidences on purpose: nothing may be derived from it."""

    sequence: int
    text: str


@dataclass(frozen=True, slots=True)
class FinalTranscript:
    """The provider's settled transcript of one utterance, with the per-token confidences the key-slot rule needs."""

    text: str
    tokens: tuple[TokenConfidence, ...]
    utterance_confidence: float | None
    detected_locale: Locale | None
    model_version_reported: str | None

    def __post_init__(self) -> None:
        check_ratio(self.utterance_confidence)


SpeechEvent = PartialTranscript | FinalTranscript


@dataclass(frozen=True, slots=True)
class ProviderCapabilities:
    """What a provider is configured and qualified to do. Built only from the committed matrix (matrix.py)."""

    name: str
    mode: PipelineMode
    streaming_asr: Support
    asr_locales: tuple[tuple[Locale, Support], ...]
    tts_locales: tuple[tuple[Locale, Support], ...]
    latency_ms_p95: int | None
    retention_terms: Support
    pinned_model_version: str | None
    qualified: bool


@runtime_checkable
class SpeechProvider(Protocol):
    name: str
    capabilities: ProviderCapabilities

    def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        """Yield zero or more PartialTranscript events, then at most one FinalTranscript, which is last.

        Raise ProviderError for a timeout, a refused connection, a malformed vendor message or a stream that closes with no
        final: silence is a failure, never an empty final (INV-07). Stop reading `audio` when the caller stops iterating.
        """
        ...


def validate_event_order(events: list[SpeechEvent]) -> None:
    """The stream contract every adapter shares (tests call this on every recorded and synthetic stream). Partial
    sequences strictly increase, a final is the last event and there is at most one. Raises ProviderError(MALFORMED)."""
    last_sequence = -1
    for index, event in enumerate(events):
        if isinstance(event, FinalTranscript):
            if index != len(events) - 1:
                raise ProviderError(ProviderFailure.MALFORMED)
        elif event.sequence <= last_sequence:
            raise ProviderError(ProviderFailure.MALFORMED)
        else:
            last_sequence = event.sequence
