"""The Intron text-to-speech adapter behind `SpeechSynthesizer` and its wire format (VOI-04, REQ-VOI-002, INV-07, AGENT-13; rulings A179 to A181, A241, A249).

Intron's documented streaming text-to-speech (`docs.voice.intron.io`, read 16 Nov 2026) is a WebSocket: the query names the voice (language, accent, gender) and the output format; the
client sends INPUT_TEXT_CHUNK messages of 10 to 100 characters, asks for each chunk's audio with FETCH_AUDIO_CHUNK until it says READY (the field is spelled `processing_staus` on
the page and in its example, so that spelling is the wire), then sends COMMIT and the server answers COMMITTED_AUDIO. The page's own spelling and examples are pinned here, in
one place. The adapter speaks only the text it is given, which the core has already masked (A181); it splits it at word boundaries and never adds a word. The voice for a locale is
data the caller passes (no default). A scripted WebSocket server stands in for Intron (SYNTHETIC; the recorded AGENT-12 exception of noema#412); audio is synthetic PCM.
"""

import asyncio
import base64
import json
import struct
from collections.abc import Mapping, Sequence

import pytest

from noema_model.providers.speech.base import AudioCodec, Locale, PipelineMode, ProviderCapabilities, Support
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_tts import IntronTextToSpeech, Voice
from noema_model.providers.speech.intron_tts_wire import MAX_TEXT_CHARS, MIN_TEXT_CHARS, split_text, tts_url
from noema_model.providers.speech.intron_wire import ConnectionClosed
from noema_model.providers.speech.tts import SpeechSynthesizer, SynthesisRequest
from tests.support import run

KEY = "synthetic-key-0123456789"
CAPS = ProviderCapabilities("intron", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), tuple((loc, Support.YES) for loc in Locale), None, Support.YES, "pinned-1", True)
VOICES = {Locale.PCM: Voice("pcm", "pidgin", "female"), Locale.YO: Voice("yo", "yoruba", "male"), Locale.EN_NG: Voice("en", "yoruba", "female")}
GUARD_S = 5.0
RATE = 48000
LONG = "We found your transfer of fifty thousand naira. It was credited this morning, and the receipt for it is ready for you to read now."  # over 100 characters


def wav(samples: int = 480, rate: int = RATE) -> bytes:
    data = b"\x10\x00" * samples
    header = b"RIFF" + struct.pack("<I", 36 + len(data)) + b"WAVEfmt " + struct.pack("<IHHIIHH", 16, 1, 1, rate, rate * 2, 2, 16) + b"data" + struct.pack("<I", len(data))
    return header + data


def audio_reply(chunk_id: int, status: str = "READY", payload: bytes | None = None) -> str:
    return json.dumps({"message_type": "FETCH_AUDIO_CHUNK", "processing_staus": status, "chunk_id": chunk_id, "audio_base_64": base64.b64encode(payload if payload is not None else wav()).decode(),
                       "extension": "wav", "audio_config_num_channels": 1, "audio_config_sample_width": 2, "audio_config_frame_rate": RATE})


