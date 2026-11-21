"""Intron text-to-speech behind `SpeechSynthesizer` (VOI-04, REQ-VOI-002, INV-07, AGENT-13; rulings A179 to A181, A241 and A249).

One streaming session per answer. The text the core has already masked is split at word boundaries into the vendor's 10 to 100 character pieces (nothing added, dropped or reordered, A181),
each piece is sent, and the audio of piece 0, 1, 2 and so on is fetched in that order, asking again while the server says it is still processing, and yielded as 16-bit mono PCM at the rate
the vendor declares, the WAV header removed. COMMIT ends the session. The first audio is yielded as soon as the first piece is ready, which is what the first-spoken-word latency of VOI-16
measures. A stream that yields no audio is a failure, never silence-as-success.

The connection, the key, the voice for each locale and the polling interval are arguments (AGENT-16: no default): `connect(url, headers)`, `api_key()` read at call time from the service's
0600 file (A249), and the key travels only in the Authorization header. Every failure is a `ProviderError` and the voice path then shows the text (A179); the caller's deadline bounds the
call, the connection is closed on every exit, and a caller that stops listening closes it too. A locale with no voice is refused before anything is sent.
"""

import asyncio
import math
from collections.abc import AsyncIterator, Callable, Mapping

from noema_model.providers.speech.base import AudioChunk, AudioCodec, Locale, ProviderCapabilities
from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_stt import Connect, Connection
from noema_model.providers.speech.intron_tts_wire import TtsMessage, Voice, fetch_message, parse_server_message, split_text, text_message, tts_url
from noema_model.providers.speech.intron_wire import ConnectionClosed, commit_message
from noema_model.providers.speech.tts import SynthesisRequest

__all__ = ("IntronTextToSpeech", "Voice")


class IntronTextToSpeech:
    name = "intron"

    def __init__(self, *, capabilities: ProviderCapabilities, connect: Connect, base_url: str, api_key: Callable[[], str], voices: Mapping[Locale, Voice], poll_s: float) -> None:
        if not (math.isfinite(poll_s) and poll_s > 0):
            raise ValueError("the polling interval must be a positive finite number from configuration")
        self.capabilities = capabilities
        self._connect = connect
        self._base_url = base_url
        self._api_key = api_key
        self._voices = dict(voices)
        self._poll_s = poll_s

    async def synthesize(self, request: SynthesisRequest) -> AsyncIterator[AudioChunk]:
        voice = self._voices.get(request.locale)
        if voice is None:
            raise ProviderError(ProviderFailure.REJECTED)  # no voice is chosen for this locale and none is guessed
        pieces = split_text(request.text)
        url = tts_url(self._base_url, voice)
        try:
            connection = await self._connect(url, {"Authorization": f"Bearer {self._api_key()}"})
        except (OSError, TimeoutError, ConnectionClosed):
            raise ProviderError(ProviderFailure.UNAVAILABLE) from None
        try:
            for index, piece in enumerate(pieces):
                await connection.send(text_message(piece, index))
            spoken = 0
            for index in range(len(pieces)):
                while True:
                    await connection.send(fetch_message(index))
                    reply = await _reply(connection, "FETCH_AUDIO_CHUNK")
                    if reply.chunk_id != index:
                        raise ProviderError(ProviderFailure.MALFORMED)  # a reply for another chunk is never accepted, ready or not: it could repeat or drop part of the answer (Codex P2 on #640)
                    if reply.ready:
                        break
                    await asyncio.sleep(self._poll_s)
                yield AudioChunk(spoken, AudioCodec.PCM_S16LE, reply.sample_rate_hz, reply.pcm)
                spoken += 1
            await connection.send(commit_message())
            await _reply(connection, "COMMITTED_AUDIO")
        except ConnectionClosed:
            raise ProviderError(ProviderFailure.UNAVAILABLE) from None
        finally:
            try:
                await connection.close()
            except (OSError, ConnectionClosed):
                pass  # a connection that cannot be closed is already gone


async def _reply(connection: Connection, wanted: str) -> TtsMessage:
    """The next message of the kind wanted, skipping the session and acknowledgement messages; any vendor error raises its typed ProviderError."""
    while True:
        message = parse_server_message(await connection.recv())
        if message.kind == wanted:
            return message
        if message.kind not in ("SESSION_CREATED", "TEXT_CHUNK_ACK"):
            raise ProviderError(ProviderFailure.MALFORMED)  # a message that is neither the answer nor the housekeeping is not skipped

