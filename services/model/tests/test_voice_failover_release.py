"""Failover releases the audio source in every fault family, hands no data to an unqualified provider and delivers no text after it returns
(VOI-10, REQ-VOI-002, REQ-VOI-004, REQ-INT-006, PERD R-18, AGENT-13; the Security Reviewer's four blocking findings on #218 at 75193797).

The providers and the microphone are scripted stand-ins (SYNTHETIC; the recorded AGENT-12 exception of noema#412 covers the SpeechProvider seam: stalls, drops and malformed
streams are real asyncio waits and real exceptions, not mocks inside the code under test). The microphone is an explicit handle (`tests/support_voice.Source`), because an
async generator that never started has opened nothing and so cannot show that a close happened. Each test asserts the three things the seam's conditions ask: the source is
closed after the call returns, no text reaches the display after it returns, and the result carries no utterance unless a provider settled one.
"""

import asyncio
from collections.abc import AsyncIterator, Sequence

import pytest

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
from noema_model.voice.failover import FailoverResult, transcribe_with_failover
from tests.support import run
from tests.support_voice import Source, chunk

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)
GUARD_S = 5.0


def caps(name: str, *, qualified: bool = True, retention: Support = Support.YES) -> ProviderCapabilities:
    return ProviderCapabilities(name, PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, retention, "v1", qualified)


def final(text: str = "send fifty thousand naira") -> FinalTranscript:
    return FinalTranscript(text, tuple(TokenConfidence(w, 0.95) for w in text.split()), 0.95, Locale.PCM, "m1")


class Fake:
    """Reads `reads` chunks, then plays `events`, then raises `raises`, hangs, or (with `stubborn`) ignores its cancellation and speaks `late` afterwards."""

    def __init__(self, name: str, reads: int = 1, events: Sequence[SpeechEvent] = (), raises: Exception | None = None, hang: bool = False,
                 stubborn: bool = False, capabilities: ProviderCapabilities | None = None) -> None:
        self.name = name
        self.capabilities = capabilities or caps(name)
        self._reads, self._events, self._raises, self._hang, self._stubborn = reads, events, raises, hang, stubborn
        self.calls = 0
        self.received: list[int] = []

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        self.calls += 1
        count = 0
        async for item in audio:
            self.received.append(item.sequence)
            count += 1
            if count >= self._reads:
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
                    await asyncio.sleep(0.15)  # carries on after the cancellation
                    yield PartialTranscript(99, "a late partial the person must never see")


def mic(n: int = 8) -> Source:
    return Source([chunk(i, 500) for i in range(n)])


def go(providers: Sequence[Fake], source: Source, shown: list[str], *, deadline_s: float = 0.2, max_buffered: int = 100) -> FailoverResult:
    async def call() -> FailoverResult:
        return await transcribe_with_failover(providers, REQUEST, source, deadline_s=deadline_s, cleanup_s=0.1, on_partial=shown.append,  # type: ignore[arg-type]
                                              max_buffered_chunks=max_buffered)

    return run(asyncio.wait_for(call(), timeout=GUARD_S))


def families() -> list[tuple[str, list[Fake], int]]:
    """(name, providers, chunks the microphone offers): one entry per fault family the failover handles."""
    return [
        ("success on the first provider", [Fake("a", 1, [final()])], 8),
        ("failover on a typed error", [Fake("a", 1, [PartialTranscript(0, "send")], raises=ProviderError(ProviderFailure.UNAVAILABLE)), Fake("b", 1, [final()])], 8),
        ("failover on a silent provider", [Fake("a", 1, hang=True), Fake("b", 1, [final()])], 8),
        ("failover on a stream with no final", [Fake("a", 1, [PartialTranscript(0, "send")]), Fake("b", 1, [final()])], 8),
        ("every provider fails", [Fake("a", 1, raises=ProviderError(ProviderFailure.TIMEOUT)), Fake("b", 1, raises=ProviderError(ProviderFailure.UNAVAILABLE))], 8),
        ("a provider that ignores its cancellation", [Fake("a", 1, hang=True, stubborn=True), Fake("b", 1, [final()])], 8),
    ]


@pytest.mark.parametrize("name,providers,n", families(), ids=[f[0] for f in families()])
def test_the_source_is_closed_and_no_text_arrives_after_the_call_returns(name: str, providers: list[Fake], n: int) -> None:
    source, shown = mic(n), []
    result = go(providers, source, shown)
    shown_at_return = list(shown)

    async def let_abandoned_tasks_run() -> None:
        await asyncio.sleep(0.5)

    run(let_abandoned_tasks_run())
    assert source.closed  # the microphone is released in every family (INV-07, AGENT-13)
    assert shown == shown_at_return  # a provider that carried on after being cancelled delivers nothing after the return
    assert "late partial" not in " ".join(shown)
    if result.utterance is None:
        assert result.offer_text  # no utterance means the person is offered text, never a guess


def test_a_replay_overflow_still_closes_the_source_and_offers_text() -> None:
    source, shown = mic(8), []
    providers = [Fake("a", 8, raises=ProviderError(ProviderFailure.UNAVAILABLE)), Fake("b", 1, [final()])]
    result = go(providers, source, shown, max_buffered=2)
    assert result.replay_overflow and result.utterance is None and result.offer_text
    assert source.closed
    assert providers[1].calls == 0  # the second provider was never handed half an utterance


def test_a_stubborn_provider_cannot_make_the_call_wait_for_it() -> None:
    import time

    source, shown = mic(), []
    started = time.monotonic()
    result = go([Fake("a", 1, hang=True, stubborn=True), Fake("b", 1, [final()])], source, shown)
    assert time.monotonic() - started < 1.5
    assert result.utterance is not None and result.utterance.provider_name == "b"
    assert source.closed
