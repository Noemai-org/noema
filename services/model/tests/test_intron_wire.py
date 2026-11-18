"""The Intron wire format as pure functions (VOI-01, REQ-VOI-002, AGENT-13; rulings A179 to A181, A191, A241 and A249).

Intron's documented streaming protocol (the Intron Voice API documentation, read 13 Nov 2026; `docs/voice/intron-docs-notes-DRAFT.md`) is a WebSocket: the client sends base64 PCM16 chunks of 1 KB
to 32 KB and a COMMIT; the server answers with SESSION_CREATED, AUDIO_CHUNK_ACK, PARTIAL_TRANSCRIPT, COMMITTED_TRANSCRIPT or QUOTA_EXCEEDED. These tests pin our reading of it in one
place so that the first live call can correct it in one place. The key is never in the URL; a message that is not understood is MALFORMED, never skipped; and no error text carries
a body or a key. All audio is SYNTHETIC (AGENT-25).
"""

import base64
import json

import pytest

from noema_model.providers.speech.base import Locale
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_wire import (
    LOCALE_CODES,
    MAX_CHUNK_BYTES,
    MIN_CHUNK_BYTES,
    ServerMessage,
    audio_message,
    commit_message,
    parse_server_message,
    stt_url,
)

BASE = "wss://example.invalid"


def test_the_vendor_chunk_limits_are_the_documented_ones() -> None:
    assert (MIN_CHUNK_BYTES, MAX_CHUNK_BYTES) == (1024, 32 * 1024)


@pytest.mark.parametrize("locale,code", [(Locale.EN_NG, "en"), (Locale.PCM, "pcm"), (Locale.YO, "yo"), (Locale.HA, "ha"), (Locale.IG, "ig")])
def test_every_locale_of_the_voice_track_has_its_language_code(locale: Locale, code: str) -> None:
    assert LOCALE_CODES[locale] == code
    url = stt_url(BASE, locale)
    assert url.startswith(f"{BASE}/stt/v1/stream?")
    assert f"use_language_asr_input={code}" in url and "sample_rate=16000" in url and "bit_rate=16" in url and "num_channels=1" in url


def test_the_url_carries_no_key_and_no_other_member() -> None:
    url = stt_url(BASE, Locale.PCM)
    assert "key" not in url.lower() and "token" not in url.lower() and "authorization" not in url.lower()


def test_a_base_url_that_is_not_a_secure_websocket_is_refused() -> None:
    for bad in ("ws://example.invalid", "https://example.invalid", "example.invalid", "wss://", ""):
        with pytest.raises(ValueError):
            stt_url(bad, Locale.PCM)


def test_an_audio_message_is_base64_pcm_with_its_ack_id() -> None:
    pcm = bytes(range(256)) * 4
    decoded = json.loads(audio_message(pcm, 7))
    assert decoded["message_type"] == "INPUT_AUDIO_CHUNK" and decoded["ack_id"] == 7
    assert base64.b64decode(decoded["audio_base_64"], validate=True) == pcm


@pytest.mark.parametrize("size", [0, 1, MIN_CHUNK_BYTES - 1, MAX_CHUNK_BYTES + 1])
def test_a_chunk_outside_the_vendors_limits_is_refused_before_it_is_sent(size: int) -> None:
    with pytest.raises(ValueError):
        audio_message(b"\x00" * size, 0)


def test_the_commit_message() -> None:
    assert json.loads(commit_message()) == {"message_type": "COMMIT"}


def test_the_server_messages_are_understood() -> None:
    assert parse_server_message(json.dumps({"message_type": "SESSION_CREATED", "credit_balance": 12.5})) == ServerMessage("SESSION_CREATED")
    assert parse_server_message(json.dumps({"message_type": "AUDIO_CHUNK_ACK", "ack_id": 3})) == ServerMessage("AUDIO_CHUNK_ACK")
    assert parse_server_message(json.dumps({"message_type": "PARTIAL_TRANSCRIPT", "transcript": "send fi"})) == ServerMessage("PARTIAL_TRANSCRIPT", "send fi")
    committed = {"message_type": "COMMITTED_TRANSCRIPT", "transcript_id": "t-1", "transcript_text": "send fifty thousand naira", "audio_len": 6.1}
    assert parse_server_message(json.dumps(committed)) == ServerMessage("COMMITTED_TRANSCRIPT", "send fifty thousand naira")
    assert parse_server_message(json.dumps({"message_type": "QUOTA_EXCEEDED"})).kind == "QUOTA_EXCEEDED"


@pytest.mark.parametrize("raw", [
    "", "not json", "[]", "7", "null", "{}", json.dumps({"message_type": 5}), json.dumps({"message_type": "SOMETHING_NEW"}),
    json.dumps({"message_type": "PARTIAL_TRANSCRIPT"}), json.dumps({"message_type": "PARTIAL_TRANSCRIPT", "transcript": 3}),
    json.dumps({"message_type": "COMMITTED_TRANSCRIPT", "transcript_id": "t"}), json.dumps({"message_type": "COMMITTED_TRANSCRIPT", "transcript_text": None}),
    pytest.param("x" * 200_000, id="huge"),
])
def test_anything_not_understood_is_malformed_never_skipped_and_never_echoed(raw: str) -> None:
    with pytest.raises(ProviderError) as caught:
        parse_server_message(raw)
    assert caught.value.kind is ProviderFailure.MALFORMED
    assert raw[:20] not in str(caught.value) or raw == ""  # the body is never in the error text (AGENT-13)
