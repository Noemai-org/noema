"""Traces, Prometheus-style metrics with enum-only labels, and structured JSON logs carrying the correlation chain.

Implements: REQ-OBS-001, REQ-OBS-002.

May import: noema_model.platform.redaction and declared dependencies.

Must never: log what the redactor has not seen; treat a log line as proof of an outcome (AGENT-21).

This module defines the narrow Telemetry interface the harness and the pipeline emit through, plus a null and an
in-memory implementation. The exporters (OpenTelemetry, Prometheus) are wired by the platform tasks (REC-02,
PLT-05) behind the same interface. Metric names are noema_<name>_<unit|total>; label values are members of a
finite set (task, provider name, outcome), never free text, an identifier or customer data.
"""

from collections.abc import Iterator, Mapping
from contextlib import AbstractContextManager, contextmanager, nullcontext
from dataclasses import dataclass, field
from typing import Protocol

from opentelemetry import trace

Attribute = str | int | bool


class Span(Protocol):
    def set_attribute(self, key: str, value: Attribute) -> None: ...


class Telemetry(Protocol):
    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None: ...

    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None: ...

    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None:
        """A structured log line. The caller passes only fields it has already minimised."""
        ...

    def span(self, name: str, correlation_id: str, attributes: Mapping[str, Attribute]) -> AbstractContextManager[Span]:
        """A trace span on the correlation chain (REQ-OBS-002). Attributes are enum-like, never customer text."""
        ...


class _NullSpan:
    def set_attribute(self, key: str, value: Attribute) -> None:
        return None


class NullTelemetry:
    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None:
        return None

    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None:
        return None

    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None:
        return None

    def span(self, name: str, correlation_id: str, attributes: Mapping[str, Attribute]) -> AbstractContextManager[Span]:
        return nullcontext(_NullSpan())


@dataclass
class InMemoryTelemetry:
    """Keeps everything it is given, for tests and for the benchmark's own accounting."""

    counts: list[tuple[str, tuple[tuple[str, str], ...], int]] = field(default_factory=list)
    observations: list[tuple[str, tuple[tuple[str, str], ...], int]] = field(default_factory=list)
    events: list[tuple[str, str, dict[str, str | int | bool]]] = field(default_factory=list)
    spans: list[tuple[str, str, dict[str, Attribute]]] = field(default_factory=list)

    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None:
        self.counts.append((name, tuple(sorted(labels.items())), value))

    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None:
        self.observations.append((name, tuple(sorted(labels.items())), value))

    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None:
        self.events.append((name, correlation_id, dict(fields)))

    def total(self, name: str, **labels: str) -> int:
        wanted = set(labels.items())
        return sum(v for n, ls, v in self.counts if n == name and wanted <= set(ls))

    @contextmanager
    def span(self, name: str, correlation_id: str, attributes: Mapping[str, Attribute]) -> Iterator[Span]:
        recorded = dict(attributes)
        self.spans.append((name, correlation_id, recorded))

        class _Recorder:
            def set_attribute(self, key: str, value: Attribute) -> None:
                recorded[key] = value

        yield _Recorder()


class OtelTelemetry:
    """Spans go to OpenTelemetry (a no-op until the platform tasks install an SDK and exporter); metrics and events go
    to the wrapped telemetry. The correlation id rides every span as an attribute."""

    def __init__(self, inner: Telemetry) -> None:
        self._inner = inner
        self._tracer = trace.get_tracer("noema_model")

    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None:
        self._inner.count(name, labels, value)

    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None:
        self._inner.observe(name, labels, value)

    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None:
        self._inner.event(name, correlation_id, fields)

    @contextmanager
    def span(self, name: str, correlation_id: str, attributes: Mapping[str, Attribute]) -> Iterator[Span]:
        with self._tracer.start_as_current_span(
            name, attributes={**attributes, "correlation_id": correlation_id}
        ) as otel_span:
            yield otel_span
