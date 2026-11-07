"""Model qualification records (UND-13; REQ-INT-006, DEC-2026-10-31 D25, AGENT-28; ruling A17 of 6 Nov 2026).

A model or speech provider version is used only when a qualification record exists for exactly that provider, model ID and pinned version. A record is made from the scorecard
of a run of the qualification subset that includes pass^k (exactly five runs, REQ-BEN-009) and from the drift run record (`run.py`). The format is
`contracts/schemas/qualification-record.v1.json`. This module is pure: it reads no file, runs nothing and sets no threshold.

Interim QUALIFIED (A17) means all of:

- the run is complete;
- every Tier 0 gate is at zero;
- the drift check result is PASS;
- pass^k is not below the bar. The bar is the higher of the pinned baseline's own pass^k and a Founder-set floor (pack data, set by a decision record). With neither there is no
  bar, and a first qualification waits for a Founder-set floor: the verdict is NOT_QUALIFIED.

The model service's configuration check (C2) refuses an ID with no QUALIFIED record by the same rules as `check_model`; it is not built here.

Generated layer only; the held-out set is private (OD-16, INV-22). SYNTHETIC in tests."""
from collections.abc import Mapping, Sequence
from typing import Any

from benchmark.contracts import sha256_of
from benchmark.qualification.pins import KINDS, is_pinned, parse_date
from benchmark.qualification.run import validate_run_record

Json = dict[str, Any]
SCHEMA = "qualification-record.v1"
VERDICTS = ("QUALIFIED", "NOT_QUALIFIED")
REQUIRED = ("schema", "kind", "provider", "model_id", "version", "run_id", "n_cases", "complete", "tier0_all_zero", "pass_k", "scorecard_sha256", "drift", "bar", "decision_record",
            "qualified_on", "verdict", "reasons")


def _reasons(complete: bool, tier0_all_zero: bool, pass_k: Mapping[str, Any], drift_result: str, bar: Mapping[str, Any]) -> list[str]:
    reasons = []
    if not complete:
        reasons.append("the run is not complete")
    if not tier0_all_zero:
        reasons.append("a Tier 0 gate is not at zero")
    if pass_k.get("available") is not True:
        reasons.append(f"pass^k was not available ({pass_k.get('reason') or 'no reason given'})")
    if drift_result != "PASS":
        reasons.append(f"the drift check result is {drift_result}, not PASS")
    bars = [b for b in (bar.get("baseline_pass_k"), bar.get("floor")) if b is not None]
    if not bars:
        reasons.append("there is no pass^k bar: a first qualification waits for a Founder-set floor (A17)")
    elif pass_k.get("available") is True:
        rate = (pass_k.get("rate") or {}).get("rate")
        if rate is None:
            reasons.append("pass^k has no rate to compare with the bar")
        elif rate < max(bars):
            reasons.append(f"pass^k {rate} is below the bar {max(bars)}")
    return reasons


def _fraction(name: str, value: float | None) -> None:
    if value is not None and not 0 <= value <= 1:
        raise ValueError(f"{name} must be between 0 and 1, not {value!r}")


