"""The latency measurement harness skeleton (VOI-16, REQ-VOI-001, REQ-NFR-003; rulings A180, A241; AGENT-16, INV-21).

One session is one synthetic utterance through the speech-to-text adapter, the core's admission and answer (a `Core` the caller supplies), and the text-to-speech adapter to its first
audio chunk. An injected clock times the five stages of `latency.py`, so speech-to-text, the response and text-to-speech are measured separately; the number against the target is the
`summarise` of `latency.py`, whose target is the caller's argument. This module sets no threshold.

Safety, before any provider is called: the utterance must be marked synthetic (A180: no real speaker audio goes to any vendor before the data terms are signed), its length must be inside the
caller's bounds (the Founder's 5 to 15 second condition is passed as data, not defaulted here), and its audio must fit the audio budget the caller sets (the free minutes of the
Integrator account); a refusal is a `BenchError` and nothing is sent. A failure at any stage leaves an incomplete timeline with the failure's kind, which `summarise` counts as the worst
case, so a run cannot meet its target by losing its failures.

The skeleton has no network and no key: the adapters, the clock and the core are arguments. The live entry point that reads the key file and calls Intron is a later change.
"""

import math
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Protocol

from noema_model.providers.speech.base import AudioChunk, Locale, SpeechProvider, StreamRequest
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.tts import SpeechSynthesizer, SynthesisRequest
from noema_model.voice.latency import LatencySummary, Stage, StageTimeline, summarise
from noema_model.voice.pipeline import transcribe_utterance

_BYTES_PER_SAMPLE = 2  # PCM16 mono, the contract of every speech source (A191)


class BenchError(Exception):
    """The measurement was refused before anything was sent. The message is fixed text."""


class Core(Protocol):
    """What the response stage is: the core admits the final transcript as an intent, then produces the answer text (already masked, A181)."""

    async def admit(self, text: str) -> None: ...

    async def answer(self) -> str: ...


@dataclass(frozen=True, slots=True)
class SyntheticUtterance:
    label: str
    locale: Locale
    frames: tuple[AudioChunk, ...]
    synthetic: bool  # must be True (A180); the caller states it, and an unmarked utterance is refused

    @property
    def duration_s(self) -> float:
        return sum(len(frame.data) // _BYTES_PER_SAMPLE / frame.sample_rate_hz for frame in self.frames)


class AudioBudget:
    """The seconds of audio the run may send. Charged before a session starts; a session that does not fit is refused and charges nothing."""

    def __init__(self, *, total_s: float) -> None:
        if not (math.isfinite(total_s) and total_s > 0):
            raise ValueError("the audio budget must be a positive finite number of seconds")
        self.total_s = total_s
        self.spent_s = 0.0

    def charge(self, seconds: float) -> None:
        if self.spent_s + seconds > self.total_s:
            raise BenchError("the audio budget would be exceeded")
        self.spent_s += seconds


@dataclass(frozen=True, slots=True)
class SessionResult:
    label: str
    timeline: StageTimeline
    failure: ProviderFailure | None  # None for a complete session


class _Frames:
    """The audio as a source the pipeline can close. End of speech is marked when the last frame has been read, which is when the person stopped speaking."""

    def __init__(self, frames: Sequence[AudioChunk], on_exhausted: Callable[[], None]) -> None:
        self._items = iter(frames)
        self._on_exhausted = on_exhausted

    def __aiter__(self) -> "_Frames":
        return self

    async def __anext__(self) -> AudioChunk:
        try:
            return next(self._items)
        except StopIteration:
            self._on_exhausted()
            raise StopAsyncIteration from None

    async def aclose(self) -> None:
        return None


async def measure_session(
    *,
    stt: SpeechProvider,
    tts: SpeechSynthesizer,
    core: Core,
    utterance: SyntheticUtterance,
    stt_request: StreamRequest,
    tts_model_version: str,
    clock_ms: Callable[[], int],
    deadline_s: float,
    bounds_s: tuple[float, float],
    budget: AudioBudget,
) -> SessionResult:
    if not utterance.synthetic:
        raise BenchError("only synthetic audio is measured")
    low, high = bounds_s
    if not (low <= utterance.duration_s <= high):
        raise BenchError("the utterance is outside the measurement bounds")
    budget.charge(utterance.duration_s)
    timeline = StageTimeline(clock_ms)
    state = {"ended": False}

    def end_of_speech() -> None:
        if not state["ended"]:
            state["ended"] = True
            timeline.mark(Stage.END_OF_SPEECH)

    try:
        final = await transcribe_utterance(stt, stt_request, _Frames(utterance.frames, end_of_speech), deadline_s=deadline_s, on_partial=lambda _: None)
        end_of_speech()  # a provider that finalised before the audio ended: the stage is marked now, in order
        timeline.mark(Stage.FINAL_TRANSCRIPT)
        await core.admit(final.text)
        timeline.mark(Stage.INTENT_ADMITTED)
        answer = await core.answer()
        timeline.mark(Stage.ANSWER_TEXT)
        stream = tts.synthesize(SynthesisRequest(text=answer, locale=utterance.locale, model_version=tts_model_version))
        try:
            first = await anext(stream, None)
        finally:
            await stream.aclose()  # type: ignore[attr-defined]
        if first is None:
            raise ProviderError(ProviderFailure.MALFORMED)  # silence is never a spoken answer
        timeline.mark(Stage.FIRST_AUDIO)
    except ProviderError as error:
        return SessionResult(utterance.label, timeline, error.kind)
    except TimeoutError:
        return SessionResult(utterance.label, timeline, ProviderFailure.TIMEOUT)
    return SessionResult(utterance.label, timeline, None)


def measure_batch(results: Sequence[SessionResult], *, target_ms: int) -> LatencySummary:
    """The 95th-percentile summary of the sessions against the caller's target; an incomplete session counts as the worst case."""
    if not results:
        raise ValueError("no sessions")
    return summarise([result.timeline for result in results], target_ms=target_ms)


__all__ = ("AudioBudget", "BenchError", "Core", "SessionResult", "SyntheticUtterance", "measure_batch", "measure_session")
