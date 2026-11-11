"""Streaming voice pipeline tests (VOI-02, REQ-VOI-001, REQ-VOI-002, REQ-AMB-007, EC-V04).

The provider is a scripted fake that speaks the SpeechProvider stream contract. No vendor is involved and no wire format
is assumed. The faults are the ones PERD §35.3 lists: a partial that disagrees with the final, a stream with no final,
a provider failure, silence and a stream that outlives its final.
"""

import asyncio
import struct
from collections.abc import AsyncGenerator, AsyncIterator, Sequence

import pytest

from noema_model.providers.llm.base import ProviderError, ProviderFailure
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
from noema_model.voice.endpoint import EndpointDetector, EndpointState, VadConfig, until_end_of_speech
from noema_model.voice.pipeline import FinalUtterance, transcribe_utterance
from tests.support import run

REQUEST = StreamRequest(model_version="pinned-2026-01-01", locale_hint=Locale.PCM)
CAPS = ProviderCapabilities("fake", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, Support.YES, "pinned", True)
DEADLINE_S = 5.0
SHORT_DEADLINE_S = 0.05


def final(text: str = "send fifty thousand naira", conf: float | None = 0.93) -> FinalTranscript:
    tokens = tuple(TokenConfidence(word, conf) for word in text.split())
    return FinalTranscript(text, tokens, conf, Locale.PCM, "reported-1")


class ScriptedProvider:
    """Plays a fixed list of events, then ends. `raises` is raised after the events; `hang` waits forever after them."""

    name = "fake"
    capabilities = CAPS

    def __init__(self, events: Sequence[SpeechEvent], raises: Exception | None = None, hang: bool = False) -> None:
        self._events = events
        self._raises = raises
        self._hang = hang
        self.chunks_read = 0
        self.closed_early = False

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        try:
            async for _ in audio:
                self.chunks_read += 1
                break
            for event in self._events:
                yield event
            if self._raises is not None:
                raise self._raises
            if self._hang:
                await asyncio.Event().wait()
        except GeneratorExit:
            self.closed_early = True
            raise


def pcm(samples: Sequence[int]) -> bytes:
    return struct.pack(f"<{len(samples)}h", *samples)


def chunk(sequence: int, level: int, ms: int = 20, rate: int = 16000) -> AudioChunk:
    count = rate * ms // 1000
    return AudioChunk(sequence, AudioCodec.PCM_S16LE, rate, pcm([level] * count))


async def audio_of(chunks: Sequence[AudioChunk]) -> AsyncGenerator[AudioChunk, None]:
    for item in chunks:
        yield item


def transcribe(provider: ScriptedProvider, shown: list[str] | None = None, deadline_s: float = DEADLINE_S) -> FinalUtterance:
    sink = shown if shown is not None else []
    return run(transcribe_utterance(provider, REQUEST, audio_of([chunk(0, 500)] * 1), deadline_s=deadline_s,
                                    on_partial=sink.append))


# ---- partial versus final (EC-V04) -----------------------------------------------------------------------------------


def test_only_the_final_transcript_becomes_the_utterance_and_partials_go_to_the_display() -> None:
    shown: list[str] = []
    provider = ScriptedProvider([PartialTranscript(0, "send fi"), PartialTranscript(1, "send five thousand"),
                                 final("send fifty thousand naira")])
    utterance = transcribe(provider, shown)
    assert utterance.text == "send fifty thousand naira"
    assert shown == ["send fi", "send five thousand"]
    assert "five" not in utterance.text


def test_a_partial_that_would_change_the_amount_never_reaches_the_utterance() -> None:
    provider = ScriptedProvider([PartialTranscript(0, "pay 5,000"), final("pay 50,000")])
    assert transcribe(provider).text == "pay 50,000"


