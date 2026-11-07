"""The qualification subset, the pinned baseline and the run record of the model-drift canary (UND-13; REQ-INT-006, DEC-2026-10-31 D25, AGENT-28).

Three documents, all JSON, all pure functions of their inputs (no file, no network, no model call):

- SUBSET (`qualification-subset.v1`): the GENERATED cases that depend on understanding, sorted, with their digest. Held-out and partner cases are refused outright (INV-22).
- BASELINE (`qualification-baseline.v1`): the structured output (slots) the previously qualified, pinned version gave for each subset case, with the commit SHA it was taken at.
  It is a recorded answer of a model, never the oracle's expectation: the oracle is not read here.
- RUN RECORD (`qualification-run.v1`): what the canary in the model service emits. It compares a new version's answers to the baseline, case by case, names the differing slots
  and the version that actually answered, and ends in PASS, DRIFT or PROVIDER_UNAVAILABLE. The JSON Schema is `schemas/qualification-run.v1.schema.json`.

An answer is `{"slots": {...}, "answered_by": "<version the provider reports>"}`, or null when the provider did not answer. A version that answers under another version than the
one asked for is drift even when its slots match: an alias that moved is exactly what the canary exists to see. DRIFT outranks PROVIDER_UNAVAILABLE.

Generated layer only; the held-out set is private (OD-16, INV-22). SYNTHETIC in tests."""
import re
from collections.abc import Mapping
from typing import Any

from benchmark.contracts import sha256_of
from benchmark.qualification.pins import KINDS, is_pinned, parse_date
from benchmark.scoring.diagnostics import understanding_dependent

Json = dict[str, Any]
SUBSET_SCHEMA = "qualification-subset.v1"
BASELINE_SCHEMA = "qualification-baseline.v1"
RUN_SCHEMA = "qualification-run.v1"
STATUSES = ("MATCH", "DRIFT", "UNAVAILABLE")
RESULTS = ("PASS", "DRIFT", "PROVIDER_UNAVAILABLE")
REQUIRED_RUN = ("schema", "subset_id", "layer", "n_cases", "cases_sha256", "pinned", "baseline", "commit_sha", "recorded_on", "answered_versions", "result", "cases")
PINNED_FIELDS = ("kind", "provider", "model_id", "version")
_SHA = re.compile(r"^[0-9a-f]{40}$")


def select_subset(cases: list[Json], *, subset_id: str) -> Json:
    """The GENERATED, understanding-dependent cases, sorted. Any case of another layer anywhere in the input is an error, not something to skip."""
    if not subset_id or subset_id != subset_id.strip():
        raise ValueError("subset_id must be a non-empty string without surrounding space")
    for case in cases:
        if case.get("suite_layer") != "GENERATED":
            raise ValueError(f"case {case.get('case_id')} is in layer {case.get('suite_layer')}; held-out and partner cases never reach qualification (INV-22)")
    chosen = sorted((c for c in cases if understanding_dependent(c)), key=lambda c: str(c["case_id"]))
    if not chosen:
        raise ValueError("no understanding-dependent GENERATED case: a qualification subset cannot be empty")
    return {"schema": SUBSET_SCHEMA, "subset_id": subset_id, "layer": "GENERATED", "n_cases": len(chosen), "case_ids": [c["case_id"] for c in chosen],
            "cases_sha256": sha256_of(chosen), "cases": chosen}


def _pinned(pinned: Mapping[str, Any]) -> Json:
    if pinned.get("kind") not in KINDS:
        raise ValueError(f"kind must be one of {', '.join(KINDS)}, not {pinned.get('kind')!r}")
    for name in ("provider", "model_id"):
        value = pinned.get(name)
        if not isinstance(value, str) or not value or value != value.strip():
            raise ValueError(f"{name} must be a non-empty string without surrounding space")
    if not is_pinned(pinned.get("version")):
        raise ValueError(f"version {pinned.get('version')!r} is not pinned: a dated or numbered version, never an alias such as latest")
    return {name: pinned[name] for name in PINNED_FIELDS}


