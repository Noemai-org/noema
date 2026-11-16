"""Keep the evidence of an earlier run when a new run writes to the same place (BEN-29, REQ-TST-005; Codex on noema#117).

A suite writes `selection.txt`, `suite-run.json`, results, a manifest, a scorecard and a report into one output directory. Running again with a new run ID into the same directory
overwrote the run before it, and a later NOT_RUN could leave an older score or replay report beside the new record. Before a run writes, anything already there is moved, whole,
to a sibling named by the run ID it recorded, so a failure stays reproducible and no stale file can sit beside a new record. Nothing is ever deleted.

Generated layer only (INV-22)."""
import datetime
import json
import re
from pathlib import Path

_UNSAFE = re.compile(r"[^A-Za-z0-9_.-]")


def _recorded_run_id(out: Path) -> str:
    try:
        loaded = json.loads((out / "suite-run.json").read_text(encoding="utf-8"))
    except (OSError, ValueError):
        loaded = None
    run_id = loaded.get("run_id") if isinstance(loaded, dict) else None
    if isinstance(run_id, str) and run_id:
        return _UNSAFE.sub("_", run_id)[:64].strip(".") or "unrecorded"
    return "unrecorded-" + datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def archive_previous(out: Path) -> Path | None:
    """Move a non-empty `out` to `out.<run id>` beside it and return where it went; None if there was nothing to keep. A name already taken gets a numeric suffix."""
    if not out.is_dir() or not any(out.iterdir()):
        return None
    base = out.parent / f"{out.name}.{_recorded_run_id(out)}"
    target, n = base, 1
    while target.exists():
        n += 1
        target = Path(f"{base}.{n}")
    out.rename(target)
    return target
