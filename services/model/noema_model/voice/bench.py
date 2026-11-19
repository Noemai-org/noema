"""The latency measurement harness skeleton (VOI-16, REQ-VOI-001, REQ-NFR-003; rulings A241, A377 to A379; AGENT-16, INV-21).

One session is one synthetic utterance through the speech-to-text adapter, the core's admission and answer (a `Core` the caller supplies), and the text-to-speech adapter to its first
audio chunk. An injected clock times the five stages of `latency.py`, so speech-to-text, the response and text-to-speech are measured separately; the number against the target is the
`summarise` of `latency.py`, whose target is the caller's argument. This module sets no threshold.

Safety, before any provider is called: the utterance must carry an origin label, SYNTHETIC or TEAM_RECORDING (a recording of a consenting member of the team; A377 to A379), and anything else is refused; its length must be inside the caller's bounds (the Founder's 5 to 15 second condition is passed as data, not defaulted here), and its audio must fit the audio budget the caller sets (the free minutes of the Integrator account); a refusal is a `BenchError` and nothing is sent. Raw audio of either kind lives only in the bench Environment's private store and never in a report or a log (REQ-VOI-004); this module holds frames in memory for the call only. A failure at any stage leaves an incomplete timeline with the failure's kind, which `summarise` counts as the worst case, so a run cannot meet its target by losing its failures.

The skeleton has no network and no key: the adapters, the clock and the core are arguments. The live entry point that reads the key file and calls Intron is a later change.
"""

import asyncio
import math
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from enum import StrEnum
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


class AudioOrigin(StrEnum):
    SYNTHETIC = "SYNTHETIC"  # made by text-to-speech from our own script (AGENT-25)
    TEAM_RECORDING = "TEAM_RECORDING"  # a recording of a consenting member of the team


@dataclass(frozen=True, slots=True)
class Utterance:
    label: str
    locale: Locale
    frames: tuple[AudioChunk, ...]
    origin: AudioOrigin  # the caller's label; anything that is not an AudioOrigin is refused

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
    origin: AudioOrigin
    timeline: StageTimeline
    failure: ProviderFailure | None  # None for a complete session


class _Frames:
    """The audio as a source the pipeline can close. End of speech is marked when the LAST frame is handed over (the person has stopped speaking), not when the provider reads on to the
    end of the stream: a provider that finalises early never exhausts it (Codex P2 on #694)."""

    def __init__(self, frames: Sequence[AudioChunk], on_last: Callable[[], None]) -> None:
        self._frames = tuple(frames)
        self._next = 0
        self._on_last = on_last

    def __aiter__(self) -> "_Frames":
        return self

    async def __anext__(self) -> AudioChunk:
        if self._next >= len(self._frames):
            raise StopAsyncIteration
        frame = self._frames[self._next]
        self._next += 1
        if self._next == len(self._frames):
            self._on_last()
        return frame

    async def aclose(self) -> None:
        return None


async def _close_stream(stream: object, cleanup_s: float) -> None:
    """Close a synthesis stream within `cleanup_s`. A stream with no `aclose` (a plain async iterator is a contract-compliant return) has nothing to close. A close that does not finish is
    abandoned: TimeoutError. Any other error is the provider's own and is raised to the caller to decide (Codex P2s on #694)."""
    close = getattr(stream, "aclose", None)
    if close is None:
        return
    await asyncio.wait_for(close(), timeout=cleanup_s)


async def _first_audio(tts: SpeechSynthesizer, request: SynthesisRequest, deadline_s: float, cleanup_s: float, timeline: StageTimeline) -> None:
    """The first audio chunk, marked the moment it arrives; the stream is then closed within `cleanup_s`, so cleanup never counts as latency and never holds the session (Codex P2 on #694).
    No audio within `deadline_s` is a timeout; a stream that ends with none is MALFORMED. After the audio was heard: a close that does not finish is abandoned and the session stays complete;
    a close that FAILS is the session's failure (UNAVAILABLE), recorded and counted as one in the batch, never swallowed. If the audio was not heard, that failure stands and a failing close
    does not replace it."""
    stream = tts.synthesize(request)
    try:
        first = await asyncio.wait_for(anext(stream, None), timeout=deadline_s)
        if first is None:
            raise ProviderError(ProviderFailure.MALFORMED)  # silence is never a spoken answer
        timeline.mark(Stage.FIRST_AUDIO)
    except BaseException:
        try:
            await _close_stream(stream, cleanup_s)
        except Exception:  # noqa: BLE001 - the original failure stands; a close that also fails cannot replace it
            pass
        raise
    try:
        await _close_stream(stream, cleanup_s)
    except TimeoutError:
        return  # abandoned: the audio was heard and the stream is let go of
    except ProviderError:
        raise
    except Exception:  # noqa: BLE001 - a provider's own failure at shutdown, after the first audio: recorded as a failure, with no text of it kept
        raise ProviderError(ProviderFailure.UNAVAILABLE) from None


