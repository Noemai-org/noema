"""The text fallback survives an unexpected synthesizer failure (VOI-04, REQ-VOI-002; Codex P2 on #213).

An adapter is meant to normalise every failure to ProviderError, but a runtime or transport error it did not anticipate must
not take the answer's text with it (PERD §35.3: on a TTS outage the overlay shows the text).
"""

from collections.abc import AsyncIterator

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    Locale,
    PipelineMode,
    ProviderCapabilities,
    Support,
)
from noema_model.providers.speech.errors import ProviderFailure
from noema_model.providers.speech.tts import SynthesisRequest
from noema_model.voice.answer import DeliveryMode, deliver_answer
from tests.support import run

CAPS = ProviderCapabilities("fake", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), tuple((loc, Support.YES) for loc in Locale),
                            None, Support.YES, "v1", True)
REQUEST = SynthesisRequest(text="We found your transfer.", locale=Locale.PCM, model_version="v1")


class Exploding:
    name = "fake"
    capabilities = CAPS

    def __init__(self, error: BaseException, after_chunks: int = 0) -> None:
        self._error, self._after = error, after_chunks

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        for i in range(self._after):
            yield AudioChunk(i, AudioCodec.PCM_S16LE, 16000, b"\x01\x00" * 160)
        raise self._error


@pytest.mark.parametrize("error", [RuntimeError("boom"), OSError("socket closed"), KeyError("x"), ValueError("bad")])
@pytest.mark.parametrize("after", [0, 2])
def test_an_unexpected_synthesizer_error_still_delivers_the_text(error: Exception, after: int) -> None:
    result = run(deliver_answer(Exploding(error, after), REQUEST, deadline_s=5.0))
    assert result.mode is DeliveryMode.TEXT_ONLY
    assert result.text == REQUEST.text
    assert result.audio == ()
    assert result.failure is ProviderFailure.UNAVAILABLE
    assert result.error_type == type(error).__name__


def test_the_diagnostic_never_carries_the_error_message() -> None:
    result = run(deliver_answer(Exploding(RuntimeError("secret text of the answer")), REQUEST, deadline_s=5.0))
    assert "secret" not in repr(result).replace(REQUEST.text, "")


def test_a_cancellation_is_not_swallowed() -> None:
    import asyncio

    with pytest.raises(asyncio.CancelledError):
        run(deliver_answer(Exploding(asyncio.CancelledError()), REQUEST, deadline_s=5.0))


# ---- Codex P2 on #213: a synthesizer's own shutdown cannot hold the text fallback back ---------------------------------------------------------------


class SlowToStop:
    """Yields a chunk, then stalls; when it is cancelled its generator's cleanup runs for `cleanup_s`, or for ever if that is None."""

    name = "fake"
    capabilities = CAPS

    def __init__(self, cleanup_s: float | None) -> None:
        self._cleanup_s = cleanup_s

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        import asyncio

        try:
            yield AudioChunk(0, AudioCodec.PCM_S16LE, 16000, b"\x01\x00" * 160)
            await asyncio.Event().wait()
        finally:
            if self._cleanup_s is None:
                await asyncio.Event().wait()
            else:
                await asyncio.sleep(self._cleanup_s)


@pytest.mark.parametrize("cleanup", [0.5, None])
def test_the_text_fallback_does_not_wait_for_the_synthesizers_shutdown(cleanup: float | None) -> None:
    import asyncio
    import time

    async def call() -> tuple[object, float]:
        started = time.monotonic()
        result = await asyncio.wait_for(
            deliver_answer(SlowToStop(cleanup), REQUEST, deadline_s=0.05, cleanup_s=0.05), timeout=5.0)
        return result, time.monotonic() - started

    result, elapsed = run(call())
    assert result.mode is DeliveryMode.TEXT_ONLY and result.text == REQUEST.text and result.audio == ()  # type: ignore[attr-defined]
    assert result.failure is ProviderFailure.TIMEOUT  # type: ignore[attr-defined]
    assert elapsed < 0.4, elapsed  # the deadline plus the cleanup bound, not the synthesizer's own 0.5 s or its endless wait
