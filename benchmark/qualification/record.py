"""Model qualification records (UND-13; REQ-INT-006, DEC-2026-10-31 D25, AGENT-28).

A model or speech provider version is used only when a qualification record exists for exactly that provider, model ID and pinned version. A record is made from the
scorecard of a run of the qualification subset that includes pass^k (exactly five runs, REQ-BEN-009). This module is pure: it reads no file, runs nothing and decides no
threshold. It records what the scorecard says and gives a verdict only on the conditions that need no judgement:

- the run is complete;
- every Tier 0 gate is at zero;
- pass^k was available (exactly five runs supplied).

How good the numbers must be beyond that is a founder decision (the record's `decision_record` names it). The model service's configuration check (C2) refuses an ID with
no QUALIFIED record by the same rules as `check_model`; it is not built here.

Generated layer only; the held-out set is private (OD-16, INV-22). SYNTHETIC in tests."""
import datetime
from collections.abc import Mapping, Sequence
from typing import Any

from benchmark.contracts import sha256_of

Json = dict[str, Any]
SCHEMA = "qualification-record.v1"
KINDS = ("llm", "speech")
VERDICTS = ("QUALIFIED", "NOT_QUALIFIED")
REQUIRED = ("schema", "kind", "provider", "model_id", "version", "run_id", "n_cases", "complete", "tier0_all_zero", "pass_k", "scorecard_sha256", "decision_record",
            "qualified_on", "verdict", "reasons")
ALIASES = frozenset({"", "latest", "default", "current", "stable", "auto", "*"})


def is_pinned(version: Any) -> bool:
    """A pinned version is a dated or numbered identifier, not an alias that the provider can move."""
    if not isinstance(version, str) or version != version.strip() or any(ch.isspace() for ch in version):
        return False
    lowered = version.lower()
    return lowered not in ALIASES and "latest" not in lowered


def _reasons(complete: bool, tier0_all_zero: bool, pass_k: Mapping[str, Any]) -> list[str]:
    reasons = []
    if not complete:
        reasons.append("the run is not complete")
    if not tier0_all_zero:
        reasons.append("a Tier 0 gate is not at zero")
    if not pass_k.get("available"):
        reasons.append(f"pass^k was not available ({pass_k.get('reason') or 'no reason given'})")
    return reasons


def _parse_date(value: Any) -> datetime.date:
    if not isinstance(value, str):
        raise ValueError(f"qualified_on must be an ISO date, not {value!r}")
    return datetime.date.fromisoformat(value)


def build_record(card: Mapping[str, Any], *, kind: str, provider: str, model_id: str, version: str, decision_record: str, qualified_on: str) -> Json:
    """A record whose numbers are copied from the scorecard, so an author never types one."""
    if kind not in KINDS:
        raise ValueError(f"kind must be one of {', '.join(KINDS)}, not {kind!r}")
    for name, value in (("provider", provider), ("model_id", model_id), ("decision_record", decision_record)):
        if not value or value != value.strip():
            raise ValueError(f"{name} must be a non-empty string without surrounding space")
    if not is_pinned(version):
        raise ValueError(f"version {version!r} is not pinned: a qualification record names a dated or numbered version, never an alias such as latest")
    _parse_date(qualified_on)
    pass_k = dict(card["tier1"]["diagnostics"]["pass_k"])
    reasons = _reasons(bool(card["complete"]), bool(card["tier0"]["all_zero"]), pass_k)
    return {
        "schema": SCHEMA, "kind": kind, "provider": provider, "model_id": model_id, "version": version, "run_id": card["run_id"], "n_cases": card["n_cases"],
        "complete": bool(card["complete"]), "tier0_all_zero": bool(card["tier0"]["all_zero"]), "pass_k": pass_k, "scorecard_sha256": sha256_of(card),
        "decision_record": decision_record, "qualified_on": qualified_on, "verdict": "NOT_QUALIFIED" if reasons else "QUALIFIED", "reasons": reasons,
    }


