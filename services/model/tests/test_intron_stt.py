"""The Intron speech-to-text adapter behind `SpeechProvider` (VOI-01, REQ-VOI-002, REQ-VOI-003, INV-07, AGENT-13; rulings A179 to A181, A191, A228, A241, A249).

A scripted WebSocket server stands in for Intron (SYNTHETIC; the recorded AGENT-12 exception of noema#412 covers the SpeechProvider seam: stalls, drops and malformed messages are
real asyncio waits and real exceptions). The adapter takes its connection as an argument, so no WebSocket library is needed to test it and the key never reaches a log. The audio is
synthetic PCM16 at 16 kHz, 20 ms frames of 640 bytes, which the adapter gathers into Intron's 1 KB minimum chunks.
"""

import asyncio
import base64
import json
from collections.abc import Mapping, Sequence

import pytest

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    FinalTranscript,
    Locale,
    PartialTranscript,
    PipelineMode,
    ProviderCapabilities,
    StreamRequest,
    Support,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_stt import IntronSpeechToText
from noema_model.providers.speech.intron_wire import MAX_CHUNK_BYTES, MIN_CHUNK_BYTES, ConnectionClosed
from noema_model.voice.confidence import slot_confidence
from noema_model.voice.pipeline import FinalUtterance, transcribe_utterance
from tests.support import run
from tests.support_voice import Source, chunk

KEY = "synthetic-key-0123456789"
REQUEST = StreamRequest(model_version="pinned-1", locale_hint=Locale.PCM)
CAPS = ProviderCapabilities("intron", PipelineMode.TRANSCRIBE_ONLY, Support.YES, (), (), None, Support.YES, "pinned-1", True)
GUARD_S = 5.0


def msg(kind: str, **fields: object) -> str:
    return json.dumps({"message_type": kind, **fields})


class Server:
    """A scripted Intron: records what the client sent, answers when the client commits (or earlier, per `script`), and can stall or drop."""

    def __init__(self, script: Sequence[str] = (), *, drop_after: int | None = None, stall: bool = False) -> None:
        self.url = ""
        self.headers: Mapping[str, str] = {}
        self.sent: list[dict[str, object]] = []
        self.closed = False
        self._script = list(script)
        self._drop_after = drop_after
        self._stall = stall
        self._queue: asyncio.Queue[str | None] = asyncio.Queue()
        self._received = 0

    async def send(self, message: str) -> None:
        self.sent.append(json.loads(message))
        self._received += 1
        if self._drop_after is not None and self._received >= self._drop_after:
            await self._queue.put(None)
            return
        if json.loads(message)["message_type"] == "COMMIT" and not self._stall:
            for line in self._script:
                await self._queue.put(line)

    async def recv(self) -> str:
        item = await self._queue.get()
        if item is None:
            raise ConnectionClosed
        return item

    async def close(self) -> None:
        self.closed = True

    def audio(self) -> list[bytes]:
        return [base64.b64decode(str(m["audio_base_64"])) for m in self.sent if m["message_type"] == "INPUT_AUDIO_CHUNK"]


def adapter(server: Server, *, fail_connect: Exception | None = None) -> IntronSpeechToText:
    async def connect(url: str, headers: Mapping[str, str]) -> Server:
        if fail_connect is not None:
            raise fail_connect
        server.url, server.headers = url, dict(headers)
        return server

    return IntronSpeechToText(capabilities=CAPS, connect=connect, base_url="wss://example.invalid", api_key=lambda: KEY)


def frames(n: int, level: int = 800) -> list[AudioChunk]:
    return [chunk(i, level) for i in range(n)]


def utterance(server: Server, n: int = 30) -> FinalUtterance:
    return run(asyncio.wait_for(transcribe_utterance(adapter(server), REQUEST, Source(frames(n)), deadline_s=2.0, cleanup_s=0.3, on_partial=lambda _: None), timeout=GUARD_S))


