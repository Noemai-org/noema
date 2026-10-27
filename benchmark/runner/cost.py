"""Provider cost for one case, read from the model service's bench cost files (REQ-NFR-007; noema#62; the founders' route for cost).

During a bench run the model service writes each model call's record, the JSON of `CallRecord.to_dict()`, to `$BENCH_OUT/cost/<correlation_id>-<NNN>.json`
(services/model/noema_model/platform/telemetry/__init__.py). The name is exact: the producer refuses a correlation id with a character outside [A-Za-z0-9_-] (it does not
rewrite it), refuses an empty or over-long one (never cuts it), counts NNN from 001 to 999, and refuses the 1000th call for one id; files are created exclusively. This reader
applies the same rules, so two different ids never read one file. The runner reads that directory. It does not import services/** and it adds no endpoint. A case's cost is the sum of the `cost.total_micro` of its
calls, in millionths of a US dollar (`cost_usd_micros`).

A cost that is not known is null ("not available"), never zero and never an estimate. It is null when: no directory is configured or it does not exist; the correlation id is not
a valid file name (a character outside [A-Za-z0-9_-], empty, or longer than MAX_CORRELATION_ID_LENGTH); the case has no file; a file's sequence is not three digits from 001 to 999; any file is unreadable, not JSON, or not shaped as a record; any record says `cost.complete` is not true; any `total_micro` is not a non-negative integer (a float,
a string, a boolean and null are all refused); or any record's currency is not USD (there is no rate to convert and none is invented). One bad file makes the case null: a
partial sum would mislead."""
import json
import os
import re
from pathlib import Path
from typing import Any

MAX_CALLS_PER_CORRELATION = 999
MAX_CORRELATION_ID_LENGTH = 255 - len("-NNN.json")  # the file system's name limit less the suffix; an id is refused beyond it, never cut
_ALLOWED = re.compile(r"[A-Za-z0-9_-]+")


def _refuse(value: str) -> Any:
    raise ValueError(f"not an integer: {value}")


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
        if not _ALLOWED.fullmatch(correlation_id) or len(correlation_id) > MAX_CORRELATION_ID_LENGTH:
            return None
        pattern = re.compile(re.escape(correlation_id) + r"-(\d+)\.json")
        try:
            names = sorted(e.name for e in os.scandir(self.directory) if pattern.fullmatch(e.name) and e.is_file(follow_symlinks=False))
        except OSError:
            return None
        if not names:
            return None
        for name in names:
            digits = pattern.fullmatch(name).group(1)  # type: ignore[union-attr]
            if len(digits) != 3 or not 1 <= int(digits) <= MAX_CALLS_PER_CORRELATION:
                return None
        parts = [_micro(self.directory / name) for name in names]
        return None if any(p is None for p in parts) else sum(p for p in parts if p is not None)