def _sha(commit_sha: str) -> str:
    if not _SHA.fullmatch(commit_sha):
        raise ValueError(f"commit_sha must be a 40-character lowercase hex SHA, not {commit_sha!r}")
    return commit_sha


def _answers(subset: Mapping[str, Any], answers: Mapping[str, Json | None]) -> None:
    wanted, got = set(subset["case_ids"]), set(answers)
    if wanted != got:
        raise ValueError(f"answers must cover exactly the subset: missing {sorted(wanted - got)}, outside the subset {sorted(got - wanted)}")
    for case_id, answer in answers.items():
        if answer is not None and not (isinstance(answer.get("slots"), dict) and isinstance(answer.get("answered_by"), str)):
            raise ValueError(f"the answer for {case_id} must be null or have slots (an object) and answered_by (a string)")


def build_baseline(subset: Mapping[str, Any], pinned: Mapping[str, Any], answers: Mapping[str, Json | None], *, commit_sha: str) -> Json:
    """The pinned expectations: what the qualified version answered. Every case answered, by exactly the pinned version."""
    pin = _pinned(pinned)
    _answers(subset, answers)
    cases: Json = {}
    for case_id in subset["case_ids"]:
        answer = answers[case_id]
        if answer is None:
            raise ValueError(f"the baseline needs an answer for {case_id}; an unavailable provider is not a baseline")
        if answer["answered_by"] != pin["version"]:
            raise ValueError(f"{case_id} was answered by {answer['answered_by']!r}, not by the pinned version {pin['version']!r}")
        cases[case_id] = {"slots": answer["slots"], "slots_sha256": sha256_of(answer["slots"])}
    return {"schema": BASELINE_SCHEMA, "subset_id": subset["subset_id"], "cases_sha256": subset["cases_sha256"], "pinned": pin, "commit_sha": _sha(commit_sha), "cases": cases}


def _same(a: Any, b: Any) -> bool:
    """JSON equality by type: a boolean is never a number (in Python True == 1), None is never 0, and a string is never a number. Numbers compare as one JSON number."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool) and a == b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return bool(a == b)
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(_same(a[k], b[k]) for k in a)
    if isinstance(a, (list, tuple)) and isinstance(b, (list, tuple)):
        return len(a) == len(b) and all(_same(x, y) for x, y in zip(a, b))
    return type(a) is type(b) and bool(a == b)


def _differing(expected: Mapping[str, Any], observed: Mapping[str, Any]) -> list[Json]:
    out = []
    for slot in sorted(set(expected) | set(observed)):
        if slot in expected and slot in observed and _same(expected[slot], observed[slot]):
            continue
        diff: Json = {"slot": slot}
        if slot in expected:
            diff["expected"] = expected[slot]
        if slot in observed:
            diff["observed"] = observed[slot]
        out.append(diff)
    return out


def _result(statuses: list[str]) -> str:
    if "DRIFT" in statuses:
        return "DRIFT"
    return "PROVIDER_UNAVAILABLE" if "UNAVAILABLE" in statuses else "PASS"


def compare(subset: Mapping[str, Any], baseline: Mapping[str, Any], pinned: Mapping[str, Any], answers: Mapping[str, Json | None], *, commit_sha: str, recorded_on: str) -> Json:
    """The run record: a new pinned version's answers against the baseline, case by case."""
    pin = _pinned(pinned)
    if baseline["subset_id"] != subset["subset_id"] or baseline["cases_sha256"] != subset["cases_sha256"]:
        raise ValueError("the baseline was taken on a different subset than this one (subset_id or cases_sha256 differ)")
    base_pin = baseline["pinned"]
    if any(base_pin[name] != pin[name] for name in ("kind", "provider", "model_id")):
        raise ValueError(f"the baseline is for {base_pin['provider']}/{base_pin['model_id']} ({base_pin['kind']}), not {pin['provider']}/{pin['model_id']} ({pin['kind']})")
    _answers(subset, answers)
    parse_date(recorded_on)
    rows = []
    for case_id in subset["case_ids"]:
        expected = baseline["cases"][case_id]
        answer = answers[case_id]
        if answer is None:
            rows.append({"case_id": case_id, "status": "UNAVAILABLE", "answered_by": None, "version_mismatch": False, "differing_slots": [],
                         "expected_sha256": expected["slots_sha256"], "observed_sha256": None})
            continue
        diffs = _differing(expected["slots"], answer["slots"])
        mismatch = answer["answered_by"] != pin["version"]
        rows.append({"case_id": case_id, "status": "DRIFT" if diffs or mismatch else "MATCH", "answered_by": answer["answered_by"], "version_mismatch": mismatch,
                     "differing_slots": diffs, "expected_sha256": expected["slots_sha256"], "observed_sha256": sha256_of(answer["slots"])})
    return {"schema": RUN_SCHEMA, "subset_id": subset["subset_id"], "layer": "GENERATED", "n_cases": subset["n_cases"], "cases_sha256": subset["cases_sha256"], "pinned": pin,
            "baseline": {"pinned": dict(base_pin), "commit_sha": baseline["commit_sha"], "sha256": sha256_of(baseline)}, "commit_sha": _sha(commit_sha), "recorded_on": recorded_on,
            "answered_versions": sorted({r["answered_by"] for r in rows if r["answered_by"] is not None}), "result": _result([r["status"] for r in rows]), "cases": rows}


