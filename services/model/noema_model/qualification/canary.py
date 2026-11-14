"""The model-drift canary (REQ-INT-006, D25, AGENT-28): the qualification subset through an `Asker` (the harness's `ProviderAsker` wraps the service's own provider adapters), compared with the pinned baseline.

It emits the run record the benchmark module defines (`qualification-run.v1`): per case MATCH, DRIFT (with the differing slots) or UNAVAILABLE, the version that answered, and a
result of PASS, DRIFT or PROVIDER_UNAVAILABLE (DRIFT outranks UNAVAILABLE). Whether a drift is acceptable and whether the version is then qualified is not decided here: that is
the qualification record's decision record (a founder decision). A PASS is not a qualification.

Held-out material never reaches this service (INV-22): a subset or case outside the GENERATED layer is refused before any provider is called. The baseline is a recorded model
answer, never the oracle's expectation. No timeout is chosen here (AGENT-16): the caller passes the deadline from configuration.
"""

import asyncio
import hashlib
import json
import math
import re
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any, Protocol

from noema_model.qualification.gate import KINDS, is_pinned

RUN_SCHEMA = "qualification-run.v1"
LAYER = "GENERATED"
_SHA = re.compile(r"^[0-9a-f]{40}$")

Json = dict[str, Any]


@dataclass(frozen=True, slots=True)
class CaseAnswer:
    """A usable structured answer: the slots, and the version the provider reported answering with."""

    slots: Mapping[str, Any]
    answered_by: str


class AskerUnavailable(Exception):
    """The provider gave no answer: a failed call, a timeout, a refusal. The canary records the case as UNAVAILABLE."""


class Asker(Protocol):
    async def ask(self, case: Mapping[str, Any]) -> CaseAnswer | None:
        """The structured answer for one case, or None when the provider gave none. Raise AskerUnavailable for a failed call."""
        ...


