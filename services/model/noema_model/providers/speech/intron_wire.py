"""The wire format of Intron's streaming speech-to-text, as pure functions (VOI-01, REQ-VOI-002, AGENT-13; rulings A179 to A181, A191, A241 and A249).

Everything here is Intron's documented protocol, read from its public documentation on 13 Nov 2026 (`docs/voice/intron-docs-notes-DRAFT.md`), and nothing is invented: a WebSocket at
`<base>/stt/v1/stream` with the language, sample rate, bit rate and channel count in the query; the client sends base64 PCM16 little-endian chunks of 1 KB to 32 KB, each with an
`ack_id`, then a COMMIT; the server answers SESSION_CREATED, AUDIO_CHUNK_ACK, PARTIAL_TRANSCRIPT (`transcript`), COMMITTED_TRANSCRIPT (`transcript_text`) or QUOTA_EXCEEDED. The key is
never in the URL (it travels in a header, set by the adapter). The numbers below are the vendor's published chunk limits, not Noema thresholds. If the first live call shows a
different spelling, this is the one file that changes.

A server message that is not understood is MALFORMED: never skipped, never guessed at, and never echoed (an error carries no body, AGENT-13).
"""

import base64
import json
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlsplit

from noema_model.providers.speech.base import Locale
from noema_model.providers.speech.errors import ProviderError, ProviderFailure

MIN_CHUNK_BYTES = 1024  # the vendor's documented minimum chunk
MAX_CHUNK_BYTES = 32 * 1024  # the vendor's documented maximum chunk (our own frame cap, A191, is lower)
SAMPLE_RATE_HZ = 16_000  # PCM16 mono at 16 kHz is the only format the voice track accepts (F7, A97, A191)
LOCALE_CODES: dict[Locale, str] = {Locale.EN_NG: "en", Locale.PCM: "pcm", Locale.YO: "yo", Locale.HA: "ha", Locale.IG: "ig"}


class ConnectionClosed(Exception):
    """The connection ended: closed by the server, dropped, or never made."""


@dataclass(frozen=True, slots=True)
class ServerMessage:
    kind: str
    text: str | None = None


def secure_netloc(base_url: str) -> str:
    """The host of a secure WebSocket base address (wss://host) with nothing after it; anything else is refused. The key is never part of an address."""
    parts = urlsplit(base_url)
    if parts.scheme != "wss" or not parts.netloc or parts.query or parts.fragment or parts.path not in ("", "/"):
        raise ValueError("the base URL must be a secure WebSocket address (wss://host)")
    return parts.netloc


def stt_url(base_url: str, locale: Locale) -> str:
    """The streaming speech-to-text URL for a locale; the key is not in the URL."""
    return f"wss://{secure_netloc(base_url)}/stt/v1/stream?use_language_asr_input={LOCALE_CODES[locale]}&sample_rate={SAMPLE_RATE_HZ}&bit_rate=16&num_channels=1"


def audio_message(pcm: bytes, ack_id: int) -> str:
    """One chunk of audio. A chunk outside the vendor's limits is refused here, before anything is sent."""
    if not MIN_CHUNK_BYTES <= len(pcm) <= MAX_CHUNK_BYTES:
        raise ValueError("an audio chunk must be within the vendor's chunk limits")
    return json.dumps({"message_type": "INPUT_AUDIO_CHUNK", "audio_base_64": base64.b64encode(pcm).decode("ascii"), "ack_id": ack_id})


def commit_message() -> str:
    return json.dumps({"message_type": "COMMIT"})


def _malformed() -> ProviderError:
    return ProviderError(ProviderFailure.MALFORMED)


def _text(document: dict[str, Any], key: str) -> str:
    value = document.get(key)
    if not isinstance(value, str):
        raise _malformed()
    return value


_ERRORS = {
    "AUTHENTICATION_ERROR": ProviderFailure.REJECTED,
    "RESOURCE_EXHAUSTED": ProviderFailure.RATE_LIMITED,
    "SESSION_TIME_LIMIT_EXCEEDED": ProviderFailure.TIMEOUT,
    "INSUFFICIENT_AUDIO_ACTIVITY": ProviderFailure.TIMEOUT,
    "ERROR": ProviderFailure.UNAVAILABLE,
    "INPUT_ERROR": ProviderFailure.MALFORMED,
    "CHUNK_SIZE_TOO_SMALL": ProviderFailure.MALFORMED,
    "CHUNK_SIZE_TOO_LARGE": ProviderFailure.MALFORMED,
    "CHUNK_ID_MISMATCH_WITH_TOTAL": ProviderFailure.MALFORMED,
}


def parse_server_message(raw: str) -> ServerMessage:
    """One message from the server, or MALFORMED."""
    try:
        document = json.loads(raw)
    except (ValueError, RecursionError):
        raise _malformed() from None
    if not isinstance(document, dict) or not isinstance(document.get("message_type"), str):
        raise _malformed()
    kind = document["message_type"]
    if kind in ("SESSION_CREATED", "AUDIO_CHUNK_ACK", "QUOTA_EXCEEDED"):
        return ServerMessage(kind)
    if kind in _ERRORS:
        raise ProviderError(_ERRORS[kind])  # the vendor's error types of its streaming speech-to-text page; no body or message of the vendor is kept (AGENT-13)
    if kind == "PARTIAL_TRANSCRIPT":
        return ServerMessage(kind, _text(document, "transcript"))
    if kind == "COMMITTED_TRANSCRIPT":
        _text(document, "transcript_id")
        return ServerMessage(kind, _text(document, "transcript_text"))
    raise _malformed()
