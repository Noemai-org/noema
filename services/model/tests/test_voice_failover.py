"""Provider failover mid-utterance and barge-in tests (VOI-10, REQ-VOI-002, PERD §35.3).

Providers are scripted fakes. No vendor wire format is assumed.
"""

import asyncio
from collections.abc import AsyncGenerator, AsyncIterator, Sequence

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
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
from noema_model.voice.bargein import BargeInMonitor, BargeInResult, play_until_barge_in
from noema_model.voice.endpoint import VadConfig
from noema_model.voice.failover import FailoverResult, transcribe_with_failover
from tests.support import run

REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)


def caps(name: str) -> ProviderCapabilities:
    return ProviderCapabilities(name, PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, Support.YES, "v1", True)


def final(text: str = "send fifty thousand naira") -> FinalTranscript:
    return FinalTranscript(text, tuple(TokenConfidence(w, 0.95) for w in text.split()), 0.95, Locale.PCM, "m1")


def chunk(i: int, level: int = 500) -> AudioChunk:
    return AudioChunk(i, AudioCodec.PCM_S16LE, 16000, level.to_bytes(2, "little", signed=True) * 320)


async def audio_of(chunks: Sequence[AudioChunk]) -> AsyncGenerator[AudioChunk, None]:
    for item in chunks:
        yield item


class Fake:
    """Reads `reads` chunks, then plays `events`, then raises `raises` or hangs. Records every chunk it received."""

    def __init__(self, name: str, reads: int, events: Sequence[SpeechEvent] = (), raises: Exception | None = None,
                 hang: bool = False) -> None:
        self.name = name
        self.capabilities = caps(name)
        self._reads, self._events, self._raises, self._hang = reads, events, raises, hang
        self.received: list[int] = []

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
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
            await asyncio.Event().wait()


def go(providers: Sequence[Fake], chunks: int = 8, shown: list[str] | None = None, deadline_s: float = 5.0,
       max_buffered: int = 100) -> FailoverResult:
    sink = shown if shown is not None else []
    return run(transcribe_with_failover(providers, REQUEST, audio_of([chunk(i) for i in range(chunks)]),
                                        deadline_s=deadline_s, on_partial=sink.append, max_buffered_chunks=max_buffered))


def test_the_first_provider_answering_ends_the_call_with_no_failover() -> None:
    first, second = Fake("a", 2, [final()]), Fake("b", 2, [final("other")])
    result = go([first, second])
    assert result.utterance is not None and result.utterance.text == "send fifty thousand naira"
    assert result.utterance.provider_name == "a"
    assert result.steps == () and result.offer_text is False
    assert second.received == []


@pytest.mark.parametrize("kind", list(ProviderFailure))
def test_any_provider_failure_mid_utterance_moves_to_the_next_provider(kind: ProviderFailure) -> None:
    first = Fake("a", 3, [PartialTranscript(0, "send fi")], raises=ProviderError(kind))
    second = Fake("b", 8, [final()])
    result = go([first, second])
    assert result.utterance is not None and result.utterance.provider_name == "b"
    assert [(s.provider_name, s.failure) for s in result.steps] == [("a", kind)]
    assert result.offer_text is False


def test_the_next_provider_gets_the_whole_utterance_from_the_start_not_only_what_is_left() -> None:
    first = Fake("a", 3, raises=ProviderError(ProviderFailure.TIMEOUT))
    second = Fake("b", 8, [final()])
    go([first, second])
    assert first.received == [0, 1, 2]
    assert second.received == list(range(8))


def test_a_provider_that_goes_silent_fails_over_at_its_own_deadline() -> None:
    result = go([Fake("a", 1, hang=True), Fake("b", 8, [final()])], deadline_s=0.05)
    assert result.utterance is not None and result.utterance.provider_name == "b"
    assert result.steps[0].failure is ProviderFailure.TIMEOUT