def _digest(document: Any) -> str:
    return "sha256:" + hashlib.sha256(json.dumps(document, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")).hexdigest()


def _pinned(pinned: Mapping[str, Any]) -> Json:
    if pinned.get("kind") not in KINDS:
        raise ValueError(f"kind must be one of {', '.join(KINDS)}, not {pinned.get('kind')!r}")
    for name in ("provider", "model_id"):
        value = pinned.get(name)
        if not isinstance(value, str) or not value or value != value.strip():
            raise ValueError(f"{name} must be a non-empty string without surrounding space")
    if not is_pinned(pinned.get("version")):
        raise ValueError(f"version {pinned.get('version')!r} is not pinned: a dated or numbered version, never an alias such as latest")
    return {name: pinned[name] for name in ("kind", "provider", "model_id", "version")}


def _same(a: Any, b: Any) -> bool:
    """JSON equality: a boolean is never a number (Python's True == 1 is not JSON's), 1 and 1.0 are the same number, containers compare member by member."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool) and a == b
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(_same(a[k], b[k]) for k in a)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(_same(x, y) for x, y in zip(a, b, strict=True))
    if isinstance(a, dict | list) or isinstance(b, dict | list):
        return False
    return bool(a == b)


def _differing(expected: Mapping[str, Any], observed: Mapping[str, Any]) -> list[Json]:
    out: list[Json] = []
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


def _check_inputs(subset: Mapping[str, Any], baseline: Mapping[str, Any], pin: Mapping[str, Any]) -> None:
    if subset.get("layer") != LAYER or any(c.get("suite_layer") != LAYER for c in subset["cases"]):
        raise ValueError("the subset holds a case outside the GENERATED layer; held-out and partner cases never reach the model service (INV-22)")
    cases = subset["cases"]
    if not cases:
        raise ValueError("the subset is empty: an empty qualification suite is not a clean canary")
    ids = [c["case_id"] for c in cases]
    if subset["case_ids"] != ids or ids != sorted(ids) or len(set(ids)) != len(ids) or subset["n_cases"] != len(cases) or subset["cases_sha256"] != _digest(cases):
        raise ValueError("the subset does not match its own header (case_ids, n_cases or cases_sha256): it was truncated or edited")
    if set(baseline["cases"]) != set(ids):
        raise ValueError("the baseline does not cover exactly the cases of the subset")
    if not is_pinned(baseline["pinned"].get("version")):
        raise ValueError("the baseline does not name a pinned version (a floating alias is not a baseline)")
    for case_id, entry in baseline["cases"].items():
        if not isinstance(entry.get("slots"), dict) or entry.get("slots_sha256") != _digest(entry["slots"]):
            raise ValueError(f"the baseline for {case_id} does not match its own slots_sha256: it was edited or truncated")
    if baseline["subset_id"] != subset["subset_id"] or baseline["cases_sha256"] != subset["cases_sha256"]:
        raise ValueError("the baseline was taken on a different subset than this one (subset_id or cases_sha256 differ)")
    base_pin = baseline["pinned"]
    if any(base_pin[name] != pin[name] for name in ("kind", "provider", "model_id")):
        raise ValueError(f"the baseline is for {base_pin['provider']}/{base_pin['model_id']} ({base_pin['kind']}), not {pin['provider']}/{pin['model_id']} ({pin['kind']})")


async def _ask(asker: Asker, case: Mapping[str, Any], timeout_s: float) -> CaseAnswer | None:
    try:
        async with asyncio.timeout(timeout_s):
            return await asker.ask(case)
    except (AskerUnavailable, TimeoutError):
        return None


async def run_canary(*, subset: Mapping[str, Any], baseline: Mapping[str, Any], pinned: Mapping[str, Any], asker: Asker, commit_sha: str, recorded_on: str, timeout_s: float) -> Json:
    """Ask every subset case once, in order, and return the run record. A failed or timed-out call is UNAVAILABLE for that case; any other error propagates."""
    if type(timeout_s) not in (int, float) or not math.isfinite(timeout_s) or timeout_s <= 0:
        raise ValueError(f"timeout_s must be a positive finite number from configuration, not {timeout_s!r} (None would wait forever)")
    pin = _pinned(pinned)
    _check_inputs(subset, baseline, pin)
    if not _SHA.match(commit_sha):
        raise ValueError(f"commit_sha must be a 40-character lowercase hex SHA, not {commit_sha!r}")
    rows: list[Json] = []
    for case in subset["cases"]:
        case_id = case["case_id"]
        expected = baseline["cases"][case_id]
        answer = await _ask(asker, case, timeout_s)
        if answer is None:
            rows.append({"case_id": case_id, "status": "UNAVAILABLE", "answered_by": None, "version_mismatch": False, "differing_slots": [],
                         "expected_sha256": expected["slots_sha256"], "observed_sha256": None})
            continue
        diffs = _differing(expected["slots"], answer.slots)
        mismatch = answer.answered_by != pin["version"]
        rows.append({"case_id": case_id, "status": "DRIFT" if diffs or mismatch else "MATCH", "answered_by": answer.answered_by, "version_mismatch": mismatch,
                     "differing_slots": diffs, "expected_sha256": expected["slots_sha256"], "observed_sha256": _digest(dict(answer.slots))})
    return {"schema": RUN_SCHEMA, "subset_id": subset["subset_id"], "layer": LAYER, "n_cases": subset["n_cases"], "cases_sha256": subset["cases_sha256"], "pinned": pin,
            "baseline": {"pinned": dict(baseline["pinned"]), "commit_sha": baseline["commit_sha"], "sha256": _digest(baseline)}, "commit_sha": commit_sha, "recorded_on": recorded_on,
            "answered_versions": sorted({r["answered_by"] for r in rows if r["answered_by"] is not None}), "result": _result([r["status"] for r in rows]), "cases": rows}