OK_SCRIPT = [msg("SESSION_CREATED", credit_balance=10), msg("AUDIO_CHUNK_ACK", ack_id=0), msg("PARTIAL_TRANSCRIPT", transcript="send fi"),
             msg("COMMITTED_TRANSCRIPT", transcript_id="t1", transcript_text="send fifty thousand naira", audio_len=0.6)]


def test_the_adapter_is_a_speech_provider_with_the_capabilities_it_was_given() -> None:
    from noema_model.providers.speech.base import SpeechProvider

    provider = adapter(Server())
    assert isinstance(provider, SpeechProvider) and provider.name == "intron" and provider.capabilities is CAPS


def test_frames_are_gathered_into_chunks_within_the_vendors_limits_and_no_audio_is_lost() -> None:
    server = Server(OK_SCRIPT)
    result = utterance(server, 30)
    sent = server.audio()
    assert result.text == "send fifty thousand naira"
    assert sent and all(MIN_CHUNK_BYTES <= len(c) <= MAX_CHUNK_BYTES for c in sent)
    original = b"".join(f.data for f in frames(30))
    joined = b"".join(sent)
    assert joined[: len(original)] == original  # every sample arrives in order
    assert set(joined[len(original):]) <= {0}  # only silence padding at the very end, to reach the vendor's minimum chunk
    assert len(joined) - len(original) < MIN_CHUNK_BYTES
    assert [m["message_type"] for m in server.sent][-1] == "COMMIT" and sum(m["message_type"] == "COMMIT" for m in server.sent) == 1
    assert [m["ack_id"] for m in server.sent if m["message_type"] == "INPUT_AUDIO_CHUNK"] == list(range(len(sent)))


def test_a_long_burst_is_split_at_the_vendors_maximum() -> None:
    big = [AudioChunk(0, AudioCodec.PCM_S16LE, 16000, b"\x10\x00" * 16_000)] * 4  # 4 x 32,000 bytes
    server = Server(OK_SCRIPT)
    run(asyncio.wait_for(transcribe_utterance(adapter(server), REQUEST, Source(big), deadline_s=2.0, cleanup_s=0.3, on_partial=lambda _: None), timeout=GUARD_S))
    assert all(MIN_CHUNK_BYTES <= len(c) <= MAX_CHUNK_BYTES for c in server.audio())


def test_the_key_is_only_in_the_authorization_header() -> None:
    server = Server(OK_SCRIPT)
    utterance(server)
    assert server.headers == {"Authorization": f"Bearer {KEY}"}
    assert KEY not in server.url and "use_language_asr_input=pcm" in server.url
    assert KEY not in json.dumps(server.sent)


def test_partials_go_to_the_display_in_order_and_the_final_has_a_missing_confidence_for_every_token() -> None:
    server = Server([msg("PARTIAL_TRANSCRIPT", transcript="send"), msg("PARTIAL_TRANSCRIPT", transcript="send fifty"),
                     msg("COMMITTED_TRANSCRIPT", transcript_id="t1", transcript_text="send fifty thousand naira", audio_len=0.6)])
    shown: list[str] = []
    result = run(asyncio.wait_for(transcribe_utterance(adapter(server), REQUEST, Source(frames(30)), deadline_s=2.0, cleanup_s=0.3, on_partial=shown.append), timeout=GUARD_S))
    assert shown == ["send", "send fifty"] and result.text == "send fifty thousand naira"
    assert result.token_confidences == (None, None, None, None) and result.utterance_confidence is None  # Intron returns none: it is not invented (A228)
    assert slot_confidence(result.token_confidences) == 0.0  # so every key slot counts as low, is measured, and is confirmed with the person


def test_session_and_ack_messages_are_not_events() -> None:
    server = Server([msg("SESSION_CREATED", credit_balance=1), msg("AUDIO_CHUNK_ACK", ack_id=0), msg("AUDIO_CHUNK_ACK", ack_id=1),
                     msg("COMMITTED_TRANSCRIPT", transcript_id="t1", transcript_text="send ten", audio_len=0.6)])
    events = run(asyncio.wait_for(_events(adapter(server), Source(frames(10))), timeout=GUARD_S))
    assert [type(e) for e in events] == [FinalTranscript]


