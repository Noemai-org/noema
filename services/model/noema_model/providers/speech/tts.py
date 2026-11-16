"""Provider-neutral text-to-speech (VOI-04, REQ-VOI-002, BD-16, INV-29).

A synthesizer turns the text of an answer into audio chunks and nothing else. Whether it may be used for a locale is
decided from the capability matrix (voice/answer.py), never from this interface.
"""

from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Protocol, runtime_checkable

from noema_model.providers.speech.base import AudioChunk, Locale, ProviderCapabilities


@dataclass(frozen=True, slots=True)
class SynthesisRequest:
    text: str
    locale: Locale
    model_version: str

    def __post_init__(self) -> None:
        if not self.text.strip():
            raise ValueError("an answer to speak has no text")


@runtime_checkable
class SpeechSynthesizer(Protocol):
    name: str
    capabilities: ProviderCapabilities

    def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        """Yield the audio of `request.text`. Raise ProviderError for a timeout, a refused connection or a malformed
        vendor message; a stream that ends with no audio is the caller's failure to judge, never silence-as-success."""
        ...
