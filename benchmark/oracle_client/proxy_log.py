"""`monetary_calls` from the hostility proxy's per-route request log (Founder's decision of 4 Nov 2026, source (a); the log is ENV-09, `GET /control/v1/requests`).

The proxy sits between the runtime and the system behind it, so it sees every request that reached that system. A monetary call is a request that was FORWARDED on a route that is
not classified read-only. A request the proxy refused (a path outside its table) never reached a system and is not counted. A write whose answer was dropped still counts: it was
forwarded. A log that lost entries (`dropped`), that shrank, that reports no per-start `generation` (ruling A22), or that cannot be read is unavailable, never zero. The classification is data (`READ_ONLY`), and a test fails if the proxy's
route table and this list differ, so a new route is never silently read-only. Generated layer only; the bench credential is read from a file (INV-22, REQ-ENV-002)."""
import hashlib
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from benchmark.oracle_client.counters import CounterUnavailable, Mark
from benchmark.oracle_client.transport import HttpError, JsonClient, TransportError, read_secret

Json = dict[str, Any]
ROUTE = "/control/v1/requests"
# (target, operation) pairs of the proxy's route table, each classified READ-ONLY: none of them can move money (the POC has no adapter for a payout, BD-08, INV-05).
READ_ONLY = frozenset({
    ("mock-ledger", "get_transaction"), ("mock-ledger", "list_transactions"), ("mock-switch", "get_status"), ("mock-ledger", "get_entries"),
    ("mock-customer", "get_beneficiaries"), ("mock-destination", "events_feed"), ("mock-destination", "relay_pull"), ("mock-customer", "post_lookup"),
    # the Noema-side stand-in's two read routes (`config/routes.standin.json`, noema#207): both are GET reads, so a G1 run through the stand-in does not count them as monetary calls (Voice F1)
    ("mock-customer", "get_principal"), ("mock-destination", "get_destination_status"),
})


@dataclass(frozen=True)
class LogMark(Mark):
    """A position in the log plus what identifies the log up to it, so a reset that refills past the position is detected, not undercounted."""
    last_seq: Any = None
    prefix_digest: str = ""
    generation: Any = None


def _digest(entries: list[Any]) -> str:
    return hashlib.sha256(json.dumps(entries, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


class ProxyRouteLog:
    name = "monetary_calls"

    def __init__(self, base_url: str, credential_file: Path, *, timeout: float = 10.0) -> None:
        self._client = JsonClient(base_url, token=read_secret(credential_file), timeout=timeout)
        self._url = base_url

    def _log(self) -> Json:
        try:
            body, _ = self._client.request("GET", ROUTE)
        except HttpError as error:
            raise CounterUnavailable(self.name, f"the proxy refused the request log: {error}") from None
        except TransportError as error:
            raise CounterUnavailable(self.name, f"the proxy's request log cannot be read: {error}") from None
        if not isinstance(body.get("entries"), list):
            raise CounterUnavailable(self.name, "the proxy's request log has no entries list")
        generation = body.get("generation")
        if not isinstance(generation, str) or not generation:
            raise CounterUnavailable(self.name, "the proxy's request log reports no generation (a non-empty per-start identifier): without it a restart that replays an identical log prefix cannot be told from no restart, so the count would be a guess (ruling A22)")
        if body.get("dropped", 0):
            raise CounterUnavailable(self.name, f"the proxy's request log dropped {body['dropped']} entries (its limit was reached), so the count would be too low")
        return body

    def mark(self) -> Mark:
        body = self._log()
        entries = body["entries"]
        last = entries[-1].get("seq") if entries else None
        return LogMark(len(entries), last, _digest(entries), body.get("generation"))

    def since(self, mark: Mark) -> int:
        if not isinstance(mark, LogMark):
            raise CounterUnavailable(self.name, "the baseline mark does not identify the log")
        body = self._log()
        entries = body["entries"]
        n = mark.offset
        if len(entries) < n:
            raise CounterUnavailable(self.name, "the proxy's request log shrank: it was reset during the case")
        if body.get("generation") != mark.generation:
            raise CounterUnavailable(self.name, "the proxy's log generation changed: it was reset or restarted during the case")
        if (entries[n - 1].get("seq") if n else None) != mark.last_seq or _digest(entries[:n]) != mark.prefix_digest:
            raise CounterUnavailable(self.name, "the proxy's request log changed under the baseline: it was reset and refilled during the case")
        return sum(1 for e in entries[n:] if e.get("forwarded") and (e.get("target"), e.get("operation")) not in READ_ONLY)

    def describe(self) -> str:
        return (f"hostility proxy request log {self._url}{ROUTE}: forwarded requests on non-read-only routes since the baseline; the proxy's per-start generation is required, "
                "and a log that reports none is unavailable, never zero")
