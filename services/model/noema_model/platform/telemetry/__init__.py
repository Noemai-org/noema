"""Traces, Prometheus-style metrics with enum-only labels, and structured JSON logs carrying the correlation chain.

Implements: REQ-OBS-001, REQ-OBS-002.

May import: noema_model.platform.redaction and declared dependencies.

Must never: log what the redactor has not seen; treat a log line as proof of an outcome (AGENT-21).

This module defines the narrow Telemetry interface the harness and the pipeline emit through, plus a null and an
in-memory implementation. The exporters (OpenTelemetry, Prometheus) are wired by the platform tasks (REC-02,
PLT-05) behind the same interface. Metric names are noema_<name>_<unit|total>; label values are members of a
finite set (task, provider name, outcome), never free text, an identifier or customer data.
"""

import re
from collections.abc import Iterator, Mapping
from contextlib import AbstractContextManager, contextmanager, nullcontext
from dataclasses import dataclass, field
from pathlib import Path
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


COST_EVENT = "MODEL_CALL_RECORDED"
"""Emitted by the harness once per call. Its one field, `record`, is the call's `CallRecord.to_dict()` as JSON."""

_UNSAFE = re.compile(r"[^A-Za-z0-9_-]")


class CostFileTelemetry:
    """Bench runs only: writes each call's record to <out>/cost/<call_id>.json, the same JSON as `CallRecord.to_dict()`,
    so the benchmark can read provider cost (REQ-NFR-007). Everything else passes through to the wrapped telemetry.

    A call id is the correlation id plus a three-digit sequence; the file is created exclusively, so a record already on
    disk is never overwritten. A write that fails raises: a bench run with missing cost must not look complete."""

    def __init__(self, inner: Telemetry, out: Path) -> None:
        self._inner = inner
        self._dir = out / "cost"

    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None:
        self._inner.count(name, labels, value)

    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None:
        self._inner.observe(name, labels, value)

    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None:
        self._inner.event(name, correlation_id, fields)
        if name == COST_EVENT:
            self._write(correlation_id, str(fields["record"]))

    def span(self, name: str, correlation_id: str, attributes: Mapping[str, Attribute]) -> AbstractContextManager[Span]:
        return self._inner.span(name, correlation_id, attributes)

    def _write(self, correlation_id: str, record: str) -> None:
        self._dir.mkdir(parents=True, exist_ok=True)
        stem = _UNSAFE.sub("_", correlation_id)[:128] or "call"  # the id names the file; it never chooses the directory
        sequence = 1
        while True:
            try:
                with open(self._dir / f"{stem}-{sequence:03d}.json", "x", encoding="utf-8") as handle:
                    handle.write(record)
                return
            except FileExistsError:
                sequence += 1


def cost_files_from_environment(inner: Telemetry, environ: Mapping[str, str]) -> Telemetry:
    """Wrap `inner` in cost files when BENCH_OUT is set (the Makefile's bench output directory); otherwise return it as is.
    The caller passes the environment, so production wiring that passes none never writes a file."""
    out = environ.get("BENCH_OUT", "")
    return CostFileTelemetry(inner, Path(out)) if out else inner