def validate_run_record(record: Mapping[str, Any]) -> list[str]:
    """Every problem with a run record's shape, and with a result its own cases do not support."""
    missing = [f for f in REQUIRED_RUN if f not in record]
    if missing:
        return [f"run record is missing {', '.join(missing)}"]
    out: list[str] = []
    if record["schema"] != RUN_SCHEMA:
        out.append(f"schema must be {RUN_SCHEMA}, not {record['schema']!r}")
    if record["layer"] != "GENERATED":
        out.append(f"layer must be GENERATED, not {record['layer']!r}")
    pin = record["pinned"] if isinstance(record["pinned"], Mapping) else {}
    if pin.get("kind") not in KINDS or not pin.get("provider") or not pin.get("model_id"):
        out.append("pinned needs a kind (llm or speech), a provider and a model_id")
    if not is_pinned(pin.get("version")):
        out.append(f"pinned.version {pin.get('version')!r} is not pinned")
    if not isinstance(record["commit_sha"], str) or not _SHA.fullmatch(record["commit_sha"]):
        out.append("commit_sha must be a 40-character lowercase hex SHA")
    try:
        parse_date(record["recorded_on"])
    except ValueError:
        out.append(f"recorded_on {record['recorded_on']!r} is not a date written YYYY-MM-DD")
    if record["result"] not in RESULTS:
        out.append(f"result must be one of {', '.join(RESULTS)}, not {record['result']!r}")
    cases = record["cases"] if isinstance(record["cases"], list) else []
    if record["n_cases"] != len(cases):
        out.append(f"n_cases is {record['n_cases']!r} but there are {len(cases)} cases")
    statuses = []
    for case in cases:
        status, case_id = case.get("status"), case.get("case_id")
        if status not in STATUSES:
            out.append(f"case {case_id}: status {status!r} is not one of {', '.join(STATUSES)}")
            continue
        statuses.append(status)
        if type(case.get("version_mismatch")) is not bool:
            out.append(f"case {case_id}: version_mismatch must be a boolean, not {case.get('version_mismatch')!r}")
            statuses.pop()
            continue
        drifted = bool(case.get("differing_slots")) or case["version_mismatch"]
        if (status == "DRIFT") != drifted and status != "UNAVAILABLE":
            out.append(f"case {case_id}: status {status} does not agree with its differing_slots and version_mismatch")
    if record["result"] in RESULTS and len(statuses) == len(cases) and record["result"] != _result(statuses):
        out.append(f"result is {record['result']} but the cases support {_result(statuses)}")
    seen = sorted({c.get("answered_by") for c in cases if c.get("answered_by") is not None})
    if record["answered_versions"] != seen:
        out.append(f"answered_versions is {record['answered_versions']!r} but the cases were answered by {seen!r}")
    return out
