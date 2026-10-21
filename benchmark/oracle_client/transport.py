"""A small JSON-over-HTTP client (standard library only). No redirects, http and https only, a bearer credential read from a file."""
import json
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

Json = dict[str, Any]


class HttpError(Exception):
    """A non-2xx answer. `code` is the error envelope's code (PERD §39.1) when the body has one."""

    def __init__(self, status: int, code: str, message: str) -> None:
        super().__init__(f"HTTP {status} {code}: {message}")
        self.status, self.code, self.message = status, code, message


class TransportError(Exception):
    """The request did not complete (connection refused, timeout, reset): the answer is unknown."""


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args: Any, **kwargs: Any) -> None:
        return None


def read_secret(path: Path) -> str:
    """A credential is a file's contents (the `*_FILE` convention), never an argument or an environment value."""
    value = Path(path).read_text(encoding="utf-8").strip()
    if not value:
        raise ValueError(f"the credential file {path} is empty")
    return value


class JsonClient:
    def __init__(self, base_url: str, *, token: str | None = None, timeout: float = 10.0) -> None:
        if not base_url.startswith(("http://", "https://")):
            raise ValueError("only http and https URLs are accepted")
        self.base_url, self._token, self.timeout = base_url.rstrip("/"), token, timeout
        self._opener = urllib.request.build_opener(_NoRedirect)

    def request(self, method: str, path: str, body: Any = None, headers: dict[str, str] | None = None) -> tuple[Json, dict[str, str]]:
        """Returns (decoded JSON body, response headers). Raises HttpError for a non-2xx answer and TransportError when no answer came."""
        data = None if body is None else json.dumps(body).encode("utf-8")
        request = urllib.request.Request(self.base_url + path, data=data, method=method)
        request.add_header("Accept", "application/json")
        if data is not None:
            request.add_header("Content-Type", "application/json")
        if self._token:
            request.add_header("Authorization", f"Bearer {self._token}")
        for key, value in (headers or {}).items():
            request.add_header(key, value)
        try:
            with self._opener.open(request, timeout=self.timeout) as response:
                raw = response.read()
                return (json.loads(raw) if raw else {}), dict(response.headers.items())
        except urllib.error.HTTPError as error:
            raw = error.read()
            try:
                envelope = json.loads(raw).get("error", {})
            except (ValueError, AttributeError):
                envelope = {}
            raise HttpError(error.code, str(envelope.get("code", "UNKNOWN")), str(envelope.get("message", ""))[:200]) from None
        except (urllib.error.URLError, TimeoutError, ConnectionError, OSError) as error:
            raise TransportError(f"{method} {path}: {type(error).__name__}") from None
