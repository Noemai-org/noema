"""A speech failure degrades to text: one provider, no replay, the source always released (VOI-10, REQ-VOI-002, REQ-VOI-004, REQ-INT-006, PERD R-18, AGENT-13; rulings A179, A249).

A249: "There is no replay to a second provider: failover means degrade to text." `transcribe_or_text` makes one attempt with one provider. If that provider is not qualified or its
data terms are not confirmed it is never called and never given a chunk; if it fails, is silent, ends with no final, or ignores its cancellation, the person is offered text, no
utterance is guessed, nothing reaches the display after the call returns, and the live source is closed. Providers and the microphone are SYNTHETIC scripted stand-ins under the
recorded AGENT-12 exception of noema#412 (the SpeechProvider seam; stalls and cancellations are real asyncio waits).
"""

import asyncio
import time
from collections.abc import AsyncIterator

import pytest

from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.speech.base import (
    AudioChunk,
    FinalTranscript,
    Locale,
    PartialTranscript,
    PipelineMode,
    ProviderCapabilities,
    SpeechEvent,
    StreamRequest,
    Support,
    TokenConfidence,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.voice.degrade import DegradeResult, transcribe_or_text
from tests.support import run
from tests.support_voice import Source, chunk

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)
GUARD_S = 5.0


def caps(*, qualified: bool = True, retention: Support = Support.YES) -> ProviderCapabilities:
    return ProviderCapabilities("fake", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, retention, "v1", qualified)


def final() -> FinalTranscript:
    return FinalTranscript("send ten", (TokenConfidence("send", 0.95), TokenConfidence("ten", 0.95)), 0.95, Locale.PCM, "m1")


class Fake:
    """Reads one chunk, then plays `events`, then raises `raises`, hangs, or (with `stubborn`) ignores its cancellation and speaks a late partial."""

    def __init__(self, events: tuple[SpeechEvent, ...] = (), raises: Exception | None = None, hang: bool = False, stubborn: bool = False,
                 capabilities: ProviderCapabilities | None = None) -> None:
        self.name = "fake"
        self.capabilities = capabilities or caps()
        self._events, self._raises, self._hang, self._stubborn = events, raises, hang, stubborn
        self.calls = 0
        self.received: list[int] = []

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        self.calls += 1
        async for item in audio:
            self.received.append(item.sequence)
            break
        for event in self._events:
            yield event
        if self._raises is not None:
            raise self._raises
        if self._hang:
            while True:
                try:
                    await asyncio.Event().wait()
                except asyncio.CancelledError:
                    if not self._stubborn:
                        raise
                    await asyncio.sleep(0.15)
                    yield PartialTranscript(99, "a late partial the person must never see")


def mic(n: int = 8) -> Source:
    return Source([chunk(i, 500) for i in range(n)])


def go(provider: Fake, source: object, shown: list[str] | None = None) -> DegradeResult:
    sink = shown if shown is not None else []

    async def call() -> DegradeResult:
        return await transcribe_or_text(provider, REQUEST, source, deadline_s=0.2, cleanup_s=0.1, on_partial=sink.append)  # type: ignore[arg-type]

    return run(asyncio.wait_for(call(), timeout=GUARD_S))


def test_a_provider_that_answers_gives_the_utterance_and_no_offer_of_text() -> None:
    source = mic()
    result = go(Fake((final(),)), source)
    assert result.utterance is not None and result.utterance.text == "send ten"
    assert not result.offer_text and result.failure is None and not result.skipped
    assert source.closed


FAILURES = [
    ("a typed error", lambda: Fake((PartialTranscript(0, "send"),), raises=ProviderError(ProviderFailure.UNAVAILABLE))),
    ("a timeout error", lambda: Fake(raises=ProviderError(ProviderFailure.TIMEOUT))),
    ("a silent provider", lambda: Fake(hang=True)),
    ("a stream with a partial and no final", lambda: Fake((PartialTranscript(0, "send five"),))),
    ("a provider that ignores its cancellation", lambda: Fake(hang=True, stubborn=True)),
]


