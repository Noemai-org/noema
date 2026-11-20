"""The harness's read-only reader of Tushe's two reads (PLAN-tushe-readonly-fold.md section 3.4; delta v1.3 section 3; A240; AT-05, AT-14, AT-17).

`TusheReadOnly.transitions(account, payout)` reads `GET /v1/accounts/{a}/payouts/{p}/transitions`; `read_feed(after, limit, max_pages)` and `events_for_trace(...)` read
`GET /v1/events?after={sequence}&limit={n}`, paged on the sequence and never on a timestamp. The folding and the trace join are the pure functions of `tushe_fold` and `tushe_trace`; this
module only reads, and every row it hands over has passed the checks the fold needs.

What it guarantees, and what it does not know:
- GET only, one request per read, no retry, no redirect followed (the transport refuses them). The key is read from a file, held only inside a closure, and appears in no error, repr or attribute.
- A failure is never "no rows": 401/403/other 4xx and a redirect are REFUSED, 404 NOT_FOUND, 429, 5xx, a refused connection and a timeout UNAVAILABLE, a 2xx it cannot read MALFORMED
  (UNKNOWN to the caller), a bad argument INVALID (nothing is sent). The feed is read until an EMPTY page (a short page is not the end: a server may cap its page size); one that is not
  empty within `max_pages` is an error, never a truncated list.
- Nothing here was observed against Tushe (AGENTS section 2, item 4). How the key is presented (`bearer` or `x-api-key`) and the envelope of each list (a bare array, or an object holding the
  array under a key) are required parameters with no default, so adopting the real ones changes one value. http is refused except for a loopback host when asked for (the local stand-in).
SYNTHETIC in every test. The key is the Founder's read-only key (A240); this code never creates, rotates or reveals one."""
import re
from collections.abc import Callable, Mapping
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from benchmark.oracle_client.transport import HttpError, JsonClient, TransportError, read_secret

Json = dict[str, Any]
PRESENTATIONS = ("bearer", "x-api-key")
LOOPBACK = ("127.0.0.1", "localhost", "::1")
ID = re.compile(r"[A-Za-z0-9_-]{1,64}")
CODE = re.compile(r"[A-Z0-9_:.\-]{1,64}")


class TusheReadError(Exception):
    """`kind` is one of REFUSED, NOT_FOUND, UNAVAILABLE, MALFORMED, INVALID. `code` is a plain code or ADAPTER:...; the server's own words and the key never appear."""

    def __init__(self, kind: str, code: str, message: str) -> None:
        super().__init__(f"{kind} {code}: {message}")
        self.kind, self.code = kind, code


def _int(value: Any) -> bool:
    return type(value) is int


def _text(value: Any) -> bool:
    return isinstance(value, str) and bool(value)