class Server:
    """A scripted Intron TTS. Answers each INPUT_TEXT_CHUNK with an ack, each FETCH_AUDIO_CHUNK per `fetches` (a status per request), COMMIT with COMMITTED_AUDIO."""

    def __init__(self, fetches: Sequence[str] = (), *, raw_replies: Mapping[str, str] | None = None, drop_at: int | None = None) -> None:
        self.url = ""
        self.headers: Mapping[str, str] = {}
        self.sent: list[dict[str, object]] = []
        self.closed = False
        self._statuses = list(fetches)
        self._raw = dict(raw_replies or {})
        self._drop_at = drop_at
        self._queue: asyncio.Queue[str | None] = asyncio.Queue()
        self.requests_for: dict[int, int] = {}

    async def send(self, message: str) -> None:
        body = json.loads(message)
        self.sent.append(body)
        if self._drop_at is not None and len(self.sent) >= self._drop_at:
            await self._queue.put(None)
            return
        kind = body["message_type"]
        if kind in self._raw:
            await self._queue.put(self._raw[kind])
        elif kind == "INPUT_TEXT_CHUNK":
            await self._queue.put(json.dumps({"message_type": "TEXT_CHUNK_ACK", "chunk_id": body["ack_id"], "total_chunks": body["ack_id"] + 1}))
        elif kind == "FETCH_AUDIO_CHUNK":
            chunk_id = int(body["chunk_id"])
            seen = self.requests_for[chunk_id] = self.requests_for.get(chunk_id, 0) + 1
            status = self._statuses.pop(0) if self._statuses else ("READY" if seen > 0 else "PROCESSING")
            await self._queue.put(audio_reply(chunk_id, status))
        elif kind == "COMMIT":
            await self._queue.put(json.dumps({"message_type": "COMMITTED_AUDIO", "text_id": "t1", "audio_len": 1.2}))

    async def recv(self) -> str:
        item = await self._queue.get()
        if item is None:
            raise ConnectionClosed
        return item

    async def close(self) -> None:
        self.closed = True

    def texts(self) -> list[str]:
        return [str(m["text"]) for m in self.sent if m["message_type"] == "INPUT_TEXT_CHUNK"]


def adapter(server: Server, *, fail_connect: Exception | None = None, poll_s: float = 0.001) -> IntronTextToSpeech:
    async def connect(url: str, headers: Mapping[str, str]) -> Server:
        if fail_connect is not None:
            raise fail_connect
        server.url, server.headers = url, dict(headers)
        return server

    return IntronTextToSpeech(capabilities=CAPS, connect=connect, base_url="wss://example.invalid", api_key=lambda: KEY, voices=VOICES, poll_s=poll_s)


def speak(server: Server, text: str, locale: Locale = Locale.PCM) -> list[object]:
    async def call() -> list[object]:
        return [chunk async for chunk in adapter(server).synthesize(SynthesisRequest(text=text, locale=locale, model_version="pinned-1"))]

    return run(asyncio.wait_for(call(), timeout=GUARD_S))


# ---- the text is split at word boundaries into the vendor's 10 to 100 characters, and nothing is added or lost ---------------------------------------------------------


def test_the_vendor_limits_are_the_documented_ones() -> None:
    assert (MIN_TEXT_CHARS, MAX_TEXT_CHARS) == (10, 100)


@pytest.mark.parametrize("text", [
    "We found your transfer of fifty thousand naira to the recipient ending in 4821 and it was credited this morning.",
    "Short enough to send at once.",
    "a " * 80,
    "Ọ̀rọ̀ yìí gùn tó láti pín sí ọ̀pọ̀lọpọ̀ ẹ̀yà, a sì gbọ́dọ̀ kọ́ gbogbo wọn sílẹ̀ ní ọ̀kọ̀ọ̀kan kí a tó fi ránṣẹ́ sí olùgbọ́.",
])
def test_every_piece_is_within_the_limits_and_the_words_are_the_same_in_the_same_order(text: str) -> None:
    pieces = split_text(text)
    assert all(MIN_TEXT_CHARS <= len(p) <= MAX_TEXT_CHARS for p in pieces)
    assert " ".join(" ".join(pieces).split()) == " ".join(text.split())  # no word added, none lost, none reordered (the spaces at a join are the only difference)


def test_text_shorter_than_the_vendors_minimum_is_padded_with_spaces_and_nothing_else() -> None:
    pieces = split_text("Done.")
    assert len(pieces) == 1 and len(pieces[0]) == MIN_TEXT_CHARS and pieces[0].strip() == "Done."


def test_a_short_tail_joins_the_piece_before_it_instead_of_being_sent_alone() -> None:
    pieces = split_text("word " * 19 + "ok")  # 97 characters, then a 2-character tail
    assert all(len(p) >= MIN_TEXT_CHARS for p in pieces)


def test_a_single_word_longer_than_the_maximum_is_refused_not_cut() -> None:
    with pytest.raises(ValueError):
        split_text("x" * (MAX_TEXT_CHARS + 1))


