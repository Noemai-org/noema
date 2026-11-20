"""The WebSocket connect factory of the Intron adapters (VOI-01, REQ-VOI-002, INV-29, AGENT-13, AGENT-16; rulings A249, A380).

The one place in the model service that opens a WebSocket (`websockets` 17.3, approved by A249). `websocket_connector(...)` returns the `Connect` the adapters take: `connect(url, headers)`
gives a `Connection` with `send`, `recv` and `close`. Every timeout and the largest message are arguments with no default (AGENT-16). The adapters build only `wss://` addresses (`secure_netloc` refuses anything else), so the key header travels only over TLS in production; this function connects to the address it is given and does not itself
refuse `ws://` (the tests use the loopback interface of the test process). The client uses no proxy (a proxy would see the key
header), no compression and no user-agent string. Failures become the adapters' own: a refusal by the server is a ProviderError with no body, header or address in it (401 and 403
REJECTED, 429 RATE_LIMITED, anything else UNAVAILABLE); a dropped connection is `ConnectionClosed`; an open that does not finish in time is TimeoutError; a refused TCP connection stays an
OSError; only text frames are accepted (a binary frame is MALFORMED).
"""

import math
from collections.abc import Mapping

from websockets.asyncio.client import ClientConnection, connect
from websockets.exceptions import ConnectionClosed as WsConnectionClosed
from websockets.exceptions import InvalidStatus, WebSocketException

from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_stt import Connect, Connection
from noema_model.providers.speech.intron_wire import ConnectionClosed

__all__ = ("websocket_connector",)

_REJECTED = {401, 403}
_RATE_LIMITED = {429}


def _positive(value: float, what: str) -> float:
    if isinstance(value, bool) or not (math.isfinite(value) and value > 0):
        raise ValueError(f"{what} must be a positive finite number from configuration")
    return value


def _positive_or_none(value: float | None, what: str) -> float | None:
    return None if value is None else _positive(value, what)


class _Socket:
    """The adapters' `Connection` over one websockets connection. Text frames only; the vendor's close or a drop is `ConnectionClosed`."""

    def __init__(self, socket: ClientConnection) -> None:
        self._socket = socket

    async def send(self, message: str) -> None:
        try:
            await self._socket.send(message)
        except WsConnectionClosed:
            raise ConnectionClosed from None

    async def recv(self) -> str:
        try:
            frame = await self._socket.recv()
        except WsConnectionClosed:
            raise ConnectionClosed from None
        if not isinstance(frame, str):
            raise ProviderError(ProviderFailure.MALFORMED)  # a binary frame is not part of either protocol
        return frame

    async def close(self) -> None:
        try:
            await self._socket.close()
        except WsConnectionClosed:
            return  # already closed


def _refusal(error: InvalidStatus) -> ProviderError:
    status = error.response.status_code
    if status in _REJECTED:
        return ProviderError(ProviderFailure.REJECTED)
    if status in _RATE_LIMITED:
        return ProviderError(ProviderFailure.RATE_LIMITED)
    return ProviderError(ProviderFailure.UNAVAILABLE)


def websocket_connector(
    *,
    open_timeout_s: float,
    close_timeout_s: float,
    ping_interval_s: float | None,
    ping_timeout_s: float | None,
    max_message_bytes: int,
) -> Connect:
    """The `Connect` for the adapters. `ping_interval_s` and `ping_timeout_s` may be None (no keep-alive pings) and are otherwise positive; the rest are positive and finite."""
    open_s = _positive(open_timeout_s, "the open timeout")
    close_s = _positive(close_timeout_s, "the close timeout")
    interval_s = _positive_or_none(ping_interval_s, "the ping interval")
    ping_s = _positive_or_none(ping_timeout_s, "the ping timeout")
    largest = int(_positive(max_message_bytes, "the largest message"))

    async def open_connection(url: str, headers: Mapping[str, str]) -> Connection:
        try:
            socket = await connect(
                url,
                additional_headers=dict(headers),
                open_timeout=open_s,
                close_timeout=close_s,
                ping_interval=interval_s,
                ping_timeout=ping_s,
                max_size=largest,
                compression=None,
                proxy=None,
                user_agent_header=None,
            )
        except InvalidStatus as error:
            raise _refusal(error) from None
        except WebSocketException:
            raise ConnectionClosed from None  # a failed handshake that is not a status: the adapters map it to UNAVAILABLE
        return _Socket(socket)

    return open_connection


