"""Delivering an answer: speech where a qualified provider covers the locale, and the text always (VOI-04, REQ-VOI-002).

PERD §35.3: on a TTS outage the overlay displays the text. So the text is never conditional on audio. Audio is used only
when the capability matrix says YES for the locale and the provider is qualified; an UNVERIFIED cell is not a YES (OQ-07).
A failure is returned with its kind rather than raised, because the person still gets the answer, and it is not swallowed:
the caller records it. A partly synthesised answer is never returned as audio.
"""

import asyncio
from dataclasses import dataclass
from enum import StrEnum
from typing import Any

from noema_model.providers.speech.base import AudioChunk, Locale, ProviderCapabilities, Support
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.tts import SpeechSynthesizer, SynthesisRequest


class DeliveryMode(StrEnum):
    SPEAK = "SPEAK"
    TEXT_ONLY = "TEXT_ONLY"


@dataclass(frozen=True, slots=True)
class Delivery:
    mode: DeliveryMode
    text: str
    audio: tuple[AudioChunk, ...]
    failure: ProviderFailure | None
    error_type: str | None = None  # set only for a failure the adapter did not normalise to ProviderError


def plan_delivery(locale: Locale, capabilities: ProviderCapabilities) -> DeliveryMode:
    covered = dict(capabilities.tts_locales).get(locale) is Support.YES
    return DeliveryMode.SPEAK if covered and capabilities.qualified else DeliveryMode.TEXT_ONLY


def _text_only(request: SynthesisRequest, failure: ProviderFailure | None) -> Delivery:
    return Delivery(DeliveryMode.TEXT_ONLY, request.text, (), failure)


async def _close(stream: object, bound_s: float) -> None:
    """Close a synthesis iterator within `bound_s`. A close that raises is reported by the caller's own error path; one that never returns is abandoned."""
    close = getattr(stream, "aclose", None)
    if close is None:
        return
    try:
        async with asyncio.timeout(bound_s):
            await close()
    except TimeoutError:
        pass  # abandoned: a stalled provider shutdown must not hold the answer


async def _collect(synthesizer: SpeechSynthesizer, request: SynthesisRequest, bound_s: float) -> tuple[AudioChunk, ...]:
    stream = synthesizer.synthesize(request)  # retained, so it is closed on every exit path: completion, error, timeout and cancellation
    try:
        chunks = [chunk async for chunk in stream]
    finally:
        await _close(stream, bound_s)
    if not chunks or any(not chunk.data for chunk in chunks):
        raise ProviderError(ProviderFailure.MALFORMED)
    return tuple(chunks)


def _mark_retrieved(task: "asyncio.Future[Any]") -> None:
    """A synthesizer task that is abandoned must not log an unretrieved exception when it finally ends."""
    if not task.cancelled():
        task.exception()


async def deliver_answer(synthesizer: SpeechSynthesizer, request: SynthesisRequest, *, deadline_s: float, cleanup_s: float | None = None) -> Delivery:
    """The answer text, with audio when the locale is covered and the synthesizer works within `deadline_s`.

    The synthesis runs as a task of its own. At the deadline it is cancelled and given `cleanup_s` (default: `deadline_s`) to stop; one that does not is
    abandoned, so the text fallback never waits for a synthesizer's own shutdown (Codex P2 on #213).
    """
    if plan_delivery(request.locale, synthesizer.capabilities) is DeliveryMode.TEXT_ONLY:
        return _text_only(request, None)
    bound_s = deadline_s if cleanup_s is None else cleanup_s
    task = asyncio.ensure_future(_collect(synthesizer, request, bound_s))
    task.add_done_callback(_mark_retrieved)
    try:
        await asyncio.wait({task}, timeout=deadline_s)
        if not task.done():
            task.cancel()
            await asyncio.wait({task}, timeout=bound_s)  # bounded: a synthesizer that will not stop is abandoned
            return _text_only(request, ProviderFailure.TIMEOUT)
        audio = task.result()
    except ProviderError as error:
        return _text_only(request, error.kind)
    except Exception as error:  # noqa: BLE001 - an adapter bug must not take the answer's text with it (PERD §35.3)
        # Reported, not swallowed: the class name (never the message, which may hold the answer) is returned for the caller to
        # record. CancelledError is a BaseException and passes through.
        return Delivery(DeliveryMode.TEXT_ONLY, request.text, (), ProviderFailure.UNAVAILABLE, type(error).__name__)
    finally:
        if not task.done():
            task.cancel()  # the caller was cancelled: stop the synthesis; it is not waited for
    return Delivery(DeliveryMode.SPEAK, request.text, audio, None)
