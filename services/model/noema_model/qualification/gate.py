"""The configuration gate: a model ID is used only when a QUALIFIED record matches its provider, model ID and pinned version (REQ-INT-006, D25, AGENT-28).

Rules, as the benchmark module states them (`record.check_model` is the reference; `tests/test_qualification_gate.py` runs every case it gave):

- an unpinned version (an alias such as `latest`) is refused before any lookup;
- only a valid QUALIFIED record for exactly this provider, model ID and version allows; records that fail validation are ignored, and if none is valid the ID is refused;
- the latest record by `qualified_on` decides: a later NOT_QUALIFIED revokes; two records of one day that disagree refuse;
- a record file that cannot be read is an error for the whole directory, never a skipped file: a missing record must not look like a qualified model (fail closed).
"""

import datetime
import json
import re
from collections.abc import Iterable, Mapping, Sequence
from pathlib import Path
from typing import Any

from noema_model.harness.config import check_pinned
from noema_model.platform.config import ConfigError

SCHEMA = "qualification-record.v1"
KINDS = ("llm", "speech")
VERDICTS = ("QUALIFIED", "NOT_QUALIFIED")
REQUIRED = ("schema", "kind", "provider", "model_id", "version", "run_id", "n_cases", "complete", "tier0_all_zero", "pass_k", "scorecard_sha256", "decision_record",
            "qualified_on", "verdict", "reasons")

Record = Mapping[str, Any]


class QualificationUnreadable(Exception):
    """The qualification records could not be read. The service must not start on a guess."""


class ModelNotQualified(Exception):
    """One or more configured models are not qualified. `refusals` has one line for each."""

    def __init__(self, refusals: Sequence[str]) -> None:
        super().__init__("model not qualified: " + " | ".join(refusals))
        self.refusals = tuple(refusals)


def is_pinned(version: Any) -> bool:
    """The harness's own pin rule (`harness.config.check_pinned`): one snapshot, never a floating alias. A second rule here would let a version pass the gate that the harness refuses."""
    if not isinstance(version, str):
        return False
    try:
        check_pinned(version)
    except ConfigError:
        return False
    return True


def _reasons(complete: bool, tier0_all_zero: bool, pass_k: Mapping[str, Any]) -> list[str]:
    reasons = []
    if not complete:
        reasons.append("the run is not complete")
    if not tier0_all_zero:
        reasons.append("a Tier 0 gate is not at zero")
    if not pass_k.get("available"):
        reasons.append(f"pass^k was not available ({pass_k.get('reason') or 'no reason given'})")
    return reasons


_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
_DATE = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}$")


def _valid_date(value: Any) -> bool:
    """The one canonical spelling, YYYY-MM-DD, that is also a real calendar day: other ISO spellings of a day (2026W455) are refused so two records of one day compare equal."""
    if not isinstance(value, str) or not _DATE.match(value):
        return False
    try:
        datetime.date.fromisoformat(value)
    except ValueError:
        return False
    return True


