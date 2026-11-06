"""Delivering an answer: speech where a qualified provider covers the locale, and the text always (VOI-04, REQ-VOI-002).

PERD §35.3: on a TTS outage the overlay displays the text. So the text is never conditional on audio. Audio is used only
when the capability matrix says YES for the locale and the provider is qualified; an UNVERIFIED cell is not a YES (OQ-07).
A failure is returned with its kind rather than raised, because the person still gets the answer, and it is not swallowed:
the caller records it. A partly synthesised answer is never returned as audio.
"""

import asyncio
from dataclasses import dataclass
from enum import StrEnum

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


def plan_delivery(locale: Locale, capabilities: ProviderCapabilities) -> DeliveryMode:
    covered = dict(capabilities.tts_locales).get(locale) is Support.YES
    return DeliveryMode.SPEAK if covered and capabilities.qualified else DeliveryMode.TEXT_ONLY


def _text_only(request: SynthesisRequest, failure: ProviderFailure | None) -> Delivery:
    return Delivery(DeliveryMode.TEXT_ONLY, request.text, (), failure)


async def _collect(synthesizer: SpeechSynthesizer, request: SynthesisRequest) -> tuple[AudioChunk, ...]:
    chunks = [chunk async for chunk in synthesizer.synthesize(request)]
    if not chunks or any(not chunk.data for chunk in chunks):
        raise ProviderError(ProviderFailure.MALFORMED)
    return tuple(chunks)


async def deliver_answer(synthesizer: SpeechSynthesizer, request: SynthesisRequest, *, deadline_s: float) -> Delivery:
    """The answer text, with audio when the locale is covered and the synthesizer works within `deadline_s`."""
    if plan_delivery(request.locale, synthesizer.capabilities) is DeliveryMode.TEXT_ONLY:
        return _text_only(request, None)
    try:
        async with asyncio.timeout(deadline_s):
            audio = await _collect(synthesizer, request)
    except TimeoutError:
        return _text_only(request, ProviderFailure.TIMEOUT)
    except ProviderError as error:
        return _text_only(request, error.kind)
    return Delivery(DeliveryMode.SPEAK, request.text, audio, None)
