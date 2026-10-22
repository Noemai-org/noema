"""The default transport on a real socket: deadline, no redirects, size cap, https only (INV-29, AGENT-13).

A tiny local HTTP server plays the vendor. The adapter, the transport and the harness are the real code.
"""

import json
import threading
import time
from collections.abc import Iterator
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

import pytest

from noema_model.harness import AttemptOutcome, GenerateRequest, Harness, PromptTemplate
from noema_model.harness.config import HarnessConfig, ProviderConfig, load_harness_config
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import AdapterSettings, ProviderError, ProviderFailure
from noema_model.providers.llm.openai_chat import OpenAIChat
from noema_model.providers.llm.transport import UrllibTransport
from pydantic import BaseModel, ConfigDict
from tests.support import FIXTURES, ManualClock, raw_config, run

OK_BODY = json.dumps({
    "model": "model-alpha-2026-01-01",
    "choices": [{"message": {"content": json.dumps({"surface": "20k"})}, "finish_reason": "stop"}],
    "usage": {"prompt_tokens": 9, "completion_tokens": 2},
}).encode()


class Out(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
    surface: str


class Server:
    def __init__(self) -> None:
        self.behaviour: dict[str, Any] = {"status": 200, "body": OK_BODY}
        self.seen: list[dict[str, str]] = []
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802
                self.rfile.read(int(self.headers.get("content-length", 0)))
                outer.seen.append({k.lower(): v for k, v in self.headers.items()})
                b = outer.behaviour
                time.sleep(b.get("delay", 0))
                self.send_response(b["status"])
                for k, v in b.get("headers", {}).items():
                    self.send_header(k, v)
                self.send_header("content-length", str(len(b["body"])))
                self.end_headers()
                self.wfile.write(b["body"])

            do_GET = do_POST  # noqa: N815  (a followed redirect arrives as a GET)

            def log_message(self, *args: Any) -> None:
                return None

        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.httpd.server_address[1]}"
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self) -> None:
        self.httpd.shutdown()
        self.httpd.server_close()


@pytest.fixture
def server() -> Iterator[Server]:
    s = Server()
    yield s
    s.close()


@pytest.fixture
def other() -> Iterator[Server]:
    s = Server()
    yield s
    s.close()


def adapter(url: str, *, timeout_ms: int = 500, max_bytes: int = 65536) -> OpenAIChat:
    settings = AdapterSettings(url, "SECRET-CRED", None, timeout_ms, max_bytes)
    return OpenAIChat("alpha", settings, UrllibTransport(allow_loopback_http=True))


def request() -> Any:
    from noema_model.providers.llm.base import ProviderRequest
    return ProviderRequest("model-alpha-2026-01-01", "S", "U", "n", {"type": "object"}, 8, None)


def test_a_real_round_trip_parses_and_sends_the_credential(server: Server) -> None:
    response = run(adapter(server.url).complete(request()))
    assert json.loads(response.output_json) == {"surface": "20k"} and response.tokens_in == 9
    assert server.seen[0]["authorization"] == "Bearer SECRET-CRED"


def test_a_slow_server_hits_the_deadline(server: Server) -> None:
    server.behaviour = {"status": 200, "body": OK_BODY, "delay": 1.0}
    started = time.monotonic()
    with pytest.raises(ProviderError) as caught:
        run(adapter(server.url, timeout_ms=100).complete(request()))
    assert caught.value.kind is ProviderFailure.TIMEOUT and time.monotonic() - started < 0.9


def test_a_redirect_is_not_followed_and_the_credential_is_not_replayed(server: Server, other: Server) -> None:
    server.behaviour = {"status": 302, "body": b"", "headers": {"location": other.url + "/v1/chat/completions"}}
    with pytest.raises(ProviderError) as caught:
        run(adapter(server.url).complete(request()))
    assert caught.value.kind is ProviderFailure.REJECTED
    assert other.seen == []


def test_an_oversize_body_is_refused(server: Server) -> None:
    server.behaviour = {"status": 200, "body": b"x" * 2000}
    with pytest.raises(ProviderError) as caught:
        run(adapter(server.url, max_bytes=1000).complete(request()))
    assert caught.value.kind is ProviderFailure.MALFORMED


def test_a_5xx_status_and_its_body_surface_as_unavailable_without_the_body(server: Server) -> None:
    server.behaviour = {"status": 503, "body": b"upstream says SECRET-CRED"}
    with pytest.raises(ProviderError) as caught:
        run(adapter(server.url).complete(request()))
    assert caught.value.kind is ProviderFailure.UNAVAILABLE and "SECRET-CRED" not in str(caught.value)


def test_a_closed_port_is_unavailable() -> None:
    dead = Server()
    url = dead.url
    dead.close()
    with pytest.raises(ProviderError) as caught:
        run(adapter(url).complete(request()))
    assert caught.value.kind is ProviderFailure.UNAVAILABLE


@pytest.mark.parametrize("url", ["http://127.0.0.1:1", "http://example.invalid", "ftp://example.invalid", "file:///etc/passwd", "https://"])
def test_the_default_transport_refuses_plaintext_and_odd_schemes(url: str) -> None:
    transport = UrllibTransport()
    with pytest.raises(ProviderError) as caught:
        run(transport.post(url, {}, b"{}", 1.0, 100))
    assert caught.value.kind is ProviderFailure.REJECTED


def test_plaintext_is_refused_even_when_loopback_is_allowed_for_other_hosts() -> None:
    with pytest.raises(ProviderError):
        run(UrllibTransport(allow_loopback_http=True).post("http://example.invalid", {}, b"{}", 1.0, 100))


def test_full_stack_failover_over_real_sockets(server: Server, other: Server) -> None:
    server.behaviour = {"status": 503, "body": b"down"}
    cfg = load_harness_config(raw_config(providers=("alpha", "beta")))
    providers = {
        n: ProviderConfig(**{**{f: getattr(p, f) for f in p.__slots__}, "endpoint": url})
        for (n, p), url in zip(cfg.providers.items(), (server.url, other.url), strict=True)
    }
    config = HarnessConfig(cfg.account_run_min_digits, cfg.data_limits, cfg.breaker, providers, cfg.routes)
    transport = UrllibTransport(allow_loopback_http=True)
    adapters = {n: OpenAIChat(n, AdapterSettings(p.endpoint, "CRED", None, p.timeout_ms, p.max_response_bytes), transport) for n, p in providers.items()}
    other.behaviour = {"status": 200, "body": OK_BODY.replace(b"model-alpha", b"model-beta")}
    harness = Harness(config, adapters, ManualClock(), InMemoryTelemetry())
    result = run(harness.generate(GenerateRequest("slot_values", PromptTemplate("t", 1, "S", "I"), {"utterance": "x"}, Out, None, "cor_test")))
    assert result.value == Out(surface="20k") and result.call.provider == "beta"
    assert [a.outcome for a in result.call.attempts] == [AttemptOutcome.UNAVAILABLE, AttemptOutcome.OK]
    assert FIXTURES.exists()
