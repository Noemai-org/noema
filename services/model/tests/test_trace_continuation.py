"""REC-02, REQ-AUD-004, REQ-OBS-002, REQ-SEC-003; AGENT-13: the compile route continues the trace the core sends (a `traceparent` header on the internal link) and never lets a
header set identity. The route is mounted only on a listener that requires a client certificate (UND-05, noema#555), so the header reaches it from the core only.

A well-formed W3C `traceparent` is continued: the span the compiler runs in has the sender's trace id. A malformed one is not repaired and not adopted: the request is served in a
fresh context. The `Noema-Correlation-Id` header is never the identity of the request: the core's own `canonical_input.correlation_id` is (INV-12). All data is SYNTHETIC.
"""

import json
from collections.abc import Mapping
from typing import Any

import pytest
from fastapi.testclient import TestClient
from opentelemetry import trace

from noema_model.main import create_app
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.understanding.compiler import CompileService

from tests.test_compile_route import CANONICAL, PACK_REF, ROUTE, Packs, proposal

TRACE_ID = "0123456789abcdef0123456789abcdef"
GOOD = f"00-{TRACE_ID}-0123456789abcdef-01"


class Seeing:
    """The route's collaborator. It records the trace id of the span it runs in."""

    def __init__(self) -> None:
        self.trace_ids: list[int] = []

    async def compile(self, canonical_input: Mapping[str, Any], pack_ref: str, open_intent: Mapping[str, Any] | None) -> bytes:
        self.trace_ids.append(trace.get_current_span().get_span_context().trace_id)
        return json.dumps(proposal()).encode()


def serve(headers: Mapping[str, str]) -> tuple[Seeing, InMemoryTelemetry, int]:
    seen, telemetry = Seeing(), InMemoryTelemetry()
    service = CompileService(compiler=seen, packs=Packs(), max_proposal_bytes=65536, timeout_s=5.0, max_request_bytes=65536)
    c = TestClient(create_app(check=lambda: (True, "ok"), compile_service=service, telemetry=telemetry), raise_server_exceptions=False)
    r = c.post(ROUTE, content=json.dumps({"canonical_input": CANONICAL, "pack_ref": PACK_REF}), headers={"content-type": "application/json", **headers})
    return seen, telemetry, r.status_code


# REQ-OBS-002: one trace runs core to model service: the compiler runs under the trace id the core sent.
def test_a_good_traceparent_is_continued() -> None:
    seen, _, status = serve({"traceparent": GOOD})
    assert status == 200
    assert seen.trace_ids == [int(TRACE_ID, 16)]


# REQ-SEC-003: a value that is not exactly W3C version 00 is not a trace context. It is not adopted, not repaired, and the request is still served.
@pytest.mark.parametrize(
    "bad",
    [
        "",
        GOOD.upper(),
        "ff" + GOOD[2:],
        "00-00000000000000000000000000000000-0123456789abcdef-01",
        "00-" + TRACE_ID + "-0000000000000000-01",
        GOOD + "-extra",
        GOOD[:-1],
        " " + GOOD,
        "00-" + "g" * 32 + "-0123456789abcdef-01",
    ],
)
def test_a_malformed_traceparent_is_not_adopted_and_the_request_is_served(bad: str) -> None:
    seen, _, status = serve({"traceparent": bad})
    assert status == 200
    assert seen.trace_ids == [0], "no trace was adopted from a malformed header"


def test_a_request_without_a_traceparent_runs_in_no_adopted_trace() -> None:
    seen, _, status = serve({})
    assert status == 200 and seen.trace_ids == [0]


# AGENT-13, INV-12: the span carries the correlation id of the core's canonical input; a header never replaces it, and no span attribute is customer text.
def test_the_span_names_the_requests_own_correlation_id_and_not_the_headers() -> None:
    _, telemetry, _ = serve({"traceparent": GOOD, "Noema-Correlation-Id": "corr_0123456789"})
    spans = [s for s in telemetry.spans if s[0] == "model.compile"]
    assert len(spans) == 1
    _, corr, attributes = spans[0]
    assert corr == CANONICAL["correlation_id"] and corr != "corr_0123456789"
    assert all(isinstance(v, (bool, int)) or v in {"compile"} for v in attributes.values()), "enum-like attributes only"
