"""The latency measurement harness skeleton (VOI-16, REQ-VOI-001, REQ-NFR-003; A241, A377 to A379, AGENT-16, INV-21).

One session is one synthetic utterance through the speech-to-text adapter, the core's admission and answer (a Core the caller supplies), and the text-to-speech adapter to the first
audio chunk, timed by an injected clock into the five stages of `latency.py`, so speech-to-text, response and text-to-speech are measured separately. Labelled audio only (A377 to A379: SYNTHETIC, or TEAM_RECORDING of a consenting team member); an
utterance that carries neither label, or is outside the caller's duration bounds, or would exceed the audio budget, is refused before any provider is called. A failure at any stage
leaves an incomplete timeline, which `summarise` counts as the worst case. Providers and clock here are scripted stand-ins (AGENT-12 exception, noema#412); no network is involved.
"""

import asyncio
from collections.abc import AsyncIterator

import pytest

from noema_model.providers.speech.base import AudioChunk, AudioCodec, FinalTranscript, Locale, PartialTranscript, SpeechEvent, StreamRequest, TokenConfidence
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.tts import SynthesisRequest
from noema_model.voice.bench import AudioBudget, AudioOrigin, BenchError, Core, SessionResult, Utterance, measure_batch, measure_session
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


def utterance(seconds: float = 8.0, *, origin: AudioOrigin | None = AudioOrigin.SYNTHETIC, label: str = "pcm-01") -> Utterance:
    return Utterance(label=label, locale=Locale.PCM, frames=frames(seconds), origin=origin)  # type: ignore[arg-type]


class Stt:
    """Takes `stt_ms` after the last frame is read to produce its final. `fail` raises a ProviderError instead."""

    name = "fake-stt"
    capabilities = CAPS

    def __init__(self, clock: Clock, stt_ms: int = 600, fail: bool = False, stop_after: int | None = None) -> None:
        self.clock, self.stt_ms, self.fail, self.calls = clock, stt_ms, fail, 0
        self.stop_after = stop_after  # emit the final right after this many frames, without reading on to the end of the stream
        self.paced = 0  # frames whose own length passes on the clock before the last one is handed over (set by the session helper)

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        self.calls += 1
        index = 0
        async for _ in audio:
            if index < self.paced:
                self.clock.now += 500  # audio plays in real time: the clock moves until the last frame is handed over
            index += 1
            if self.stop_after is not None and index == self.stop_after:
                break
        if self.fail:
            raise ProviderError(ProviderFailure.UNAVAILABLE)
        self.clock.now += self.stt_ms
        yield PartialTranscript(0, "ok")
        yield FinalTranscript("send it", (TokenConfidence("send", None), TokenConfidence("it", None)), None, Locale.PCM, "pinned-1")


class Tts:
    name = "fake-tts"
    capabilities = CAPS

    def __init__(self, clock: Clock, tts_ms: int = 500, empty: bool = False, close_ms: int = 0, hang: str | None = None, close_fails: bool = False) -> None:
        self.clock, self.tts_ms, self.empty, self.calls = clock, tts_ms, empty, 0
        self.close_fails = close_fails  # the stream raises a ProviderError while it is closed, after the audio was heard
        self.close_ms, self.hang = close_ms, hang  # hang: "first" (no audio ever) or "close" (the stream cannot be closed)

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        self.calls += 1
        try:
            self.clock.now += self.tts_ms
            if self.hang == "first":
                await asyncio.Event().wait()
            if not self.empty:
                yield AudioChunk(0, AudioCodec.PCM_S16LE, 48000, b"\x00\x01" * 100)
        finally:
            self.clock.now += self.close_ms  # cleanup takes time on the clock
            if self.close_fails:
                raise ProviderError(ProviderFailure.UNAVAILABLE)
            if self.hang == "close":
                await asyncio.Event().wait()


