"""Bench-only cost files for the model service (REQ-NFR-007), moved out of the service's runtime code by the Architect's ruling A55 (8 Nov 2026, noema#293).

`CostFileTelemetry` wraps a telemetry object and, for each `MODEL_CALL_RECORDED` event, also writes the record to <out>/cost/<correlation_id>-<NNN>.json, the same
JSON as the service's `CallRecord.to_dict()`, so the benchmark can read provider cost. It uses only the standard library and the four telemetry methods, so it needs
no import from the service. Everything else passes through to the wrapped telemetry unchanged.

The name is exact. A correlation id with a character outside [A-Za-z0-9_-] is refused, not rewritten, so two different ids never map to one name (the three-digit
suffix has a fixed width, so `a` and `a-001` never meet either). NNN counts from 001 to 999; the 1000th call for one id is refused. Files are created exclusively,
so a file already on disk is never overwritten. A refusal or a failed write raises: a bench run with missing cost must not look complete. Nothing is written
through a symbolic link at <out> or <out>/cost (INV-22: the writer must not reach held-out custody)."""

import re
from collections.abc import Mapping
from pathlib import Path
from typing import Any, Protocol

COST_EVENT = "MODEL_CALL_RECORDED"  # the service's event name (noema_model.platform.telemetry.COST_EVENT); the service-side test pins that they agree
MAX_CALLS_PER_CORRELATION = 999
_FILE_NAME_LIMIT = 255
_NAME_ADDITION = len("-999.json")
MAX_CORRELATION_ID_LENGTH = _FILE_NAME_LIMIT - _NAME_ADDITION
_ALLOWED = re.compile(r"[A-Za-z0-9_-]+")


class Telemetry(Protocol):
    """The four methods of the service's telemetry, as the writer needs them."""

    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None: ...
    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None: ...
    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None: ...
    def span(self, name: str, correlation_id: str, attributes: Mapping[str, Any]) -> Any: ...


class CostFileRefused(Exception):
    """A cost file was not written: the correlation id is not exact, the id already has MAX_CALLS_PER_CORRELATION files, or the directory is a link."""


class CostFileTelemetry:
    def __init__(self, inner: Telemetry, out: Path) -> None:
        self._inner = inner
        self._out = out
        self._dir = out / "cost"

    def count(self, name: str, labels: Mapping[str, str], value: int = 1) -> None:
        self._inner.count(name, labels, value)

    def observe(self, name: str, labels: Mapping[str, str], value: int) -> None:
        self._inner.observe(name, labels, value)

    def event(self, name: str, correlation_id: str, fields: Mapping[str, str | int | bool]) -> None:
        self._inner.event(name, correlation_id, fields)
        if name == COST_EVENT:
            self._write(correlation_id, str(fields["record"]))

    def span(self, name: str, correlation_id: str, attributes: Mapping[str, Any]) -> Any:
        return self._inner.span(name, correlation_id, attributes)

    def _write(self, correlation_id: str, record: str) -> None:
        if not _ALLOWED.fullmatch(correlation_id) or len(correlation_id) > MAX_CORRELATION_ID_LENGTH:
            raise CostFileRefused(f"correlation id of {len(correlation_id)} characters is not 1 to {MAX_CORRELATION_ID_LENGTH} characters of [A-Za-z0-9_-]")
        if self._out.is_symlink():
            raise CostFileRefused("the bench output directory is a link")
        self._dir.mkdir(parents=True, exist_ok=True)
        # Only <out>/cost itself: a link there would carry the files anywhere, including into held-out custody (INV-22).
        if self._dir.is_symlink() or self._dir.resolve().parent != self._out.resolve():
            raise CostFileRefused("the cost directory is a link or lies outside the bench output directory")
        for sequence in range(1, MAX_CALLS_PER_CORRELATION + 1):
            try:
                with open(self._dir / f"{correlation_id}-{sequence:03d}.json", "x", encoding="utf-8") as handle:
                    handle.write(record)
                return
            except FileExistsError:
                continue
        raise CostFileRefused(f"correlation id already has {MAX_CALLS_PER_CORRELATION} cost files; the next call is refused")
