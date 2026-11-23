"""The WebSocket connect factory of the Intron adapters (VOI-01, REQ-VOI-002, INV-29, AGENT-13, AGENT-16; rulings A249, A380).

It is the one place that opens a WebSocket (`websockets` 17.3, approved by A249): it turns `connect(url, headers)` into the `Connection` the adapters expect. Every timeout and the largest
message are arguments with no default (AGENT-16). It uses no proxy (a proxy would see the key header), no compression and no user-agent string. Failures become the adapters' own: a refusal
by the server is a typed ProviderError (401 and 403 are REJECTED, 429 is RATE_LIMITED, anything else UNAVAILABLE) with no body and no header in it; a dropped connection is
`ConnectionClosed`; a text frame is the only kind accepted. These tests run against a real WebSocket server on the loopback interface of the test process (127.0.0.1, an ephemeral port,
no network beyond it); the key value is a SYNTHETIC low-entropy placeholder (the recorded AGENT-12 exception of noema#412).
"""

import asyncio
import contextlib
import http
from collections.abc import AsyncIterator, Awaitable, Callable
from typing import Any

import pytest
from websockets.asyncio.server import ServerConnection, serve
from websockets.http11 import Request, Response

from noema_model.providers.speech.errors import ProviderError, ProviderFailure
from noema_model.providers.speech.intron_connect import websocket_connector
from noema_model.providers.speech.intron_stt import Connect
from noema_model.providers.speech.intron_wire import ConnectionClosed
from tests.support import run

KEY = "synthetic-synthetic-synthetic"
GUARD_S = 8.0
SETTINGS: dict[str, Any] = {"open_timeout_s": 1.0, "close_timeout_s": 1.0, "ping_interval_s": None, "ping_timeout_s": None, "max_message_bytes": 4096}


def connector(**overrides: Any) -> Connect:
    return websocket_connector(**{**SETTINGS, **overrides})


@contextlib.asynccontextmanager
async def server(handler: Callable[[ServerConnection], Awaitable[None]], *, status: http.HTTPStatus | None = None) -> AsyncIterator[str]:
    def process_request(connection: ServerConnection, request: Request) -> Response | None:
        return connection.respond(status, "refused") if status is not None else None

    async with serve(handler, "127.0.0.1", 0, process_request=process_request, compression=None) as running:
        port = running.sockets[0].getsockname()[1]
        yield f"ws://127.0.0.1:{port}/path?query=1"


async def echo_headers(connection: ServerConnection) -> None:
    assert connection.request is not None
    await connection.send(str(connection.request.headers.get("Authorization")))
    await connection.send("second")
    await connection.recv()


def test_the_key_header_reaches_the_server_and_text_frames_round_trip() -> None:
    async def call() -> tuple[str, str]:
        async with server(echo_headers) as url:
            connection = await connector()(url, {"Authorization": f"Bearer {KEY}"})
            try:
                first, second = await connection.recv(), await connection.recv()
                await connection.send("ack")
                return first, second
            finally:
                await connection.close()

    assert run(asyncio.wait_for(call(), timeout=GUARD_S)) == (f"Bearer {KEY}", "second")


def test_a_binary_frame_is_malformed_never_decoded() -> None:
    async def handler(connection: ServerConnection) -> None:
        await connection.send(b"\x00\x01")
        await connection.wait_closed()

    async def call() -> None:
        async with server(handler) as url:
            connection = await connector()(url, {})
            try:
                await connection.recv()
            finally:
                await connection.close()

    with pytest.raises(ProviderError) as caught:
        run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert caught.value.kind is ProviderFailure.MALFORMED


def test_a_server_that_closes_is_connection_closed_on_receive_and_on_send() -> None:
    async def handler(connection: ServerConnection) -> None:
        await connection.close()

    async def call() -> None:
        async with server(handler) as url:
            connection = await connector()(url, {})
            with pytest.raises(ConnectionClosed):
                await connection.recv()
            with pytest.raises(ConnectionClosed):
                await connection.send("late")
            await connection.close()  # closing a closed connection is not an error

    run(asyncio.wait_for(call(), timeout=GUARD_S))


