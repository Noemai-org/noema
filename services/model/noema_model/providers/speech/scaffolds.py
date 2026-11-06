"""Adapter scaffolds for Aethex AI, Indigenius and N-ATLAS (VOI-01, REQ-VOI-002, BD-16).

These classes are the three places a vendor's wire format will live, and today they hold none. No vendor document, recorded
sample or live endpoint has been read (AGENT-02, REQ-INT-006), so a stream call refuses before it contacts anything, and
the refusal says why. Writing a guessed wire format here would produce confident transcripts from a protocol nobody has
checked, which is the failure this track exists to prevent.

A scaffold becomes an adapter in one change: its vendor document is linked, its decode is written against a recorded
sample, its matrix row turns `qualified`, and the contract tests in tests/test_speech_contract.py run that sample through
validate_event_order.
"""

from collections.abc import AsyncIterator

from noema_model.providers.llm.base import AdapterSettings, ProviderError, ProviderFailure
from noema_model.providers.speech.base import (
    AudioChunk,
    ProviderCapabilities,
    SpeechEvent,
    StreamRequest,
)


class UnqualifiedSpeechAdapter:
    """A named seat for one vendor. It holds the settings an adapter will need and nothing it could call with."""

    def __init__(self, name: str, settings: AdapterSettings, capabilities: ProviderCapabilities) -> None:
        self.name = name
        self.capabilities = capabilities
        self._settings = settings

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        # local=True: this service refused; no provider was contacted, so the failure is not charged to a breaker.
        raise ProviderError(ProviderFailure.REJECTED, local=True)
        yield  # pragma: no cover - makes this an async generator, so the refusal surfaces on iteration like a real stream


class AethexAI(UnqualifiedSpeechAdapter):
    """Aethex AI streaming ASR. Wire format: not yet read."""


class Indigenius(UnqualifiedSpeechAdapter):
    """Indigenius streaming ASR. Wire format: not yet read."""


class NAtlas(UnqualifiedSpeechAdapter):
    """N-ATLAS streaming ASR. Wire format: not yet read."""