def test_a_stream_with_partials_and_no_final_is_a_failure_not_a_result() -> None:
    shown: list[str] = []
    provider = ScriptedProvider([PartialTranscript(0, "send five thousand")])
    with pytest.raises(ProviderError) as caught:
        transcribe(provider, shown)
    assert caught.value.kind is ProviderFailure.MALFORMED
    assert shown == ["send five thousand"]  # shown to the person, never promoted


def test_an_empty_stream_is_a_failure() -> None:
    with pytest.raises(ProviderError):
        transcribe(ScriptedProvider([]))


def test_a_final_with_no_text_is_a_failure_not_an_empty_utterance() -> None:
    blank = FinalTranscript("   ", (), 0.9, None, None)
    with pytest.raises(ProviderError) as caught:
        transcribe(ScriptedProvider([blank]))
    assert caught.value.kind is ProviderFailure.MALFORMED


def test_partials_that_go_backwards_are_malformed() -> None:
    provider = ScriptedProvider([PartialTranscript(2, "a"), PartialTranscript(1, "b"), final()])
    with pytest.raises(ProviderError) as caught:
        transcribe(provider)
    assert caught.value.kind is ProviderFailure.MALFORMED


def test_an_utterance_cannot_be_made_from_a_partial() -> None:
    with pytest.raises(TypeError):
        FinalUtterance.from_final("fake", PartialTranscript(0, "send five"))  # type: ignore[arg-type]


# ---- confidences carry through untouched -------------------------------------------------------------------------------


def test_token_confidences_and_provenance_carry_through_and_a_missing_one_stays_missing() -> None:
    transcript = FinalTranscript("send ten", (TokenConfidence("send", 0.99), TokenConfidence("ten", None)), None,
                                 Locale.EN_NG, "reported-1")
    utterance = transcribe(ScriptedProvider([transcript]))
    assert utterance.token_confidences == (0.99, None)
    assert utterance.utterance_confidence is None
    assert utterance.provider_name == "fake"
    assert utterance.model_version == "reported-1"
    assert utterance.locale is Locale.EN_NG


# ---- provider failures and silence ----------------------------------------------------------------------------------------


@pytest.mark.parametrize("kind", [ProviderFailure.TIMEOUT, ProviderFailure.UNAVAILABLE, ProviderFailure.RATE_LIMITED])
def test_a_provider_failure_after_partials_yields_no_utterance(kind: ProviderFailure) -> None:
    provider = ScriptedProvider([PartialTranscript(0, "send five")], raises=ProviderError(kind))
    with pytest.raises(ProviderError) as caught:
        transcribe(provider)
    assert caught.value.kind is kind


def test_a_provider_that_goes_silent_is_a_timeout_at_the_deadline() -> None:
    provider = ScriptedProvider([PartialTranscript(0, "send")], hang=True)
    with pytest.raises(ProviderError) as caught:
        transcribe(provider, deadline_s=SHORT_DEADLINE_S)
    assert caught.value.kind is ProviderFailure.TIMEOUT


def test_the_provider_stream_is_closed_once_the_final_arrives() -> None:
    provider = ScriptedProvider([final(), PartialTranscript(9, "late partial")])
    assert transcribe(provider).text == "send fifty thousand naira"
    assert provider.closed_early


def test_audio_is_not_read_past_what_the_provider_asked_for() -> None:
    provider = ScriptedProvider([final()])
    chunks = [chunk(index, 500) for index in range(10)]
    run(transcribe_utterance(provider, REQUEST, audio_of(chunks), deadline_s=DEADLINE_S, on_partial=lambda _: None))
    assert provider.chunks_read == 1


# ---- end-of-speech detection ---------------------------------------------------------------------------------------------

VAD = VadConfig(speech_rms=300, min_speech_ms=60, hangover_ms=100)


def states(detector: EndpointDetector, chunks: Sequence[AudioChunk]) -> list[EndpointState]:
    return [detector.feed(item) for item in chunks]


def test_silence_alone_never_starts_or_ends_an_utterance() -> None:
    detector = EndpointDetector(VAD)
    assert set(states(detector, [chunk(i, 5) for i in range(50)])) == {EndpointState.WAITING}