def build_record(card: Mapping[str, Any], drift: Mapping[str, Any], *, kind: str, provider: str, model_id: str, version: str, decision_record: str, qualified_on: str,
                 baseline_pass_k: float | None, floor: float | None, floor_source: str | None) -> Json:
    """A record whose numbers are copied from the scorecard and the run record, so an author never types one."""
    if kind not in KINDS:
        raise ValueError(f"kind must be one of {', '.join(KINDS)}, not {kind!r}")
    for name, value in (("provider", provider), ("model_id", model_id), ("decision_record", decision_record)):
        if not value or value != value.strip():
            raise ValueError(f"{name} must be a non-empty string without surrounding space")
    if not is_pinned(version):
        raise ValueError(f"version {version!r} is not pinned: a qualification record names a dated or numbered version, never an alias such as latest")
    parse_date(qualified_on)
    _fraction("baseline_pass_k", baseline_pass_k)
    _fraction("floor", floor)
    if (floor is None) != (floor_source is None or not floor_source.strip()):
        raise ValueError("a floor and the decision record that set it (floor_source) come together")
    problems = validate_run_record(drift)
    if problems:
        raise ValueError(f"the drift run record is invalid: {'; '.join(problems)}")
    if drift["pinned"] != {"kind": kind, "provider": provider, "model_id": model_id, "version": version}:
        raise ValueError(f"the drift run record is for {drift['pinned']!r}, not for {provider}/{model_id}@{version} ({kind})")
    source = card["tier1"]["diagnostics"]["pass_k"]
    for flag, value in (("complete", card["complete"]), ("tier0_all_zero", card["tier0"]["all_zero"]), ("pass_k.available", source["available"])):
        if type(value) is not bool:
            raise ValueError(f"the scorecard's {flag} must be a real boolean, not {value!r}")
    rate = source.get("rate")
    pass_k = {"k_required": source["k_required"], "runs_supplied": source["runs_supplied"], "available": source["available"], "reason": source.get("reason"),
              "rate": None if rate is None else {"k": rate["k"], "n": rate["n"], "rate": rate["rate"]}, "failing_cases": list(source.get("failing_cases") or [])}
    bar = {"baseline_pass_k": baseline_pass_k, "floor": floor, "floor_source": floor_source}
    drift_block = {"result": drift["result"], "run_sha256": sha256_of(drift), "subset_id": drift["subset_id"], "cases_sha256": drift["cases_sha256"],
                   "baseline_version": drift["baseline"]["pinned"]["version"]}
    reasons = _reasons(card["complete"], card["tier0"]["all_zero"], pass_k, drift["result"], bar)
    return {
        "schema": SCHEMA, "kind": kind, "provider": provider, "model_id": model_id, "version": version, "run_id": card["run_id"], "n_cases": card["n_cases"],
        "complete": card["complete"], "tier0_all_zero": card["tier0"]["all_zero"], "pass_k": pass_k, "scorecard_sha256": sha256_of(card), "drift": drift_block,
        "bar": bar, "decision_record": decision_record, "qualified_on": qualified_on, "verdict": "NOT_QUALIFIED" if reasons else "QUALIFIED", "reasons": reasons,
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
        parse_date(record["qualified_on"])
    except ValueError:
        out.append(f"qualified_on {record['qualified_on']!r} is not a date written YYYY-MM-DD")
    blocks = {name: record[name] for name in ("pass_k", "drift", "bar") if isinstance(record[name], Mapping)}
    out += [f"{name} must be an object" for name in ("pass_k", "drift", "bar") if name not in blocks]
    flags_ok = True
    for flag in ("complete", "tier0_all_zero"):
        if type(record[flag]) is not bool:
            out.append(f"{flag} must be a boolean, not {record[flag]!r}")
            flags_ok = False
    if "pass_k" in blocks and type(blocks["pass_k"].get("available")) is not bool:
        out.append(f"pass_k.available must be a boolean, not {blocks['pass_k'].get('available')!r}")
        flags_ok = False
    if record["verdict"] not in VERDICTS:
        out.append(f"verdict must be one of {', '.join(VERDICTS)}, not {record['verdict']!r}")
    elif record["verdict"] == "QUALIFIED" and len(blocks) == 3 and flags_ok:
        out += [f"verdict is QUALIFIED but {reason}" for reason in _reasons(record["complete"], record["tier0_all_zero"], blocks["pass_k"], str(blocks["drift"].get("result")), blocks["bar"])]
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
    bad = [p for r in matching for p in validate_record(r)]
    if bad:
        return [f"{who}: an invalid qualification record exists for this identity, so the lookup refuses (fail closed): {'; '.join(bad)}"]
    latest = max(parse_date(r["qualified_on"]) for r in matching)
    newest = [r for r in matching if parse_date(r["qualified_on"]) == latest]
    verdicts = {r["verdict"] for r in newest}
    if len(verdicts) > 1:
        return [f"{who}: records of {latest.isoformat()} disagree (QUALIFIED and NOT_QUALIFIED); re-run the qualification"]
    if verdicts == {"NOT_QUALIFIED"}:
        reasons = sorted({reason for r in newest for reason in r["reasons"]})
        return [f"{who}: the latest record ({latest.isoformat()}) says NOT_QUALIFIED ({'; '.join(reasons) or 'no reason recorded'})"]
    return []
