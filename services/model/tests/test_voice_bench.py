"""The latency measurement harness skeleton (VOI-16, REQ-VOI-001, REQ-NFR-003; A241, A180, AGENT-16, INV-21).

One session is one synthetic utterance through the speech-to-text adapter, the core's admission and answer (a Core the caller supplies), and the text-to-speech adapter to the first
audio chunk, timed by an injected clock into the five stages of `latency.py`, so speech-to-text, response and text-to-speech are measured separately. SYNTHETIC utterances only (A180); an
utterance that is not marked synthetic, or is outside the caller's duration bounds, or would exceed the audio budget, is refused before any provider is called. A failure at any stage
leaves an incomplete timeline, which `summarise` counts as the worst case. Providers and clock here are scripted stand-ins (AGENT-12 exception, noema#412); no network is involved.
"""

import asyncio
from collections.abc import AsyncIterator

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec, FinalTranscript, Locale, PartialTranscript, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.tts import SynthesisRequest
from noema_model.voice.bench import AudioBudget, BenchError, Core, SessionResult, SyntheticUtterance, measure_batch, measure_session
from noema_model.voice.latency import Stage
from tests.support import run
from tests.support_voice import CAPS, REQUEST

GUARD_S = 5.0
BOUNDS = (5.0, 15.0)  # the Founder's measurement condition, passed as data


class Clock:
    def __init__(self) -> None:
        self.now = 0

    def __call__(self) -> int:
        return self.now


def frames(seconds: float, rate: int = 16000) -> tuple[AudioChunk, ...]:
    per_frame = rate // 2  # half a second of PCM16 mono per frame: rate/2 samples of 2 bytes is rate bytes
    return tuple(AudioChunk(i, AudioCodec.PCM_S16LE, rate, b"\x10\x00" * per_frame) for i in range(round(seconds * 2)))


def utterance(seconds: float = 8.0, *, synthetic: bool = True, label: str = "pcm-01") -> SyntheticUtterance:
    return SyntheticUtterance(label=label, locale=Locale.PCM, frames=frames(seconds), synthetic=synthetic)


class Stt:
    """Takes `stt_ms` after the last frame is read to produce its final. `fail` raises a ProviderError instead."""

    name = "fake-stt"
    capabilities = CAPS

    def __init__(self, clock: Clock, stt_ms: int = 600, fail: bool = False) -> None:
        self.clock, self.stt_ms, self.fail, self.calls = clock, stt_ms, fail, 0

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        self.calls += 1
        async for _ in audio:
            self.clock.now += 500  # audio plays in real time: the clock moves while frames are read
        if self.fail:
            raise ProviderError(ProviderFailure.UNAVAILABLE)
        self.clock.now += self.stt_ms
        yield PartialTranscript(0, "ok")
        yield FinalTranscript("send it", (TokenConfidence("send", None), TokenConfidence("it", None)), None, Locale.PCM, "pinned-1")


class Tts:
    name = "fake-tts"
    capabilities = CAPS

    def __init__(self, clock: Clock, tts_ms: int = 500, empty: bool = False) -> None:
        self.clock, self.tts_ms, self.empty, self.calls = clock, tts_ms, empty, 0

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        self.calls += 1
        self.clock.now += self.tts_ms
        if not self.empty:
            yield AudioChunk(0, AudioCodec.PCM_S16LE, 48000, b"\x00\x01" * 100)


class FakeCore:
    def __init__(self, clock: Clock, admit_ms: int = 100, answer_ms: int = 300) -> None:
        self.clock, self.admit_ms, self.answer_ms = clock, admit_ms, answer_ms
        self.texts: list[str] = []

    async def admit(self, text: str) -> None:
        self.texts.append(text)
        self.clock.now += self.admit_ms

    async def answer(self) -> str:
        self.clock.now += self.answer_ms
        return "We found your transfer."


async def session(clock: Clock, stt: Stt, tts: Tts, core: Core, item: SyntheticUtterance, budget: AudioBudget | None = None) -> SessionResult:
    return await measure_session(stt=stt, tts=tts, core=core, utterance=item, stt_request=REQUEST, tts_model_version="pinned-1", clock_ms=clock, deadline_s=GUARD_S, bounds_s=BOUNDS,
                                 budget=budget or AudioBudget(total_s=600.0))


