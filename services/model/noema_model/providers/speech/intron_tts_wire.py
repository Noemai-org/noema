"""The wire format of Intron's streaming text-to-speech, as pure functions (VOI-04, REQ-VOI-002, AGENT-13; rulings A179 to A181, A241 and A249).

Intron's documented protocol (`docs.voice.intron.io/docs/tts/tts-streaming`, read 16 Nov 2026), nothing invented: a WebSocket at `<base>/tts/v1/stream` whose query names the voice
(`voice_language`, `voice_accent`, `voice_gender`) and the `output_audio_format`; the client sends INPUT_TEXT_CHUNK (`text` of 10 to 100 characters, `ack_id`), asks for each chunk's audio
with FETCH_AUDIO_CHUNK (`chunk_id`) until the server says READY, then sends COMMIT and the server answers COMMITTED_AUDIO. The audio of a chunk is `audio_base_64`, a WAV. **The status
field is spelled `processing_staus` on the page and in its own example, so that is the wire**; if the first live call shows `processing_status`, this is the one file that changes. The key is
never in the URL. A message that is not understood is MALFORMED, never skipped and never echoed (AGENT-13). The numbers are the vendor's published limits, not Noema thresholds.

The adapter speaks only the text it is given, which the core has already masked (A181); `split_text` cuts it at word boundaries and never adds, drops or reorders a word. The only thing it may
add is spaces, to reach the vendor's 10-character minimum.
"""

import base64
import binascii
import json
import struct
from dataclasses import dataclass
from urllib.parse import urlencode

from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_wire import secure_netloc

MIN_TEXT_CHARS = 10  # the vendor's documented minimum characters per text chunk
MAX_TEXT_CHARS = 100  # the vendor's documented maximum (the synchronous page also says 4096; the stricter figure governs until Intron answers)
GENDERS = ("male", "female")
OUTPUT_FORMAT = "wav"


@dataclass(frozen=True, slots=True)
class Voice:
    """A voice as the vendor names it: language code, accent and gender (the accents and languages are those of its supported-languages page)."""

    language: str
    accent: str
    gender: str

    def __post_init__(self) -> None:
        if not self.language.strip() or not self.accent.strip() or self.gender not in GENDERS:
            raise ValueError("a voice needs a language, an accent and a gender of male or female")


@dataclass(frozen=True, slots=True)
class TtsMessage:
    kind: str
    chunk_id: int | None = None
    ready: bool = False
    pcm: bytes = b""
    sample_rate_hz: int = 0


def tts_url(base_url: str, voice: Voice) -> str:
    query = urlencode({"voice_language": voice.language, "voice_accent": voice.accent, "voice_gender": voice.gender, "output_audio_format": OUTPUT_FORMAT})
    return f"wss://{secure_netloc(base_url)}/tts/v1/stream?{query}"


def split_text(text: str) -> list[str]:
    """The text as pieces of MIN to MAX characters, cut at word boundaries. A word longer than the maximum is refused (it is never cut), blank text is refused, and text shorter than
    the minimum is padded with spaces after its last word (the one thing added)."""
    words = text.split()
    if not words:
        raise ValueError("there is no text to speak")
    if any(len(word) > MAX_TEXT_CHARS for word in words):
        raise ValueError("a word is longer than the vendor's text chunk limit")
    pieces: list[list[str]] = [[]]
    for word in words:
        if pieces[-1] and len(" ".join([*pieces[-1], word])) > MAX_TEXT_CHARS:
            pieces.append([])
        pieces[-1].append(word)
    while len(pieces) > 1 and len(" ".join(pieces[-1])) < MIN_TEXT_CHARS:
        tail, before = pieces[-1], pieces[-2]
        if len(" ".join([*before, *tail])) <= MAX_TEXT_CHARS:
            before.extend(tail)
            pieces.pop()
        elif len(before) > 1 and len(" ".join(before[:-1])) >= MIN_TEXT_CHARS:
            tail.insert(0, before.pop())
        else:
            break
    return [" ".join(piece).ljust(MIN_TEXT_CHARS) for piece in pieces]