def test_speech_then_enough_silence_ends_exactly_at_the_hangover() -> None:
    detector = EndpointDetector(VAD)
    sequence = [chunk(i, 2000) for i in range(5)] + [chunk(5 + i, 5) for i in range(5)]
    result = states(detector, sequence)
    assert result[:5] == [EndpointState.WAITING, EndpointState.WAITING, EndpointState.SPEAKING,
                          EndpointState.SPEAKING, EndpointState.SPEAKING]
    assert result[5:9] == [EndpointState.SPEAKING] * 4  # 80 ms of silence is under the 100 ms hangover
    assert result[9] is EndpointState.ENDED  # the fifth silent chunk makes 100 ms


def test_a_short_pause_inside_speech_does_not_end_it() -> None:
    detector = EndpointDetector(VAD)
    sequence = [chunk(0, 2000)] * 4 + [chunk(4, 5)] * 2 + [chunk(6, 2000)] * 4 + [chunk(10, 5)] * 5
    result = states(detector, sequence)
    assert EndpointState.ENDED not in result[:10]
    assert result[-1] is EndpointState.ENDED


def test_a_click_shorter_than_the_minimum_speech_is_not_speech() -> None:
    detector = EndpointDetector(VAD)
    result = states(detector, [chunk(0, 2000), *[chunk(1 + i, 5) for i in range(20)]])
    assert set(result) == {EndpointState.WAITING}


def test_two_clicks_separated_by_silence_do_not_add_up_to_speech() -> None:
    """C2 P3 on #193: deleting the reset of the speech counter left every test green."""
    detector = EndpointDetector(VAD)
    result = states(detector, [chunk(0, 2000), chunk(1, 5), chunk(2, 2000), chunk(3, 2000)])
    assert set(result) == {EndpointState.WAITING}


def test_the_detector_stays_ended() -> None:
    detector = EndpointDetector(VAD)
    states(detector, [chunk(i, 2000) for i in range(4)] + [chunk(4 + i, 5) for i in range(5)])
    assert detector.feed(chunk(9, 2000)) is EndpointState.ENDED


def test_the_detector_refuses_audio_it_cannot_measure() -> None:
    detector = EndpointDetector(VAD)
    with pytest.raises(ValueError):
        detector.feed(AudioChunk(0, AudioCodec.OPUS, 16000, b"\x00" * 40))
    with pytest.raises(ValueError):
        detector.feed(AudioChunk(0, AudioCodec.PCM_S16LE, 16000, b"\x00" * 3))
    with pytest.raises(ValueError):
        detector.feed(AudioChunk(0, AudioCodec.PCM_S16LE, 0, b"\x00" * 4))
    with pytest.raises(ValueError):
        detector.feed(AudioChunk(0, AudioCodec.PCM_S16LE, 16000, b""))


@pytest.mark.parametrize("speech_rms,min_speech_ms,hangover_ms", [(0, 60, 100), (300, 0, 100), (300, 60, 0), (-1, 60, 100)])
def test_a_vad_configuration_with_a_zero_or_negative_value_is_refused(speech_rms: int, min_speech_ms: int,
                                                                      hangover_ms: int) -> None:
    with pytest.raises(ValueError):
        VadConfig(speech_rms, min_speech_ms, hangover_ms)


def test_the_gate_stops_the_audio_after_the_chunk_that_ends_the_utterance() -> None:
    sequence = [chunk(i, 2000) for i in range(4)] + [chunk(4 + i, 5) for i in range(5)] + [chunk(9, 2000)] * 3
    gated = run(collect(until_end_of_speech(audio_of(sequence), EndpointDetector(VAD))))
    assert [item.sequence for item in gated] == list(range(9))


async def collect(stream: AsyncIterator[AudioChunk]) -> list[AudioChunk]:
    return [item async for item in stream]