def test_text_that_is_blank_is_refused() -> None:
    for blank in ("", "   ", "\n\t"):
        with pytest.raises(ValueError):
            split_text(blank)


def test_the_url_carries_the_voice_and_the_format_and_no_key() -> None:
    url = tts_url("wss://example.invalid", Voice("pcm", "pidgin", "female"))
    assert url.startswith("wss://example.invalid/tts/v1/stream?") and "voice_language=pcm" in url and "voice_accent=pidgin" in url and "voice_gender=female" in url
    assert "output_audio_format=wav" in url and "key" not in url.lower()


@pytest.mark.parametrize("bad", ["ws://example.invalid", "https://example.invalid", "", "wss://"])
def test_a_base_url_that_is_not_a_secure_websocket_is_refused(bad: str) -> None:
    with pytest.raises(ValueError):
        tts_url(bad, Voice("pcm", "pidgin", "female"))


def test_a_voice_gender_other_than_male_or_female_is_refused() -> None:
    with pytest.raises(ValueError):
        Voice("pcm", "pidgin", "other")


# ---- the adapter -----------------------------------------------------------------------------------------------------------------------------------------------------


def test_the_adapter_is_a_speech_synthesizer_with_the_capabilities_it_was_given() -> None:
    provider = adapter(Server())
    assert isinstance(provider, SpeechSynthesizer) and provider.name == "intron" and provider.capabilities is CAPS


def test_audio_comes_back_in_order_as_pcm_with_the_vendors_own_rate_and_the_header_removed() -> None:
    server = Server()
    text = LONG
    chunks = speak(server, text)
    assert len(chunks) == len(server.texts()) >= 2
    assert [c.sequence for c in chunks] == list(range(len(chunks)))  # type: ignore[attr-defined]
    assert all(c.codec is AudioCodec.PCM_S16LE and c.sample_rate_hz == RATE and c.data == b"\x10\x00" * 480 for c in chunks)  # type: ignore[attr-defined]
    assert [m["message_type"] for m in server.sent][-1] == "COMMIT" and server.closed
    assert " ".join(" ".join(server.texts()).split()) == " ".join(text.split())  # the adapter spoke the text it was given and no other


def test_the_chunks_are_acknowledged_by_id_and_each_is_fetched_by_id() -> None:
    server = Server()
    speak(server, LONG)
    sent_ids = [m["ack_id"] for m in server.sent if m["message_type"] == "INPUT_TEXT_CHUNK"]
    fetched = [m["chunk_id"] for m in server.sent if m["message_type"] == "FETCH_AUDIO_CHUNK"]
    assert sent_ids == list(range(len(sent_ids))) and sorted({int(str(f)) for f in fetched}) == sent_ids


def test_a_chunk_that_is_still_processing_is_asked_for_again_until_it_is_ready() -> None:
    server = Server(["PROCESSING", "PROCESSING", "READY"])
    chunks = speak(server, "Short enough to send at once.")
    assert len(chunks) == 1 and server.requests_for[0] == 3


def test_the_key_is_only_in_the_authorization_header() -> None:
    server = Server()
    speak(server, "Short enough to send at once.")
    assert server.headers == {"Authorization": f"Bearer {KEY}"} and KEY not in server.url and KEY not in json.dumps(server.sent)
    assert "voice_language=pcm" in server.url


def test_the_voice_comes_from_the_locale_and_a_locale_without_a_voice_is_refused_before_anything_is_sent() -> None:
    server = Server()
    with pytest.raises(ProviderError) as caught:
        speak(server, "Short enough to send at once.", Locale.HA)
    assert caught.value.kind is ProviderFailure.REJECTED and server.sent == [] and server.url == ""
    speak(Server(), "Short enough to send at once.", Locale.YO)