class FakeCore:
    def __init__(self, clock: Clock, admit_ms: int = 100, answer_ms: int = 300, hang: str | None = None) -> None:
        self.clock, self.admit_ms, self.answer_ms, self.hang = clock, admit_ms, answer_ms, hang  # hang: "admit" or "answer"
        self.texts: list[str] = []

    async def admit(self, text: str) -> None:
        self.texts.append(text)
        if self.hang == "admit":
            await asyncio.Event().wait()
        self.clock.now += self.admit_ms

    async def answer(self) -> str:
        if self.hang == "answer":
            await asyncio.Event().wait()
        self.clock.now += self.answer_ms
        return "We found your transfer."


async def session(clock: Clock, stt: Stt, tts: Tts, core: Core, item: Utterance, budget: AudioBudget | None = None) -> SessionResult:
    stt.paced = len(item.frames) - 1
    return await measure_session(stt=stt, tts=tts, core=core, utterance=item, stt_request=REQUEST, tts_model_version="pinned-1", clock_ms=clock, deadline_s=GUARD_S, core_deadline_s=0.15,
                                 tts_deadline_s=0.15, cleanup_s=0.15, bounds_s=BOUNDS, budget=budget or AudioBudget(total_s=600.0))


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


def test_an_utterance_with_no_origin_label_is_refused_before_any_provider_is_called() -> None:  # A377 to A379: audio is labelled SYNTHETIC or TEAM_RECORDING; anything else is never sent
    clock = Clock()
    stt = Stt(clock)
    for bad in (None, "REAL", "synthetic"):
        with pytest.raises(BenchError):
            run(asyncio.wait_for(session(clock, stt, Tts(clock), FakeCore(clock), utterance(origin=bad)), timeout=GUARD_S))  # type: ignore[arg-type]
    assert stt.calls == 0


def test_a_recording_of_a_consenting_team_member_is_measured_like_synthetic_audio_and_keeps_its_label() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), Tts(clock), FakeCore(clock), utterance(origin=AudioOrigin.TEAM_RECORDING)), timeout=GUARD_S))
    assert result.timeline.complete and result.origin is AudioOrigin.TEAM_RECORDING


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


# ---- Codex P2s on noema#694 at 6ef05e82 -----------------------------------------------------------------------------------------------------------------------------------


def test_the_first_audio_mark_does_not_include_the_time_the_stream_takes_to_close() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), Tts(clock, close_ms=400), FakeCore(clock), utterance()), timeout=GUARD_S))
    assert result.timeline.durations()[Stage.FIRST_AUDIO] == 500 and result.timeline.total_ms() == 1500  # cleanup after the first chunk is not latency


def test_end_of_speech_is_marked_when_the_last_frame_is_handed_over_even_if_the_provider_finalises_without_reading_to_the_end() -> None:
    clock = Clock()
    item = utterance(8.0)
    result = run(asyncio.wait_for(session(clock, Stt(clock, stop_after=len(item.frames)), Tts(clock), FakeCore(clock), item), timeout=GUARD_S))
    assert result.timeline.durations()[Stage.FINAL_TRANSCRIPT] == 600  # the stream was never exhausted; the stage still starts at the last frame


@pytest.mark.parametrize("which", ["admit", "answer"])
def test_a_core_that_never_answers_ends_the_session_at_its_deadline_as_a_timeout(which: str) -> None:
    clock = Clock()
    tts = Tts(clock)
    result = run(asyncio.wait_for(session(clock, Stt(clock), tts, FakeCore(clock, hang=which), utterance()), timeout=GUARD_S))
    assert not result.timeline.complete and result.failure is ProviderFailure.TIMEOUT and tts.calls == 0


def test_text_to_speech_that_never_yields_audio_ends_the_session_at_its_deadline_as_a_timeout() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), Tts(clock, hang="first"), FakeCore(clock), utterance()), timeout=GUARD_S))
    assert not result.timeline.complete and result.failure is ProviderFailure.TIMEOUT


def test_a_stream_that_cannot_be_closed_does_not_hold_a_complete_session_past_the_cleanup_bound() -> None:
    clock = Clock()

    async def go() -> tuple[bool, float]:
        started = asyncio.get_running_loop().time()
        result = await session(clock, Stt(clock), Tts(clock, hang="close"), FakeCore(clock), utterance())
        return result.timeline.complete, asyncio.get_running_loop().time() - started

    complete, elapsed = run(asyncio.wait_for(go(), timeout=GUARD_S))
    assert complete and elapsed < 1.5  # the first chunk was heard; the close is abandoned at the cleanup bound