@pytest.mark.parametrize("status,kind", [
    (http.HTTPStatus.UNAUTHORIZED, ProviderFailure.REJECTED),
    (http.HTTPStatus.FORBIDDEN, ProviderFailure.REJECTED),
    (http.HTTPStatus.TOO_MANY_REQUESTS, ProviderFailure.RATE_LIMITED),
    (http.HTTPStatus.SERVICE_UNAVAILABLE, ProviderFailure.UNAVAILABLE),
    (http.HTTPStatus.NOT_FOUND, ProviderFailure.UNAVAILABLE),
])
def test_a_refusal_by_the_server_is_a_typed_provider_error_with_neither_the_key_nor_the_body_in_it(status: http.HTTPStatus, kind: ProviderFailure) -> None:
    async def never(connection: ServerConnection) -> None:
        await connection.wait_closed()

    async def call() -> None:
        async with server(never, status=status) as url:
            await connector()(url, {"Authorization": f"Bearer {KEY}"})

    with pytest.raises(ProviderError) as caught:
        run(asyncio.wait_for(call(), timeout=GUARD_S))
    assert caught.value.kind is kind
    assert KEY not in str(caught.value) and "refused" not in str(caught.value) and "127.0.0.1" not in str(caught.value)


def test_a_refused_connection_is_an_os_error_the_adapters_already_map_to_unavailable() -> None:
    async def call() -> None:
        async with server(echo_headers) as url:
            pass  # the server is closed again when this block ends
        await connector()(url, {})

    with pytest.raises(OSError):
        run(asyncio.wait_for(call(), timeout=GUARD_S))


def test_a_server_that_never_answers_the_handshake_is_a_timeout_at_the_open_bound() -> None:
    async def silent(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        await asyncio.sleep(30)  # accepts the TCP connection and says nothing

    async def call() -> float:
        listener = await asyncio.start_server(silent, "127.0.0.1", 0)
        port = listener.sockets[0].getsockname()[1]
        started = asyncio.get_running_loop().time()
        try:
            with pytest.raises(TimeoutError):
                await connector(open_timeout_s=0.2)(f"ws://127.0.0.1:{port}/", {})
        finally:
            listener.close()
        return asyncio.get_running_loop().time() - started

    assert run(asyncio.wait_for(call(), timeout=GUARD_S)) < 3.0


def test_a_message_larger_than_the_bound_closes_the_connection_instead_of_being_buffered() -> None:
    async def handler(connection: ServerConnection) -> None:
        await connection.send("x" * 10_000)
        await connection.wait_closed()

    async def call() -> None:
        async with server(handler) as url:
            connection = await connector(max_message_bytes=1024)(url, {})
            try:
                with pytest.raises(ConnectionClosed):
                    await connection.recv()
            finally:
                await connection.close()

    run(asyncio.wait_for(call(), timeout=GUARD_S))


def test_a_proxy_in_the_environment_is_never_used_because_it_would_see_the_key(monkeypatch: pytest.MonkeyPatch) -> None:
    for name in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy", "WS_PROXY", "WSS_PROXY"):
        monkeypatch.setenv(name, "http://127.0.0.1:1")  # nothing listens there; a client that used it would fail

    async def call() -> str:
        async with server(echo_headers) as url:
            connection = await connector()(url, {"Authorization": f"Bearer {KEY}"})
            try:
                return await connection.recv()
            finally:
                await connection.close()

    assert run(asyncio.wait_for(call(), timeout=GUARD_S)) == f"Bearer {KEY}"


@pytest.mark.parametrize("field", ["open_timeout_s", "close_timeout_s", "max_message_bytes"])
@pytest.mark.parametrize("bad", [0, -1, float("nan"), float("inf"), True])
def test_every_timeout_and_the_size_bound_must_be_positive_and_finite(field: str, bad: float) -> None:
    with pytest.raises(ValueError):
        connector(**{field: bad})


@pytest.mark.parametrize("field", ["ping_interval_s", "ping_timeout_s"])
def test_a_ping_setting_is_none_or_positive_and_finite(field: str) -> None:
    connector(**{field: None})
    connector(**{field: 5.0})
    for bad in (0, -1.0, float("nan"), float("inf")):
        with pytest.raises(ValueError):
            connector(**{field: bad})