def validate_record(record: Mapping[str, Any]) -> list[str]:
    """Every problem with a record's shape and with a QUALIFIED verdict its own fields do not support."""
    missing = [f for f in REQUIRED if f not in record]
    if missing:
        return [f"record is missing {', '.join(missing)}"]
    out: list[str] = []
    if record["schema"] != SCHEMA:
        out.append(f"schema must be {SCHEMA}, not {record['schema']!r}")
    if record["kind"] not in KINDS:
        out.append(f"kind must be one of {', '.join(KINDS)}, not {record['kind']!r}")
    for name in ("provider", "model_id", "decision_record"):
        if not isinstance(record[name], str) or not record[name].strip():
            out.append(f"{name} must be a non-empty string")
    if not is_pinned(record["version"]):
        out.append(f"version {record['version']!r} is not pinned")
    try:
        _parse_date(record["qualified_on"])
    except ValueError:
        out.append(f"qualified_on {record['qualified_on']!r} is not an ISO date")
    if record["verdict"] not in VERDICTS:
        out.append(f"verdict must be one of {', '.join(VERDICTS)}, not {record['verdict']!r}")
    elif record["verdict"] == "QUALIFIED" and isinstance(record["pass_k"], Mapping):
        out += [f"verdict is QUALIFIED but {reason}" for reason in _reasons(bool(record["complete"]), bool(record["tier0_all_zero"]), record["pass_k"])]
    elif record["verdict"] == "QUALIFIED":
        out.append("verdict is QUALIFIED but pass^k is not recorded")
    return out


def verify_against_scorecard(record: Mapping[str, Any], card: Mapping[str, Any]) -> list[str]:
    """The record still says what the scorecard it was made from says."""
    out = []
    if record.get("scorecard_sha256") != sha256_of(card):
        out.append("the scorecard digest does not match the record's scorecard_sha256 (the scorecard or the record was edited)")
    if record.get("run_id") != card.get("run_id"):
        out.append(f"run_id {record.get('run_id')!r} is not the scorecard's {card.get('run_id')!r}")
    return out


def check_model(records: Sequence[Mapping[str, Any]], provider: str, model_id: str, version: str, kind: str | None = None) -> list[str]:
    """Why this provider, model ID and version may not be used; an empty list means it may.

    Only a valid QUALIFIED record for exactly this provider, model ID and version allows. The latest record decides: a later failed re-qualification revokes an earlier
    qualification, and two records of one day that disagree refuse."""
    who = f"{provider}/{model_id}@{version}" + (f" ({kind})" if kind else "")
    if not is_pinned(version):
        return [f"{who}: the version is not pinned (a dated or numbered version, never an alias such as latest)"]
    matching = [r for r in records if r.get("provider") == provider and r.get("model_id") == model_id and r.get("version") == version and (kind is None or r.get("kind") == kind)]
    if not matching:
        return [f"{who}: no qualification record"]
    problems = {i: validate_record(r) for i, r in enumerate(matching)}
    valid = [r for i, r in enumerate(matching) if not problems[i]]
    if not valid:
        return [f"{who}: no qualification record is valid ({'; '.join(p for ps in problems.values() for p in ps)})"]
    latest = max(r["qualified_on"] for r in valid)
    verdicts = {r["verdict"] for r in valid if r["qualified_on"] == latest}
    if len(verdicts) > 1:
        return [f"{who}: records of {latest} disagree (QUALIFIED and NOT_QUALIFIED); re-run the qualification"]
    if verdicts == {"NOT_QUALIFIED"}:
        reasons = sorted({reason for r in valid if r["qualified_on"] == latest for reason in r["reasons"]})
        return [f"{who}: the latest record ({latest}) says NOT_QUALIFIED ({'; '.join(reasons) or 'no reason recorded'})"]
    return []