class TusheReadOnly:
    __slots__ = ("_get", "_events_field", "_transitions_field", "_base")

    def __init__(self, base_url: str, key_file: Path, *, present: str, events_field: str | None, transitions_field: str | None, loopback_http: bool = False, timeout: float = 10.0) -> None:
        if present not in PRESENTATIONS:
            raise TusheReadError("INVALID", "ADAPTER:INVALID_INPUT", f"present must be one of {PRESENTATIONS}")
        parts = urlsplit(base_url)
        host = parts.hostname or ""
        if parts.scheme == "https" and host:
            pass
        elif parts.scheme == "http" and loopback_http and host in LOOPBACK:
            pass
        else:
            raise TusheReadError("INVALID", "ADAPTER:INVALID_INPUT", "the base URL must be https (http only for a loopback host, and only when asked for)")
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or timeout <= 0:
            raise TusheReadError("INVALID", "ADAPTER:INVALID_INPUT", "the timeout must be a positive number")
        try:
            key = read_secret(key_file)
        except (OSError, ValueError):
            raise TusheReadError("INVALID", "ADAPTER:INVALID_INPUT", "the key file is missing or empty") from None
        client = JsonClient(base_url, token=key if present == "bearer" else None, timeout=float(timeout))
        headers = {"X-API-Key": key} if present == "x-api-key" else None

        def get(path: str) -> Any:
            return client.request("GET", path, headers=headers)[0]

        self._get: Callable[[str], Any] = get
        self._events_field, self._transitions_field, self._base = events_field, transitions_field, parts.hostname

    def __repr__(self) -> str:
        return f"TusheReadOnly(host={self._base!r})"

    def _read(self, path: str) -> Any:
        try:
            return self._get(path)
        except HttpError as error:
            code = error.code if CODE.fullmatch(error.code) else "UNKNOWN"
            if error.status == 404:
                raise TusheReadError("NOT_FOUND", code, "the route or the resource is not found") from None
            if error.status == 429 or error.status >= 500:
                raise TusheReadError("UNAVAILABLE", code, f"the service answered {error.status}") from None
            raise TusheReadError("REFUSED", code, f"the service answered {error.status}") from None
        except TransportError:
            raise TusheReadError("UNAVAILABLE", "TRANSPORT", "no answer came (connection or timeout)") from None
        except ValueError:
            raise TusheReadError("MALFORMED", "UNRECOGNISED", "the body is not JSON") from None

    @staticmethod
    def _list(body: Any, field: str | None) -> list[Any]:
        found = body if field is None else (body.get(field) if isinstance(body, dict) else None)
        if not isinstance(found, list):
            raise TusheReadError("MALFORMED", "UNRECOGNISED", "the answer is not the list shape this reader was given")
        return found

    def transitions(self, account_id: str, payout_id: str) -> list[Json]:
        for value in (account_id, payout_id):
            if not isinstance(value, str) or not ID.fullmatch(value):
                raise TusheReadError("INVALID", "ADAPTER:INVALID_INPUT", "an id may hold only letters, digits, underscore and dash, 1 to 64 characters")
        rows = self._list(self._read(f"/v1/accounts/{account_id}/payouts/{payout_id}/transitions"), self._transitions_field)
        last = 0
        out: list[Json] = []
        for row in rows:
            if not isinstance(row, dict) or not _int(row.get("seq")) or row["seq"] <= last or not _text(row.get("toState")) or not _text(row.get("occurredAt")):
                raise TusheReadError("MALFORMED", "UNRECOGNISED", "a transition row is unreadable, or seq does not strictly rise")
            if row.get("fromState") is not None and not _text(row["fromState"]):
                raise TusheReadError("MALFORMED", "UNRECOGNISED", "a transition row has an unreadable fromState")
            last = row["seq"]
            out.append(dict(row))
        return out

    def read_feed(self, after: int, limit: int, max_pages: int) -> list[Json]:
        if not (_int(after) and after >= 0 and _int(limit) and limit >= 1 and _int(max_pages) and max_pages >= 1):
            raise TusheReadError("INVALID", "ADAPTER:INVALID_INPUT", "after must be a whole number of at least 0; limit and max_pages whole numbers of at least 1")
        cursor, out = after, []
        for _ in range(max_pages):
            page = self._list(self._read(f"/v1/events?after={cursor}&limit={limit}"), self._events_field)
            for event in page:
                if not isinstance(event, dict) or not _int(event.get("sequence")) or event["sequence"] <= cursor or not _text(event.get("id")):
                    raise TusheReadError("MALFORMED", "UNRECOGNISED", "an event is unreadable, or its sequence does not strictly rise past the cursor")
                cursor = event["sequence"]
                out.append(dict(event))
            if not page:  # only an empty page ends the read: a short page may be a server that caps its page size (C1's read of noema#772)
                return out
        raise TusheReadError("INVALID", "ADAPTER:FEED_PAGE_LIMIT", "the feed was not caught up within max_pages; a truncated list is never returned")

    def events_for_trace(self, trace_id: str, after: int, limit: int, max_pages: int) -> list[Json]:
        """The delta's two-call join: there is no filter on `/v1/events`, so the feed is read and filtered on `traceId`, in sequence order."""
        if not _text(trace_id):
            raise TusheReadError("INVALID", "ADAPTER:INVALID_INPUT", "a trace id is required")
        return [e for e in self.read_feed(after, limit, max_pages) if isinstance(e, Mapping) and e.get("traceId") == trace_id]
