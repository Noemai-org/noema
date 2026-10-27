"""Provider cost for one case, read from the model service's bench cost files (REQ-NFR-007; noema#62; the founders' route for cost).

During a bench run the model service writes each model call's record, the JSON of `CallRecord.to_dict()`, to `$BENCH_OUT/cost/<correlation_id>-<NNN>.json`
(services/model/noema_model/platform/telemetry/__init__.py: the correlation id's characters outside A-Z a-z 0-9 _ - become `_`, cut to 128, `call` when empty;
NNN is a sequence). The runner reads that directory. It does not import services/** and it adds no endpoint. A case's cost is the sum of the `cost.total_micro` of its
calls, in millionths of a US dollar (`cost_usd_micros`).

A cost that is not known is null ("not available"), never zero and never an estimate. It is null when: no directory is configured or it does not exist; the case has
no file; any file is unreadable, not JSON, or not shaped as a record; any record says `cost.complete` is not true; any `total_micro` is not a non-negative integer (a float,
a string, a boolean and null are all refused); or any record's currency is not USD (there is no rate to convert and none is invented). One bad file makes the case null: a
partial sum would mislead."""
import json
import os
import re
from pathlib import Path
from typing import Any

_UNSAFE = re.compile(r"[^A-Za-z0-9_-]")


def _refuse(value: str) -> Any:
    raise ValueError(f"not an integer: {value}")


def stem_of(correlation_id: str) -> str:
    """The writer's file-name stem for a correlation id."""
    return _UNSAFE.sub("_", correlation_id)[:128] or "call"


def _micro(path: Path) -> int | None:
    try:
        record = json.loads(path.read_text(encoding="utf-8"), parse_float=_refuse, parse_constant=_refuse)
    except (OSError, ValueError):
        return None
    cost = record.get("cost") if isinstance(record, dict) else None
    if not isinstance(cost, dict) or cost.get("complete") is not True or cost.get("currency") != "USD":
        return None
    total = cost.get("total_micro")
    if isinstance(total, bool) or not isinstance(total, int) or total < 0:
        return None
    return total


class CostFiles:
    """The cost directory of a run. `CostFiles(None)` is a run with no cost source: every case is not available."""

    def __init__(self, directory: Path | None) -> None:
        self.directory = Path(directory) if directory is not None else None

    def cost_usd_micros(self, correlation_id: str) -> int | None:
        if self.directory is None:
            return None
        pattern = re.compile(re.escape(stem_of(correlation_id)) + r"-\d{3,}\.json")
        try:
            names = sorted(e.name for e in os.scandir(self.directory) if pattern.fullmatch(e.name) and e.is_file(follow_symlinks=False))
        except OSError:
            return None
        if not names:
            return None
        parts = [_micro(self.directory / name) for name in names]
        return None if any(p is None for p in parts) else sum(p for p in parts if p is not None)