@pytest.mark.parametrize("reply,kind", [
    ({"message_type": "QUOTA_EXCEEDED", "credits_balance": "0", "message": "insufficient credits balance"}, ProviderFailure.RATE_LIMITED),
    ({"message_type": "RESOURCE_EXHAUSTED", "status": "CAPACITY_NOT_AVAILABLE"}, ProviderFailure.RATE_LIMITED),
    ({"message_type": "AUTHENTICATION_ERROR", "message": "permission denied,access-key error"}, ProviderFailure.REJECTED),
    ({"message_type": "SESSION_TIME_LIMIT_EXCEEDED", "session_time_limit": "300"}, ProviderFailure.TIMEOUT),
    ({"message_type": "INSUFFICIENT_TEXT_ACTIVITY", "message": "x"}, ProviderFailure.TIMEOUT),
    ({"message_type": "ERROR", "message": "connection error"}, ProviderFailure.UNAVAILABLE),
    ({"message_type": "INPUT_ERROR", "message": "invalid input"}, ProviderFailure.MALFORMED),
    ({"message_type": "CHUNK_SIZE_TOO_SMALL", "chunk_size_min": "10"}, ProviderFailure.MALFORMED),
    ({"message_type": "CHUNK_SIZE_TOO_LARGE", "chunk_size_max": "100"}, ProviderFailure.MALFORMED),
    ({"message_type": "SOMETHING_NEW"}, ProviderFailure.MALFORMED),
])
def test_a_vendor_error_is_a_typed_provider_error_with_no_body_in_it(reply: dict[str, str], kind: ProviderFailure) -> None:
    server = Server(raw_replies={"INPUT_TEXT_CHUNK": json.dumps(reply)})
    with pytest.raises(ProviderError) as caught:
        speak(server, "Short enough to send at once.")
    assert caught.value.kind is kind and server.closed
    assert KEY not in str(caught.value) and "credit" not in str(caught.value).lower()


@pytest.mark.parametrize("payload", [b"", b"not a wav at all", wav()[:20], wav(rate=0), wav(rate=16000)])  # the last: the header says 16 kHz, the message declares 48 kHz
def test_audio_that_is_not_a_16_bit_pcm_wav_is_malformed_never_played(payload: bytes) -> None:
    server = Server(raw_replies={"FETCH_AUDIO_CHUNK": audio_reply(0, "READY", payload)})
    with pytest.raises(ProviderError) as caught:
        speak(server, "Short enough to send at once.")
    assert caught.value.kind is ProviderFailure.MALFORMED


def test_a_stream_that_ends_with_no_audio_is_not_a_success() -> None:
    server = Server(raw_replies={"FETCH_AUDIO_CHUNK": audio_reply(0, "READY", wav(samples=0))})
    with pytest.raises(ProviderError) as caught:
        speak(server, "Short enough to send at once.")
    assert caught.value.kind is ProviderFailure.MALFORMED  # silence is never a spoken answer (the caller offers text)


@pytest.mark.parametrize("error", [OSError("refused"), TimeoutError(), ConnectionClosed()])
def test_a_connection_that_cannot_be_made_is_unavailable(error: Exception) -> None:
    async def call() -> None:
        async for _ in adapter(Server(), fail_connect=error).synthesize(SynthesisRequest("Short enough to send at once.", Locale.PCM, "pinned-1")):
            pass

    with pytest.raises(ProviderError) as caught:
        run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert caught.value.kind is ProviderFailure.UNAVAILABLE


def test_a_connection_that_drops_mid_stream_is_unavailable_and_is_closed() -> None:
    server = Server(drop_at=2)
    with pytest.raises(ProviderError) as caught:
        speak(server, LONG)
    assert caught.value.kind is ProviderFailure.UNAVAILABLE and server.closed


def test_a_caller_that_stops_listening_closes_the_connection() -> None:
    server = Server()

    async def call() -> None:
        stream = adapter(server).synthesize(SynthesisRequest(LONG, Locale.PCM, "pinned-1"))
        await stream.__anext__()
        await stream.aclose()  # type: ignore[attr-defined]

    run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert server.closed


def test_a_poll_interval_that_is_not_positive_is_refused() -> None:
    for bad in (0.0, -1.0, float("nan"), float("inf")):
        with pytest.raises(ValueError):
            adapter(Server(), poll_s=bad)