def test_a_stream_with_partials_and_no_final_fails_over_and_the_partial_is_never_the_result() -> None:
    shown: list[str] = []
    result = go([Fake("a", 2, [PartialTranscript(0, "send five thousand")]), Fake("b", 8, [final("send fifty thousand")])],
                shown=shown)
    assert result.utterance is not None and result.utterance.text == "send fifty thousand"
    assert result.steps[0].failure is ProviderFailure.MALFORMED


def test_when_every_provider_fails_the_person_is_offered_text_and_every_failure_is_listed() -> None:
    providers = [Fake("a", 1, raises=ProviderError(ProviderFailure.TIMEOUT)),
                 Fake("b", 1, raises=ProviderError(ProviderFailure.UNAVAILABLE)),
                 Fake("c", 1, raises=ProviderError(ProviderFailure.RATE_LIMITED))]
    result = go(providers)
    assert result.utterance is None and result.offer_text is True
    assert [(s.provider_name, s.failure) for s in result.steps] == [
        ("a", ProviderFailure.TIMEOUT), ("b", ProviderFailure.UNAVAILABLE), ("c", ProviderFailure.RATE_LIMITED)]


def test_no_provider_is_tried_twice() -> None:
    first = Fake("a", 1, raises=ProviderError(ProviderFailure.TIMEOUT))
    result = go([first])
    assert result.offer_text is True and first.received == [0]


def test_no_providers_is_a_configuration_error() -> None:
    with pytest.raises(ValueError):
        go([])


def test_an_utterance_longer_than_the_replay_buffer_cannot_fail_over_and_offers_text() -> None:
    first = Fake("a", 6, raises=ProviderError(ProviderFailure.TIMEOUT))
    second = Fake("b", 8, [final()])
    result = go([first, second], max_buffered=4)
    assert result.utterance is None and result.offer_text is True
    assert second.received == []  # it was never handed a partial utterance
    assert result.replay_overflow is True
    assert [(s.provider_name, s.failure) for s in result.steps] == [("a", ProviderFailure.TIMEOUT)]


def test_the_buffer_limit_must_be_positive() -> None:
    with pytest.raises(ValueError):
        go([Fake("a", 1, [final()])], max_buffered=0)


# ---- barge-in ------------------------------------------------------------------------------------------------------

VAD = VadConfig(speech_rms=300, min_speech_ms=40, hangover_ms=100)


def frames(levels: Sequence[int]) -> list[AudioChunk]:
    return [chunk(i, level) for i, level in enumerate(levels)]


def play(tts_frames: int, inbound_levels: Sequence[int], max_stop_ms: int = 300) -> tuple[BargeInResult, list[int]]:
    played: list[int] = []
    result = run(play_until_barge_in(audio_of([chunk(i, 1000) for i in range(tts_frames)]), audio_of(frames(inbound_levels)),
                                     BargeInMonitor(VAD, max_stop_ms=max_stop_ms), lambda c: played.append(c.sequence)))
    return result, played


def test_tts_plays_to_the_end_when_the_person_stays_quiet() -> None:
    result, played = play(6, [5] * 6)
    assert result.barged_in is False and played == list(range(6))


def test_tts_stops_at_the_frame_where_speech_is_established() -> None:
    result, played = play(10, [5, 5, 2000, 2000, 2000, 2000, 2000, 2000, 2000, 2000])
    assert result.barged_in is True
    assert played == [0, 1, 2, 3]  # 40 ms of speech needs two 20 ms frames: frames 2 and 3
    assert result.stopped_after_ms == 40


def test_a_click_does_not_stop_the_answer() -> None:
    result, played = play(8, [5, 2000, 5, 5, 5, 5, 5, 5])
    assert result.barged_in is False and played == list(range(8))


def test_a_barge_in_that_would_take_longer_than_the_limit_is_a_configuration_error() -> None:
    with pytest.raises(ValueError):
        BargeInMonitor(VadConfig(speech_rms=300, min_speech_ms=400, hangover_ms=100), max_stop_ms=300)


def test_the_limit_must_be_positive() -> None:
    with pytest.raises(ValueError):
        BargeInMonitor(VAD, max_stop_ms=0)
