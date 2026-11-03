"""One POST over HTTPS to the core, with mutual TLS, a hard deadline, no redirects and a response size cap.

The same rules as the provider transport, kept separate because only the harness may import `providers` (AGENT-28) and
this link carries a different identity: the client certificate. Plaintext HTTP is accepted only to a loopback host and only
when the caller constructs the transport that way (a test of the real wire path).
"""

import asyncio
import functools
import ipaddress
import ssl
import urllib.error
import urllib.request
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Protocol
from urllib.parse import urlsplit

from noema_model.platform.blocking import BoundedBlockingPool, PoolSaturated
from noema_model.platform.config import require_env_int
from noema_model.understanding.pipeline.types import CandidateSourceError

ENV_EVIDENCE_POOL = "NOEMA_MODEL_EVIDENCE_POOL_SIZE"


def evidence_pool_size(environ: Mapping[str, str]) -> int:
    """The evidence link's pool size, read strictly from the environment. There is no default in code (AGENT-16).

    Nothing in the service builds the evidence transport yet, so nothing calls this at start-up. The task that composes the evidence
    link must call it there, so that a missing or invalid value refuses to start, as the provider transport's does."""
    return require_env_int(environ, ENV_EVIDENCE_POOL, minimum=1)


@dataclass(frozen=True, slots=True)
class HttpResponse:
    status: int
    body: bytes


class EvidenceTransport(Protocol):
    async def post(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        """POST once. Raise CandidateSourceError for a timeout, a refused connection or an oversize body."""
        ...


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # type: ignore[no-untyped-def]
        return None


def _is_loopback(host: str) -> bool:
    if host == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


class UrllibEvidenceTransport:
    def __init__(self, ssl_context: ssl.SSLContext | None, *, pool_size: int, allow_loopback_http: bool = False) -> None:
        # Its own bounded pool, separate from the provider transport's (see platform/blocking).
        self._pool = BoundedBlockingPool(pool_size, name="evidence-transport")
        self._allow_loopback_http = allow_loopback_http
        handlers: list[urllib.request.BaseHandler] = [_NoRedirect()]
        if ssl_context is not None:
            handlers.append(urllib.request.HTTPSHandler(context=ssl_context))
        self._opener = urllib.request.build_opener(*handlers)
        self._has_context = ssl_context is not None

    def _check_url(self, url: str) -> None:
        parts = urlsplit(url)
        if parts.scheme == "https" and parts.hostname and self._has_context:
            return
        if parts.scheme == "http" and self._allow_loopback_http and parts.hostname and _is_loopback(parts.hostname):
            return
        raise CandidateSourceError("the evidence link must be https with a client certificate")

    def _post_blocking(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        request = urllib.request.Request(url, data=body, headers=dict(headers), method="POST")
        try:
            with self._opener.open(request, timeout=timeout_s) as response:
                data, status = response.read(max_bytes + 1), response.status
        except urllib.error.HTTPError as exc:
            with exc:
                data, status = exc.read(max_bytes + 1), exc.code
        except (urllib.error.URLError, OSError) as exc:  # includes timeouts and TLS failures
            raise CandidateSourceError("the evidence API did not answer") from exc
        if len(data) > max_bytes:
            raise CandidateSourceError("the evidence API answer was too large")
        return HttpResponse(status, data)

    async def post(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        self._check_url(url)
        try:
            return await asyncio.wait_for(
                self._pool.run(functools.partial(self._post_blocking, url, headers, body, timeout_s, max_bytes)), timeout_s
            )
        except PoolSaturated as exc:
            raise CandidateSourceError("the evidence link is busy") from exc
        except TimeoutError as exc:
            raise CandidateSourceError("the evidence API timed out") from exc
