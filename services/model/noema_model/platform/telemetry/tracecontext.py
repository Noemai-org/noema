"""The trace context the core sends on the internal link (REC-02; PERD REQ-AUD-004, REQ-OBS-002, REQ-SEC-003).

Implements: REQ-AUD-004, REQ-OBS-002.

May import: the standard library and the OpenTelemetry API.

Must never: trust a header for identity, or repair a value that is not a trace context. The route that uses this is mounted only on a listener that requires a client
certificate (UND-05, noema#555), so the header reaches it from the core only; the value is still read strictly, because a defect on the other side must not become a forged trace.

`parent_context` reads one `traceparent` header value: exactly W3C version 00, 55 characters, lower-case hex, neither identifier all zero, flags 00 or 01. Anything else is no
trace context (the answer is None); it is never adopted and never repaired. `tracestate` is not read.
"""

import re
from collections.abc import Mapping

from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.trace import NonRecordingSpan, SpanContext, TraceFlags

_TRACEPARENT = re.compile(r"00-([0-9a-f]{32})-([0-9a-f]{16})-(0[01])", re.ASCII)


def parent_context(headers: Mapping[str, str]) -> Context | None:
    value = headers.get("traceparent")
    if value is None:
        return None
    match = _TRACEPARENT.fullmatch(value)
    if match is None:
        return None
    trace_id, span_id = int(match.group(1), 16), int(match.group(2), 16)
    if trace_id == 0 or span_id == 0:
        return None
    remote = SpanContext(trace_id=trace_id, span_id=span_id, is_remote=True, trace_flags=TraceFlags(int(match.group(3), 16)))
    return trace.set_span_in_context(NonRecordingSpan(remote))
