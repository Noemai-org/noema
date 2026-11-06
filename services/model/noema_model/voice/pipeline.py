"""The streaming voice pipeline: audio in, one final utterance out (VOI-02, REQ-VOI-001, REQ-AMB-007, EC-V04).

Partial transcripts exist for the screen. They go to a display callback and nowhere else. The only value this module
hands to the gateway is a FinalUtterance, and the only way to make one is from a FinalTranscript, so a partial cannot set
a slot by any path through this code (PERD §35.3).

Silence is a failure, not a result (INV-07): a stream that ends with no final, a final with no text, a provider error and a
deadline all raise ProviderError, and the caller fails over or offers text. Nothing here retries or fails over (VOI-10).
Raw audio is never stored here; the audio iterator is consumed and closed, and the result carries none of it (AGENT-13).
"""

import asyncio
from collections.abc import AsyncGenerator, Callable
from dataclasses import dataclass

from noema_model.providers.llm.base import ProviderError, ProviderFailure
from noema_model.providers.speech.base import (
    AudioChunk,
    FinalTranscript,
    Locale,
    SpeechProvider,
    StreamRequest,
)


@dataclass(frozen=True, slots=True)
class FinalUtterance:
    """What the gateway may build a canonical voice input from. A missing token confidence stays None; the slot rule
    (voice/confidence.py) counts it as low."""

    text: str
    token_confidences: tuple[float | None, ...]
    utterance_confidence: float | None
    provider_name: str
    model_version: str | None
    locale: Locale | None

    @classmethod
    def from_final(cls, provider_name: str, transcript: FinalTranscript) -> "FinalUtterance":
        if not isinstance(transcript, FinalTranscript):
            raise TypeError("a FinalUtterance is made only from a FinalTranscript")
        if not transcript.text.strip():
            raise ProviderError(ProviderFailure.MALFORMED)
        return cls(
            text=transcript.text,
            token_confidences=tuple(token.confidence for token in transcript.tokens),
            utterance_confidence=transcript.utterance_confidence,
            provider_name=provider_name,
            model_version=transcript.model_version_reported,
            locale=transcript.detected_locale,
        )


async def transcribe_utterance(
    provider: SpeechProvider,
    request: StreamRequest,
    audio: AsyncGenerator[AudioChunk, None],
    *,
    deadline_s: float,
    on_partial: Callable[[str], None],
) -> FinalUtterance:
    """Stream `audio` to `provider` and return its final transcript. `deadline_s` bounds the whole call."""
    last_partial = -1
    stream = provider.transcribe(request, audio)
    try:
        async with asyncio.timeout(deadline_s):
            async for event in stream:
                if isinstance(event, FinalTranscript):
                    return FinalUtterance.from_final(provider.name, event)
                if event.sequence <= last_partial:
                    raise ProviderError(ProviderFailure.MALFORMED)
                last_partial = event.sequence
                on_partial(event.text)
    except TimeoutError:
        raise ProviderError(ProviderFailure.TIMEOUT) from None
    finally:
        await _close(stream)
        await audio.aclose()
    raise ProviderError(ProviderFailure.MALFORMED)  # the stream ended with no final


async def _close(stream: object) -> None:
    close = getattr(stream, "aclose", None)
    if close is not None:
        await close()
