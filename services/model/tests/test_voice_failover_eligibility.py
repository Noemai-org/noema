"""Failover never hands audio to a provider that is not qualified or whose data terms are unconfirmed (VOI-10, REQ-INT-006, PERD R-18, AGENTS section 10, A180;
the Security Reviewer's blocking finding 4 on #218 at 75193797).

`transcribe_with_failover` replays the whole buffered utterance to every provider in its list. A provider whose capabilities say `qualified` is false, or whose
`retention_terms` is not `Support.YES`, must never receive a chunk: it is skipped before it is called, the skip is listed in the result (so the caller can record it), and
a call with no eligible provider offers text. The source is still released. Providers and the microphone are scripted stand-ins (SYNTHETIC; AGENT-12 exception on noema#412).
A source that cannot be weakly referenced is refused at the boundary, as in `transcribe_utterance`.
"""

import asyncio
from collections.abc import AsyncIterator, Sequence

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    FinalTranscript,
    Locale,
    PipelineMode,
    ProviderCapabilities,
    SpeechEvent,
    StreamRequest,
    Support,
    TokenConfidence,
)
from noema_model.voice.failover import FailoverResult, transcribe_with_failover
from tests.support import run
from tests.support_voice import Source, chunk

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)


def caps(name: str, *, qualified: bool = True, retention: Support = Support.YES) -> ProviderCapabilities:
    return ProviderCapabilities(name, PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, retention, "v1", qualified)


class Fake:
    def __init__(self, name: str, capabilities: ProviderCapabilities) -> None:
        self.name = name
        self.capabilities = capabilities
        self.calls = 0
        self.received: list[int] = []

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        self.calls += 1
        async for item in audio:
            self.received.append(item.sequence)
            break
        yield FinalTranscript("send ten", (TokenConfidence("send", 0.95), TokenConfidence("ten", 0.95)), 0.95, Locale.PCM, "m1")


def mic(n: int = 4) -> Source:
    return Source([chunk(i, 500) for i in range(n)])


def go(providers: Sequence[Fake], source: object) -> FailoverResult:
    async def call() -> FailoverResult:
        return await transcribe_with_failover(providers, REQUEST, source, deadline_s=0.5, cleanup_s=0.1, on_partial=lambda _: None, max_buffered_chunks=10, allow_replay=True)  # type: ignore[arg-type]

    return run(asyncio.wait_for(call(), timeout=5.0))


INELIGIBLE = [
    pytest.param(caps("x", qualified=False), id="not qualified"),
    pytest.param(caps("x", retention=Support.NO), id="retention terms refused"),
    pytest.param(caps("x", retention=Support.UNVERIFIED), id="retention terms unverified"),
    pytest.param(caps("x", qualified=False, retention=Support.UNVERIFIED), id="neither"),
]


@pytest.mark.parametrize("capabilities", INELIGIBLE)
def test_an_ineligible_provider_is_never_called_and_never_given_a_chunk(capabilities: ProviderCapabilities) -> None:
    bad, good = Fake("bad", capabilities), Fake("good", caps("good"))
    source = mic()
    result = go([bad, good], source)
    assert bad.calls == 0 and bad.received == []  # not one chunk of the utterance reached it
    assert result.utterance is not None and result.utterance.provider_name == "good"
    assert [(s.provider_name, s.skipped) for s in result.steps] == [("bad", True)]  # the skip is listed, so the caller can record it
    assert source.closed


def test_an_ineligible_provider_after_a_failure_is_still_skipped() -> None:
    from noema_model.providers.speech.errors import ProviderError, ProviderFailure

    class Breaks(Fake):
        async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
            self.calls += 1
            async for item in audio:
                self.received.append(item.sequence)
                break
            raise ProviderError(ProviderFailure.UNAVAILABLE)
            yield  # pragma: no cover  (makes this an async generator)

    first, bad = Breaks("first", caps("first")), Fake("bad", caps("bad", qualified=False))
    result = go([first, bad], mic())
    assert first.calls == 1 and bad.calls == 0 and bad.received == []
    assert result.utterance is None and result.offer_text
    assert [(s.provider_name, s.skipped) for s in result.steps] == [("first", False), ("bad", True)]


@pytest.mark.parametrize("capabilities", INELIGIBLE)
def test_with_no_eligible_provider_the_person_is_offered_text_and_the_source_is_released(capabilities: ProviderCapabilities) -> None:
    only = Fake("only", capabilities)
    source = mic()
    result = go([only], source)
    assert only.calls == 0 and only.received == []
    assert result.utterance is None and result.offer_text and not result.replay_overflow
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


def test_a_source_that_cannot_be_weakly_referenced_is_refused_at_the_boundary_and_closed() -> None:
    source = Slotted()
    with pytest.raises(TypeError, match="weak reference"):
        go([Fake("a", caps("a"))], source)
    assert source.closed
