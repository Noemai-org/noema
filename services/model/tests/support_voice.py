"""Shared voice test doubles (VOI-02): a microphone handle, a loud-then-silent clip and the pinned request. Synthetic
audio only; no recording is involved (AGENT-25)."""

from collections.abc import Sequence

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    Locale,
    PipelineMode,
    ProviderCapabilities,
    StreamRequest,
    Support,
)
from noema_model.voice.endpoint import VadConfig

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)
CAPS = ProviderCapabilities("fake", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, Support.YES, "v1", True)
VAD = VadConfig(speech_rms=300, min_speech_ms=60, hangover_ms=100)


def chunk(i: int, level: int) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, level.to_bytes(2, "little", signed=True) * 320)


class Source:
    """A microphone stand-in with an explicit handle: `aclose()` releases it whether or not anything was read, as a real
    device handle does. (An async generator that never started has opened nothing, so it is not a faithful stand-in.)"""

    def __init__(self, chunks: Sequence[AudioChunk]) -> None:
        self._items = iter(chunks)
        self.closed = False

    def stream(self) -> "Source":
        return self

    def __aiter__(self) -> "Source":
        return self

    async def __anext__(self) -> AudioChunk:
        try:
            return next(self._items)
        except StopIteration:
            raise StopAsyncIteration from None

    async def aclose(self) -> None:
        self.closed = True


def loud_then_silent() -> list[AudioChunk]:
    return [chunk(i, 2000) for i in range(4)] + [chunk(4 + i, 5) for i in range(5)] + [chunk(9, 2000)] * 3