async def _events(provider: IntronSpeechToText, source: Source) -> list[object]:
    return [event async for event in provider.transcribe(REQUEST, source)]


@pytest.mark.parametrize("script,kind", [
    ([msg("QUOTA_EXCEEDED")], ProviderFailure.RATE_LIMITED),
    (["not json"], ProviderFailure.MALFORMED),
    ([msg("SOMETHING_NEW")], ProviderFailure.MALFORMED),
    ([msg("PARTIAL_TRANSCRIPT", transcript="send")], ProviderFailure.UNAVAILABLE),  # the server then says nothing more: the connection ends, no final
])
def test_a_failure_is_a_typed_provider_error_never_a_guessed_utterance(script: list[str], kind: ProviderFailure) -> None:
    server = Server(script)
    if kind is ProviderFailure.UNAVAILABLE:
        server = Server(script, drop_after=3)
    with pytest.raises(ProviderError) as caught:
        run(asyncio.wait_for(transcribe_utterance(adapter(server), REQUEST, Source(frames(10)), deadline_s=2.0, cleanup_s=0.3, on_partial=lambda _: None), timeout=GUARD_S))
    assert caught.value.kind is kind
    assert KEY not in str(caught.value) and server.closed


@pytest.mark.parametrize("error", [OSError("refused"), TimeoutError(), ConnectionClosed()])
def test_a_connection_that_cannot_be_made_is_unavailable_and_the_source_is_still_closed(error: Exception) -> None:
    source = Source(frames(4))
    with pytest.raises(ProviderError) as caught:
        run(asyncio.wait_for(transcribe_utterance(adapter(Server(), fail_connect=error), REQUEST, source, deadline_s=2.0, cleanup_s=0.3, on_partial=lambda _: None), timeout=GUARD_S))
    assert caught.value.kind is ProviderFailure.UNAVAILABLE and source.closed


def test_a_server_that_goes_silent_is_a_timeout_at_the_deadline_and_the_connection_is_closed() -> None:
    server = Server(OK_SCRIPT, stall=True)
    with pytest.raises(ProviderError) as caught:
        run(asyncio.wait_for(transcribe_utterance(adapter(server), REQUEST, Source(frames(10)), deadline_s=0.2, cleanup_s=0.3, on_partial=lambda _: None), timeout=GUARD_S))
    assert caught.value.kind is ProviderFailure.TIMEOUT
    assert server.closed  # the connection is released at the deadline, and the sender task is gone with it


def test_a_request_with_no_locale_is_refused_because_the_vendor_needs_a_language_and_none_is_guessed() -> None:
    server = Server(OK_SCRIPT)
    with pytest.raises(ProviderError) as caught:
        run(asyncio.wait_for(_events_with(adapter(server), StreamRequest("pinned-1", None), Source(frames(4))), timeout=GUARD_S))
    assert caught.value.kind is ProviderFailure.REJECTED and server.sent == []


async def _events_with(provider: IntronSpeechToText, request: StreamRequest, source: Source) -> list[object]:
    return [event async for event in provider.transcribe(request, source)]


@pytest.mark.parametrize("bad", [AudioChunk(0, AudioCodec.OPUS, 16000, b"\x00" * 640), AudioChunk(0, AudioCodec.PCM_S16LE, 48000, b"\x00" * 640)])
def test_audio_that_is_not_16_khz_pcm16_is_refused_and_the_connection_is_closed(bad: AudioChunk) -> None:
    server = Server(OK_SCRIPT)
    with pytest.raises(ValueError):
        run(asyncio.wait_for(transcribe_utterance(adapter(server), REQUEST, Source([bad]), deadline_s=2.0, cleanup_s=0.3, on_partial=lambda _: None), timeout=GUARD_S))
    assert server.closed
