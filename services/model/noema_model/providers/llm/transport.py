"""The default Transport: standard-library HTTPS with a hard deadline, no redirects and a response size cap.

No new dependency (AGENT-14). Redirects are refused because the standard library would replay the credential header
to the redirect target. Plain HTTP is accepted only for a loopback host, and only when the caller constructs the
transport with allow_loopback_http (a test of the real wire path); the harness configuration cannot enable it.
"""

import asyncio
import ipaddress
import urllib.error
import urllib.request
from collections.abc import Mapping
from urllib.parse import urlsplit

from noema_model.providers.llm.base import HttpResponse, ProviderError, ProviderFailure


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


class UrllibTransport:
    def __init__(self, *, allow_loopback_http: bool = False) -> None:
        self._allow_loopback_http = allow_loopback_http
        self._opener = urllib.request.build_opener(_NoRedirect)

    def _check_url(self, url: str) -> None:
        parts = urlsplit(url)
        if parts.scheme == "https" and parts.hostname:
            return
        if parts.scheme == "http" and self._allow_loopback_http and parts.hostname and _is_loopback(parts.hostname):
            return
        raise ProviderError(ProviderFailure.REJECTED)

    def _post_blocking(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        request = urllib.request.Request(url, data=body, headers=dict(headers), method="POST")
        invalid_header = False
        try:
            with self._opener.open(request, timeout=timeout_s) as response:
                data = response.read(max_bytes + 1)
                status = response.status
        except urllib.error.HTTPError as exc:
            with exc:
                data = exc.read(max_bytes + 1)
                status = exc.code
        except TimeoutError as exc:
            raise ProviderError(ProviderFailure.TIMEOUT) from exc
        except (urllib.error.URLError, OSError) as exc:
            if isinstance(exc, urllib.error.URLError) and isinstance(exc.reason, TimeoutError):
                raise ProviderError(ProviderFailure.TIMEOUT) from exc
            raise ProviderError(ProviderFailure.UNAVAILABLE) from exc
        except ValueError:
            # The standard library's text for an invalid header holds the header value (the credential). Drop the exception: it is
            # raised below, outside this block, so nothing chains to it (INV-29, AGENT-13).
            invalid_header = True
        if invalid_header:
            raise ProviderError(ProviderFailure.REJECTED)
        if len(data) > max_bytes:
            raise ProviderError(ProviderFailure.MALFORMED, status)
        return HttpResponse(status=status, body=data)

    async def post(
        self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int
    ) -> HttpResponse:
        self._check_url(url)
        try:
            return await asyncio.wait_for(
                asyncio.to_thread(self._post_blocking, url, headers, body, timeout_s, max_bytes), timeout_s
            )
        except TimeoutError as exc:
            raise ProviderError(ProviderFailure.TIMEOUT) from exc
