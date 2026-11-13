"""The Input Gateway (UND-02): one raw request body and the authenticated context in, one CanonicalInput or an INPUT_INVALID refusal out.

The body may carry only what a client can say: request_id, channel, text, locale_hint, the voice block and conversation_ref.
Identity, tenant, session, correlation and the mode come from the RequestContext that core built from the authenticated session
(REQ-INP-002, INV-12); a body that names any of them is refused, not ignored, so an attempt is visible. The text is normalised to
Unicode NFC and trimmed, and must then be 1 to 2,000 characters. A transcript that is not final is refused (a partial never sets a
slot). The refusal names a Reason and never carries the text.
"""

import json
import math
import unicodedata
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import UTC, datetime
from enum import StrEnum
from typing import Any

from pydantic import ValidationError

from noema_model.contracts.canonical_input_v2 import CanonicalInputV2
from noema_model.platform.clock import Clock

SCHEMA_VERSION = "2.0"
TEXT_LIMIT = 2000  # canonical-input.v2.json, text.maxLength; the contract bounds it and this module enforces it after normalisation
_BODY_KEYS = frozenset({"request_id", "channel", "text", "locale_hint", "voice", "conversation_ref"})
_REQUIRED = ("request_id", "channel", "text")
_VOICE_KEYS = frozenset({"provider_id", "model_version", "utterance_confidence", "token_confidences", "is_final", "audio_ref"})
_CHANNELS = frozenset({"text", "voice"})


class Reason(StrEnum):
    """Why an input was refused. A closed set, safe to log and to show: it says what rule failed, never what the person said."""

    EMPTY_TEXT = "EMPTY_TEXT"
    TEXT_TOO_LONG = "TEXT_TOO_LONG"
    ENCODING = "ENCODING"
    NOT_FINAL = "NOT_FINAL"
    UNKNOWN_FIELD = "UNKNOWN_FIELD"
    CHANNEL_MISMATCH = "CHANNEL_MISMATCH"
    BAD_FIELD = "BAD_FIELD"
    AUDIO_REF_NOT_ALLOWED = "AUDIO_REF_NOT_ALLOWED"


class InputInvalid(Exception):
    """INPUT_INVALID (PERD REQ-INP-001). Carries a Reason and the name of the field, never a value."""

    code = "INPUT_INVALID"

    def __init__(self, reason: Reason, field: str = "") -> None:
        super().__init__(f"INPUT_INVALID: {reason.value}" + (f" ({field})" if field else ""))
        self.reason = reason
        self.field = field


@dataclass(frozen=True, slots=True)
class RequestContext:
    """What core knows from the authenticated session. The Gateway copies it into the canonical input and never reads it from the body."""

    correlation_id: str
    tenant_id: str
    session_id: str
    principal_ref: str


class Gateway:
    def __init__(self, clock: Clock) -> None:
        self._clock = clock

    def canonicalise(self, raw: Mapping[str, Any], ctx: RequestContext) -> CanonicalInputV2:
        if not isinstance(raw, Mapping):
            raise InputInvalid(Reason.BAD_FIELD, "body")
        extra = sorted(str(k) for k in raw if k not in _BODY_KEYS)
        if extra:
            raise InputInvalid(Reason.UNKNOWN_FIELD, extra[0])
        for key in _REQUIRED:
            if key not in raw:
                raise InputInvalid(Reason.BAD_FIELD, key)
        channel = raw["channel"]
        if channel not in _CHANNELS:
            raise InputInvalid(Reason.BAD_FIELD, "channel")
        voice = raw.get("voice")
        if (channel == "voice") != (voice is not None):
            raise InputInvalid(Reason.CHANNEL_MISMATCH, "voice")
        if voice is not None:
            _check_voice(voice)
        text = _normalise(raw["text"])
        document: dict[str, Any] = {
            "schema_version": SCHEMA_VERSION,
            "request_id": raw["request_id"],
            "correlation_id": ctx.correlation_id,
            "tenant_id": ctx.tenant_id,
            "session_id": ctx.session_id,
            "principal_ref": ctx.principal_ref,
            "channel": channel,
            "mode": "MODE_1",
            "text": text,
            "received_at": _stamp(self._clock.now_utc()),
        }
        for key in ("locale_hint", "conversation_ref"):
            if key in raw:
                document[key] = raw[key]
        if voice is not None:
            document["voice"] = dict(voice)
        try:
            return CanonicalInputV2.model_validate_json(json.dumps(document))  # the contract models are strict about Python enums, lax about JSON
        except ValidationError as err:
            first = err.errors()[0]
            field = ".".join(str(part) for part in first["loc"])
            raise InputInvalid(Reason.BAD_FIELD, field) from None  # the pydantic message can quote the value


def _stamp(at: datetime) -> str:
    at = at.astimezone(UTC)
    return at.strftime("%Y-%m-%dT%H:%M:%S.") + f"{at.microsecond // 1000:03d}Z"


def _normalise(value: object) -> str:
    if isinstance(value, bytes):
        try:
            value = value.decode("utf-8")
        except UnicodeDecodeError:
            raise InputInvalid(Reason.ENCODING, "text") from None
    if not isinstance(value, str):
        raise InputInvalid(Reason.ENCODING, "text")
    try:
        value.encode("utf-8")  # a lone surrogate is not valid text
    except UnicodeEncodeError:
        raise InputInvalid(Reason.ENCODING, "text") from None
    text = unicodedata.normalize("NFC", value).strip()
    if not text:
        raise InputInvalid(Reason.EMPTY_TEXT, "text")
    if len(text) > TEXT_LIMIT:
        raise InputInvalid(Reason.TEXT_TOO_LONG, "text")
    return text


def _check_voice(voice: object) -> None:
    if not isinstance(voice, Mapping):
        raise InputInvalid(Reason.BAD_FIELD, "voice")
    extra = sorted(str(k) for k in voice if k not in _VOICE_KEYS)
    if extra:
        raise InputInvalid(Reason.UNKNOWN_FIELD, f"voice.{extra[0]}")
    missing = sorted(_VOICE_KEYS - set(voice))
    if missing:
        raise InputInvalid(Reason.BAD_FIELD, f"voice.{missing[0]}")
    if voice["audio_ref"] is not None:
        # canonical-input.v2 (CR docs#137): null means the audio was discarded. No consent-backed retention path exists, so a reference of any shape is refused here,
        # fail closed, and the refusal names the field, never the value (AGENT-13).
        raise InputInvalid(Reason.AUDIO_REF_NOT_ALLOWED, "voice.audio_ref")
    final = voice["is_final"]
    if not isinstance(final, bool):
        raise InputInvalid(Reason.BAD_FIELD, "voice.is_final")
    if not final:
        raise InputInvalid(Reason.NOT_FINAL, "voice.is_final")
    tokens = voice["token_confidences"]
    if not isinstance(tokens, list):
        raise InputInvalid(Reason.BAD_FIELD, "voice.token_confidences")
    for name, value in [("utterance_confidence", voice["utterance_confidence"]), *[("token_confidences", t) for t in tokens]]:
        if isinstance(value, bool) or not isinstance(value, int | float) or not math.isfinite(value) or not 0 <= value <= 1:
            raise InputInvalid(Reason.BAD_FIELD, f"voice.{name}")