def text_message(text: str, ack_id: int) -> str:
    if not MIN_TEXT_CHARS <= len(text) <= MAX_TEXT_CHARS:
        raise ValueError("a text chunk must be within the vendor's limits")
    return json.dumps({"message_type": "INPUT_TEXT_CHUNK", "text": text, "ack_id": ack_id})


def fetch_message(chunk_id: int) -> str:
    return json.dumps({"message_type": "FETCH_AUDIO_CHUNK", "chunk_id": chunk_id})


def _malformed() -> ProviderError:
    return ProviderError(ProviderFailure.MALFORMED)


_ERRORS = {
    "QUOTA_EXCEEDED": ProviderFailure.RATE_LIMITED,
    "RESOURCE_EXHAUSTED": ProviderFailure.RATE_LIMITED,
    "AUTHENTICATION_ERROR": ProviderFailure.REJECTED,
    "SESSION_TIME_LIMIT_EXCEEDED": ProviderFailure.TIMEOUT,
    "INSUFFICIENT_TEXT_ACTIVITY": ProviderFailure.TIMEOUT,
    "ERROR": ProviderFailure.UNAVAILABLE,
    "INPUT_ERROR": ProviderFailure.MALFORMED,
    "CHUNK_ID_MISMATCH_WITH_TOTAL": ProviderFailure.MALFORMED,
    "CHUNK_SIZE_TOO_SMALL": ProviderFailure.MALFORMED,
    "CHUNK_SIZE_TOO_LARGE": ProviderFailure.MALFORMED,
}


def pcm_from_wav(data: bytes, declared_rate: int) -> bytes:
    """The 16-bit mono PCM samples of a WAV, or MALFORMED. The sample rate in the header must be the one the message declares; a header, a rate or a width that does not agree is refused."""
    if len(data) < 12 or data[:4] != b"RIFF" or data[8:12] != b"WAVE":
        raise _malformed()
    position, fmt_ok, samples = 12, False, None
    while position + 8 <= len(data):
        name, size = data[position:position + 4], struct.unpack("<I", data[position + 4:position + 8])[0]
        body = data[position + 8:position + 8 + size]
        if len(body) < size:
            raise _malformed()
        if name == b"fmt ":
            if size < 16:
                raise _malformed()
            tag, channels, rate, _, _, width = struct.unpack("<HHIIHH", body[:16])
            fmt_ok = tag == 1 and channels == 1 and width == 16 and rate == declared_rate and rate >= 1
        elif name == b"data":
            samples = body
        position += 8 + size + (size & 1)
    if not fmt_ok or samples is None or len(samples) % 2:
        raise _malformed()
    return samples


def parse_server_message(raw: str) -> TtsMessage:
    """One message from the server. A vendor error raises its typed ProviderError (no body in it); anything not understood is MALFORMED."""
    try:
        document = json.loads(raw)
    except (ValueError, RecursionError):
        raise _malformed() from None
    if not isinstance(document, dict) or not isinstance(document.get("message_type"), str):
        raise _malformed()
    kind = document["message_type"]
    if kind in _ERRORS:
        raise ProviderError(_ERRORS[kind])
    if kind in ("SESSION_CREATED", "COMMITTED_AUDIO"):
        return TtsMessage(kind)
    if kind == "TEXT_CHUNK_ACK":
        chunk_id = document.get("chunk_id")
        if type(chunk_id) is not int:
            raise _malformed()
        return TtsMessage(kind, chunk_id)
    if kind == "FETCH_AUDIO_CHUNK":
        chunk_id, status = document.get("chunk_id"), document.get("processing_staus")
        if type(chunk_id) is not int or status not in ("READY", "PROCESSING"):
            raise _malformed()
        if status == "PROCESSING":
            return TtsMessage(kind, chunk_id, False)
        rate, encoded = document.get("audio_config_frame_rate"), document.get("audio_base_64")
        if type(rate) is not int or rate < 1 or not isinstance(encoded, str):
            raise _malformed()
        try:
            wav = base64.b64decode(encoded, validate=True)
        except (binascii.Error, ValueError):
            raise _malformed() from None
        pcm = pcm_from_wav(wav, rate)
        if not pcm:
            raise _malformed()  # a chunk with no audio is not a spoken answer
        return TtsMessage(kind, chunk_id, True, pcm, rate)
    raise _malformed()