def test_each_stage_is_timed_separately_from_end_of_speech_to_first_audio() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), Tts(clock), FakeCore(clock), utterance(8.0)), timeout=GUARD_S))
    assert result.timeline.complete and result.failure is None
    durations = result.timeline.durations()
    assert durations[Stage.FINAL_TRANSCRIPT] == 600  # speech-to-text: from the last audio frame to the final
    assert durations[Stage.INTENT_ADMITTED] == 100 and durations[Stage.ANSWER_TEXT] == 300  # response, in two parts
    assert durations[Stage.FIRST_AUDIO] == 500  # text-to-speech: to the first chunk
    assert result.timeline.total_ms() == 1500


def test_end_of_speech_is_the_last_frame_read_not_the_start_of_the_audio() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), Tts(clock), FakeCore(clock), utterance(10.0)), timeout=GUARD_S))
    assert result.timeline.total_ms() == 1500  # the 10 s the person spoke are not part of the latency


def test_the_core_gets_the_final_transcript_text_and_the_tts_gets_the_cores_answer() -> None:
    clock = Clock()
    core, tts = FakeCore(clock), Tts(clock)
    run(asyncio.wait_for(session(clock, Stt(clock), tts, core, utterance()), timeout=GUARD_S))
    assert core.texts == ["send it"] and tts.calls == 1


@pytest.mark.parametrize("seconds", [4.0, 15.5, 0.5])
def test_an_utterance_outside_the_callers_bounds_is_refused_before_any_provider_is_called(seconds: float) -> None:
    clock = Clock()
    stt, tts = Stt(clock), Tts(clock)
    with pytest.raises(BenchError):
        run(asyncio.wait_for(session(clock, stt, tts, FakeCore(clock), utterance(seconds)), timeout=GUARD_S))
    assert stt.calls == 0 and tts.calls == 0


def test_an_utterance_not_marked_synthetic_is_refused_before_any_provider_is_called() -> None:  # A180: no real speaker audio to any vendor
    clock = Clock()
    stt = Stt(clock)
    with pytest.raises(BenchError):
        run(asyncio.wait_for(session(clock, stt, Tts(clock), FakeCore(clock), utterance(synthetic=False)), timeout=GUARD_S))
    assert stt.calls == 0


def test_a_session_that_would_exceed_the_audio_budget_is_refused_and_the_budget_counts_what_was_sent() -> None:
    clock = Clock()
    budget = AudioBudget(total_s=20.0)
    stt = Stt(clock)
    run(asyncio.wait_for(session(clock, stt, Tts(clock), FakeCore(clock), utterance(8.0), budget), timeout=GUARD_S))
    assert budget.spent_s == 8.0
    with pytest.raises(BenchError):
        run(asyncio.wait_for(session(clock, stt, Tts(clock), FakeCore(clock), utterance(14.0), budget), timeout=GUARD_S))
    assert stt.calls == 1 and budget.spent_s == 8.0


@pytest.mark.parametrize("bad", [0.0, -1.0, float("nan"), float("inf")])
def test_a_budget_that_is_not_positive_and_finite_is_refused(bad: float) -> None:
    with pytest.raises(ValueError):
        AudioBudget(total_s=bad)


def test_a_failure_in_speech_to_text_is_an_incomplete_session_with_its_failure_and_nothing_after_it_runs() -> None:
    clock = Clock()
    core, tts = FakeCore(clock), Tts(clock)
    result = run(asyncio.wait_for(session(clock, Stt(clock, fail=True), tts, core, utterance()), timeout=GUARD_S))
    assert not result.timeline.complete and result.failure is ProviderFailure.UNAVAILABLE
    assert core.texts == [] and tts.calls == 0


def test_text_to_speech_that_yields_no_audio_is_an_incomplete_session() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), Tts(clock, empty=True), FakeCore(clock), utterance()), timeout=GUARD_S))
    assert not result.timeline.complete and result.failure is ProviderFailure.MALFORMED


def test_a_batch_is_summarised_against_the_callers_target_and_a_failure_counts_as_the_worst_case() -> None:
    async def go(fail_count: int) -> tuple[bool, int, int]:
        results = []
        for i in range(20):
            clock = Clock()
            results.append(await session(clock, Stt(clock, fail=i < fail_count), Tts(clock), FakeCore(clock), utterance(8.0, label=f"u{i}")))
        summary = measure_batch(results, target_ms=1800)
        return summary.meets_target, summary.n_complete, summary.n_incomplete

    assert run(asyncio.wait_for(go(0), timeout=GUARD_S)) == (True, 20, 0)
    assert run(asyncio.wait_for(go(2), timeout=GUARD_S)) == (False, 18, 2)  # two losses move the 95th percentile to the worst case


def test_a_batch_with_no_sessions_is_refused() -> None:
    with pytest.raises(ValueError):
        measure_batch([], target_ms=1800)
