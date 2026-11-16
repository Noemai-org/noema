"""Intron speech-to-text behind `SpeechProvider` (VOI-01, REQ-VOI-002, REQ-VOI-003, INV-07, AGENT-13; rulings A179 to A181, A191, A228, A241 and A249).

One streaming session per utterance. The adapter gathers the 20 ms frames of the caller's audio into Intron's chunks (1 KB to 32 KB), sends them while it listens, sends COMMIT when the
audio ends, and turns Intron's messages into the neutral events: PARTIAL_TRANSCRIPT becomes a `PartialTranscript` (for the screen only), COMMITTED_TRANSCRIPT becomes the one
`FinalTranscript`. Intron returns no confidence for any token, and none is invented: every token has `None` and so counts as low (A228, A191), which the caller measures. The last chunk
is padded with silence to the vendor's minimum, at the very end of the utterance only.

Nothing here owns a policy. The WebSocket connection and the key are arguments: `connect(url, headers)` returns the connection, `api_key()` reads the key at call time from where the
service keeps it (a 0600 file, A249), and the key travels only in the Authorization header, never in the URL, a log or an error. Every failure is a `ProviderError`: a refused or
dropped connection is UNAVAILABLE, a QUOTA_EXCEEDED is RATE_LIMITED, a message that is not understood is MALFORMED; the caller's deadline bounds the call, and a speech failure degrades to
text (A179). Audio that is not 16 kHz PCM16 is a caller bug and raises ValueError. Whether this provider may be used for a real speaker is not decided here: that is its capability row
(qualified, retention terms), which `voice/degrade.py` checks before any audio is sent (A180).
"""

import asyncio
from collections.abc import AsyncIterator, Awaitable, Callable, Mapping
from typing import Any, Protocol

from noema_model.providers.speech.base import (
    AudioChunk,
    AudioCodec,
    FinalTranscript,
    PartialTranscript,
    ProviderCapabilities,
    SpeechEvent,
    StreamRequest,
    TokenConfidence,
)
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_wire import (
    MAX_CHUNK_BYTES,
    MIN_CHUNK_BYTES,
    SAMPLE_RATE_HZ,
    ConnectionClosed,
    ServerMessage,
    audio_message,
    commit_message,
    parse_server_message,
    stt_url,
)


class Connection(Protocol):
    async def send(self, message: str) -> None: ...

    async def recv(self) -> str:
        """The next message, or raise `ConnectionClosed`."""
        ...

    async def close(self) -> None: ...


Connect = Callable[[str, Mapping[str, str]], Awaitable[Connection]]


class IntronSpeechToText:
    name = "intron"

    def __init__(self, *, capabilities: ProviderCapabilities, connect: Connect, base_url: str, api_key: Callable[[], str]) -> None:
        self.capabilities = capabilities
        self._connect = connect
        self._base_url = base_url
        self._api_key = api_key

    async def transcribe(self, request: StreamRequest, audio: AsyncIterator[AudioChunk]) -> AsyncIterator[SpeechEvent]:
        if request.locale_hint is None:
            raise ProviderError(ProviderFailure.REJECTED)  # the vendor needs a language and none is guessed
        url = stt_url(self._base_url, request.locale_hint)
        try:
            connection = await self._connect(url, {"Authorization": f"Bearer {self._api_key()}"})
        except (OSError, TimeoutError, ConnectionClosed):
            raise ProviderError(ProviderFailure.UNAVAILABLE) from None
        sender = asyncio.ensure_future(_send_audio(connection, audio))
        try:
            sequence = 0
            while True:
                raw = await _next_message(connection, sender)
                message = parse_server_message(raw)
                if message.kind == "QUOTA_EXCEEDED":
                    raise ProviderError(ProviderFailure.RATE_LIMITED)
                if message.kind == "PARTIAL_TRANSCRIPT":
                    yield PartialTranscript(sequence, message.text or "")
                    sequence += 1
                elif message.kind == "COMMITTED_TRANSCRIPT":
                    yield _final(message)
                    return
        finally:
            sender.cancel()
            try:
                await connection.close()
            except (OSError, ConnectionClosed):
                pass  # a connection that cannot be closed is already gone; there is nothing more to release


async def _next_message(connection: Connection, sender: "asyncio.Future[None]") -> str:
    """The next server message. If the audio sender fails first, its error is the call's error (a bad frame, a dropped connection) and is not hidden behind a wait."""
    receiving = asyncio.ensure_future(connection.recv())
    try:
        while not receiving.done():
            watched: set[asyncio.Future[Any]] = {receiving, sender}
            await asyncio.wait(watched, return_when=asyncio.FIRST_COMPLETED)
            if sender.done() and not receiving.done():
                error = None if sender.cancelled() else sender.exception()
                if error is not None:
                    raise error
                await asyncio.wait({receiving})  # the audio is all sent: wait for the server
        return receiving.result()
    except ConnectionClosed:
        raise ProviderError(ProviderFailure.UNAVAILABLE) from None
    finally:
        if not receiving.done():
            receiving.cancel()


def _final(message: ServerMessage) -> FinalTranscript:
    text = message.text or ""
    tokens = tuple(TokenConfidence(word, None) for word in text.split())
    return FinalTranscript(text, tokens, None, None, None)  # no confidence and no model version reported by the vendor: none is invented


async def _send_audio(connection: Connection, audio: AsyncIterator[AudioChunk]) -> None:
    """Gather frames into chunks of at least the vendor's minimum (at most its maximum), send them as they fill, then flush the rest and COMMIT."""
    pending = bytearray()
    ack_id = 0

    async def flush(final: bool) -> None:
        nonlocal ack_id
        while len(pending) >= MIN_CHUNK_BYTES or (final and pending):
            piece = bytes(pending[:MAX_CHUNK_BYTES])
            del pending[:MAX_CHUNK_BYTES]
            if len(piece) < MIN_CHUNK_BYTES:
                piece += b"\x00" * (MIN_CHUNK_BYTES - len(piece))  # silence after the end of speech, to reach the vendor's minimum
            await connection.send(audio_message(piece, ack_id))
            ack_id += 1

    async for item in audio:
        if item.codec is not AudioCodec.PCM_S16LE or item.sample_rate_hz != SAMPLE_RATE_HZ:
            raise ValueError("Intron speech-to-text takes 16 kHz PCM16 only")
        pending += item.data
        await flush(False)
    await flush(True)
    await connection.send(commit_message())