# ---- Codex P2 on noema#694 at a0b3d82a: a session with a recorded failure is never counted as complete in the batch ------------------------------------------------------------


def test_a_session_whose_cleanup_failed_after_the_first_audio_counts_as_a_failure_in_the_batch() -> None:
    async def go(failing: int) -> tuple[bool, int, int]:
        results = []
        for i in range(20):
            clock = Clock()
            results.append(await session(clock, Stt(clock), Tts(clock, close_fails=i < failing), FakeCore(clock), utterance(8.0, label=f"u{i}")))
        assert all(r.failure is ProviderFailure.UNAVAILABLE for r in results[:failing])  # the failure is recorded although the timeline reached its last stage
        summary = measure_batch(results, target_ms=1800)
        return summary.meets_target, summary.n_complete, summary.n_incomplete

    assert run(asyncio.wait_for(go(1), timeout=GUARD_S)) == (True, 19, 1)  # one loss in twenty still leaves the 95th percentile on a complete session
    assert run(asyncio.wait_for(go(2), timeout=GUARD_S)) == (False, 18, 2)  # two losses move it to the worst case, as for any other failure


# ---- Codex P2s on noema#694 at 592a40df: cleanup of the synthesis stream ----------------------------------------------------------------------------------------------------


class NoCloseTts:
    """A contract-compliant synthesizer whose iterator has no aclose() (a plain async iterator)."""

    name = "fake-tts-no-close"
    capabilities = CAPS

    def __init__(self) -> None:
        self.calls = 0

    def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        self.calls += 1

        class Stream:
            def __init__(self) -> None:
                self.sent = False

            def __aiter__(self) -> "Stream":
                return self

            async def __anext__(self) -> AudioChunk:
                if self.sent:
                    raise StopAsyncIteration
                self.sent = True
                return AudioChunk(0, AudioCodec.PCM_S16LE, 48000, bytes([0, 1]) * 100)

        return Stream()


def test_a_synthesis_iterator_without_aclose_does_not_abort_the_session() -> None:
    clock = Clock()
    tts = NoCloseTts()
    result = run(asyncio.wait_for(session(clock, Stt(clock), tts, FakeCore(clock), utterance()), timeout=GUARD_S))  # type: ignore[arg-type]
    assert result.timeline.complete and result.failure is None and tts.calls == 1


class RuntimeFailsOnCloseTts(Tts):
    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        try:
            async for chunk in super().synthesize(request):
                yield chunk
        finally:
            raise RuntimeError("provider shutdown failed")


def test_a_runtime_failure_while_the_stream_is_closed_after_the_first_audio_is_recorded_as_a_failure_not_swallowed() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), RuntimeFailsOnCloseTts(clock), FakeCore(clock), utterance()), timeout=GUARD_S))
    assert result.failure is ProviderFailure.UNAVAILABLE  # the session counts as a failure in the batch (see _counted)


# ---- Codex P2 on noema#694 at 4a1f676f: a TimeoutError raised BY the stream is not the harness's cleanup deadline -----------------------------------------------------------------


class TimeoutOnCloseTts(Tts):
    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        try:
            async for chunk in super().synthesize(request):
                yield chunk
        finally:
            raise TimeoutError("the provider's own shutdown timed out")


def test_a_timeout_raised_by_the_stream_while_it_closes_is_a_recorded_failure_not_an_abandoned_close() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), TimeoutOnCloseTts(clock), FakeCore(clock), utterance()), timeout=GUARD_S))
    assert result.failure is ProviderFailure.UNAVAILABLE  # counted as a failure in the batch (see _counted), though the timeline reached its last stage


def test_a_close_that_outlasts_the_cleanup_deadline_is_still_abandoned_and_the_session_stays_complete() -> None:
    clock = Clock()
    result = run(asyncio.wait_for(session(clock, Stt(clock), Tts(clock, hang="close"), FakeCore(clock), utterance()), timeout=GUARD_S))
    assert result.timeline.complete and result.failure is None