@pytest.mark.parametrize("name,make", FAILURES, ids=[f[0] for f in FAILURES])
def test_a_failure_degrades_to_text_with_no_guessed_utterance_the_source_closed_and_nothing_shown_after_return(name: str, make) -> None:  # type: ignore[no-untyped-def]
    source, shown = mic(), list[str]()
    started = time.monotonic()
    result = go(make(), source, shown)
    assert time.monotonic() - started < 1.5  # a stubborn provider is not waited for
    shown_at_return = list(shown)
    run(asyncio.sleep(0.5))  # let abandoned tasks run past the return
    assert result.utterance is None and result.offer_text and result.failure is not None
    assert source.closed
    assert shown == shown_at_return and "late partial" not in " ".join(shown)


def test_a_second_attempt_does_not_exist_so_no_audio_is_replayed() -> None:
    import noema_model.voice.degrade as degrade

    assert not hasattr(degrade, "allow_replay") and not hasattr(degrade, "_ReplayBuffer")
    import importlib.util

    assert importlib.util.find_spec("noema_model.voice.failover") is None  # the replaying module is gone, not merely unused


INELIGIBLE = [
    pytest.param(caps(qualified=False), id="not qualified"),
    pytest.param(caps(retention=Support.NO), id="retention terms refused"),
    pytest.param(caps(retention=Support.UNVERIFIED), id="retention terms unverified"),
    pytest.param(caps(qualified=False, retention=Support.UNVERIFIED), id="neither"),
]


@pytest.mark.parametrize("capabilities", INELIGIBLE)
def test_an_ineligible_provider_is_never_called_and_never_given_a_chunk_and_the_source_is_closed(capabilities: ProviderCapabilities) -> None:
    provider, source = Fake((final(),), capabilities=capabilities), mic()
    result = go(provider, source)
    assert provider.calls == 0 and provider.received == []
    assert result.utterance is None and result.offer_text and result.skipped
    assert source.closed  # a microphone is released even when nothing was allowed to listen


class Slotted:
    """A source that cannot be weakly referenced."""

    __slots__ = ("closed",)

    def __init__(self) -> None:
        self.closed = False

    def __aiter__(self) -> "Slotted":
        return self

    async def __anext__(self) -> AudioChunk:
        raise StopAsyncIteration

    async def aclose(self) -> None:
        self.closed = True


def test_a_source_that_cannot_be_weakly_referenced_is_refused_and_closed() -> None:
    source = Slotted()
    with pytest.raises(TypeError, match="weak reference"):
        go(Fake((final(),)), source)
    assert source.closed


def test_cleanup_after_the_call_is_reported_with_the_correlation_id_and_a_fixed_code() -> None:
    telemetry = InMemoryTelemetry()
    source = Source([chunk(0, 500)])

    async def call() -> DegradeResult:
        result = await transcribe_or_text(Fake(hang=True), REQUEST, source, deadline_s=0.05, cleanup_s=0.05, hold_s=0.05, on_partial=lambda _: None,
                                          telemetry=telemetry, correlation_id="corr-d")
        await asyncio.sleep(0.3)
        return result

    result = run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert result.utterance is None and result.offer_text
    assert all(c == "corr-d" and set(f) == {"code"} for n, c, f in telemetry.events if n == "voice_audio_cleanup")


# ---- Codex P2 on noema#621 at 6d084abd: a caller's cancellation while an ineligible provider's source is being closed still ends in a bounded close ----------------------


class StallsOnClose(Source):
    """A source whose close never returns on its own but honours cancellation, and records that it was cancelled."""

    def __init__(self) -> None:
        super().__init__([])
        self.close_cancelled = False

    async def aclose(self) -> None:
        try:
            await asyncio.Event().wait()
        except asyncio.CancelledError:
            self.close_cancelled = True
            raise


def test_a_cancellation_during_the_release_of_an_ineligible_providers_source_still_cancels_the_stalled_close_at_its_bound() -> None:
    source = StallsOnClose()

    async def call() -> bool:
        task = asyncio.ensure_future(transcribe_or_text(Fake((final(),), capabilities=caps(qualified=False)), REQUEST, source, deadline_s=0.5, cleanup_s=0.1, on_partial=lambda _: None))  # type: ignore[arg-type]
        await asyncio.sleep(0.03)  # the close has started
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        await asyncio.sleep(0.4)  # three cleanup windows: the close must have been given its deadline
        return source.close_cancelled  # read before the loop is torn down, which would cancel the task by itself

    assert run(asyncio.wait_for(call(), timeout=GUARD_S))