async def measure_session(
    *,
    stt: SpeechProvider,
    tts: SpeechSynthesizer,
    core: Core,
    utterance: Utterance,
    stt_request: StreamRequest,
    tts_model_version: str,
    clock_ms: Callable[[], int],
    deadline_s: float,
    core_deadline_s: float,
    tts_deadline_s: float,
    cleanup_s: float,
    bounds_s: tuple[float, float],
    budget: AudioBudget,
) -> SessionResult:
    """`deadline_s` bounds speech-to-text, `core_deadline_s` each of the core's two steps, `tts_deadline_s` the wait for the first audio and `cleanup_s` each close. None has a default
    (AGENT-16, A324): the baseline run decides their values and the Architect sets them after it."""
    if not isinstance(utterance.origin, AudioOrigin):
        raise BenchError("the audio carries no origin label")
    low, high = bounds_s
    if not (low <= utterance.duration_s <= high):
        raise BenchError("the utterance is outside the measurement bounds")
    if not all(math.isfinite(bound) and bound > 0 for bound in (deadline_s, core_deadline_s, tts_deadline_s, cleanup_s)):
        raise BenchError("every deadline must be a positive finite number")
    budget.charge(utterance.duration_s)
    timeline = StageTimeline(clock_ms)
    state = {"ended": False}

    def end_of_speech() -> None:
        if not state["ended"]:
            state["ended"] = True
            timeline.mark(Stage.END_OF_SPEECH)

    try:
        final = await transcribe_utterance(stt, stt_request, _Frames(utterance.frames, end_of_speech), deadline_s=deadline_s, on_partial=lambda _: None, cleanup_s=cleanup_s)
        end_of_speech()  # a provider that finalised before the last frame was handed over: the stage is marked now, in order
        timeline.mark(Stage.FINAL_TRANSCRIPT)
        async with asyncio.timeout(core_deadline_s):
            await core.admit(final.text)
        timeline.mark(Stage.INTENT_ADMITTED)
        async with asyncio.timeout(core_deadline_s):
            answer = await core.answer()
        timeline.mark(Stage.ANSWER_TEXT)
        await _first_audio(tts, SynthesisRequest(text=answer, locale=utterance.locale, model_version=tts_model_version), tts_deadline_s, cleanup_s, timeline)
    except ProviderError as error:
        return SessionResult(utterance.label, utterance.origin, timeline, error.kind)
    except TimeoutError:
        return SessionResult(utterance.label, utterance.origin, timeline, ProviderFailure.TIMEOUT)
    return SessionResult(utterance.label, utterance.origin, timeline, None)


def measure_batch(results: Sequence[SessionResult], *, target_ms: int) -> LatencySummary:
    """The 95th-percentile summary of the sessions against the caller's target; an incomplete session counts as the worst case."""
    if not results:
        raise ValueError("no sessions")
    return summarise([_counted(result) for result in results], target_ms=target_ms)


def _counted(result: SessionResult) -> StageTimeline:
    """The timeline a session counts as. A session with a recorded failure is never complete, even if its timeline reached the last stage (a failure while the stream was closed after the first
    audio): it counts as the worst case, like any other failed session, so a failure cannot improve the percentile (Codex P2 on #694)."""
    if result.failure is None:
        return result.timeline
    lost = StageTimeline(lambda: 0)
    lost.mark(Stage.END_OF_SPEECH)  # only the first stage: incomplete by construction
    return lost


__all__ = ("AudioBudget", "AudioOrigin", "BenchError", "Core", "SessionResult", "Utterance", "measure_batch", "measure_session")