def validate_record(record: Record) -> list[str]:
    """Every problem with a record's shape, and with a QUALIFIED verdict its own fields do not support."""
    missing = [f for f in REQUIRED if f not in record]
    if missing:
        return [f"record is missing {', '.join(missing)}"]
    out: list[str] = []
    if record["schema"] != SCHEMA:
        out.append(f"schema must be {SCHEMA}, not {record['schema']!r}")
    if record["kind"] not in KINDS:
        out.append(f"kind must be one of {', '.join(KINDS)}, not {record['kind']!r}")
    for name in ("provider", "model_id", "decision_record"):
        value = record[name]
        if not isinstance(value, str) or not value.strip():
            out.append(f"{name} must be a non-empty string")
    if not is_pinned(record["version"]):
        out.append(f"version {record['version']!r} is not pinned")
    if not _valid_date(record["qualified_on"]):
        out.append(f"qualified_on {record['qualified_on']!r} is not an ISO date")
    if not isinstance(record["scorecard_sha256"], str) or not _DIGEST.match(record["scorecard_sha256"]):
        out.append("scorecard_sha256 must be sha256: and 64 lowercase hex characters")
    if not isinstance(record["run_id"], str) or not record["run_id"].strip():
        out.append("run_id must be a non-empty string")
    if isinstance(record["n_cases"], bool) or not isinstance(record["n_cases"], int) or record["n_cases"] < 1:
        out.append("n_cases must be a positive integer")
    if not isinstance(record["pass_k"], Mapping):
        out.append("pass_k must be an object")
    if not isinstance(record["reasons"], list):
        out.append("reasons must be a list")
    if record["verdict"] not in VERDICTS:
        out.append(f"verdict must be one of {', '.join(VERDICTS)}, not {record['verdict']!r}")
    elif record["verdict"] == "QUALIFIED" and isinstance(record["pass_k"], Mapping):
        flags = (record["complete"], record["tier0_all_zero"], record["pass_k"].get("available"))
        if not all(isinstance(f, bool) for f in flags):
            out.append("complete, tier0_all_zero and pass_k.available must be JSON booleans")
        else:
            out += [f"verdict is QUALIFIED but {r}" for r in _reasons(record["complete"], record["tier0_all_zero"], record["pass_k"])]
    elif record["verdict"] == "QUALIFIED":
        out.append("verdict is QUALIFIED but pass^k is not recorded")
    return out


def check_model(records: Sequence[Record], provider: str, model_id: str, version: str, kind: str | None = None) -> list[str]:
    """Why this provider, model ID and version may not be used; an empty list means it may."""
    who = f"{provider}/{model_id}@{version}" + (f" ({kind})" if kind else "")
    if not is_pinned(version):
        return [f"{who}: the version is not pinned (a dated or numbered version, never an alias such as latest)"]
    matching = [r for r in records if r.get("provider") == provider and r.get("model_id") == model_id and r.get("version") == version and (kind is None or r.get("kind") == kind)]
    if not matching:
        return [f"{who}: no qualification record"]
    problems = [validate_record(r) for r in matching]
    valid = [r for r, p in zip(matching, problems, strict=True) if not p]
    if not valid:
        return [f"{who}: no qualification record is valid ({'; '.join(p for ps in problems for p in ps)})"]
    latest = max(datetime.date.fromisoformat(r["qualified_on"]) for r in valid).isoformat()
    newest = [r for r in valid if r["qualified_on"] == latest]
    verdicts = {r["verdict"] for r in newest}
    if len(verdicts) > 1:
        return [f"{who}: records of {latest} disagree (QUALIFIED and NOT_QUALIFIED); re-run the qualification"]
    if verdicts == {"NOT_QUALIFIED"}:
        reasons = sorted({reason for r in newest for reason in r["reasons"]})
        return [f"{who}: the latest record ({latest}) says NOT_QUALIFIED ({'; '.join(reasons) or 'no reason recorded'})"]
    return []


def load_records(directory: Path) -> list[Record]:
    """Every `*.json` record in the directory. A missing directory, an unreadable file or a file that is not a JSON object raises."""
    if not directory.is_dir():
        raise QualificationUnreadable(f"the qualification records directory {directory} does not exist")
    records: list[Record] = []
    for path in sorted(directory.glob("*.json")):
        try:
            document = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as err:
            raise QualificationUnreadable(f"{path.name} cannot be read as JSON") from err
        if not isinstance(document, dict):
            raise QualificationUnreadable(f"{path.name} is not a JSON object")
        records.append(document)
    return records


def require_qualified(wanted: Iterable[tuple[str, str, str, str]], directory: Path) -> None:
    """Raise ModelNotQualified naming every (kind, provider, model_id, version) that may not be used; return when all may."""
    records = load_records(directory)
    refusals: list[str] = []
    for kind, provider, model_id, version in wanted:
        refusals += check_model(records, provider, model_id, version, kind)
    if refusals:
        raise ModelNotQualified(refusals)
