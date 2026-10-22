"""One case, one code path (REQ-BEN-002).

1 reset the case system   2 load the fixture   3 load the fault plan   4 baseline the oracle   5 drive the inputs through the public API as a partner app
would, advancing clocks and answering confirmations in the order of the case's timeline   6 collect the Record   7 read the environment through the
oracle client   8 write a result the scorer can reproduce a score from.

Anything the run cannot do exactly is an ERROR with a code, never a quiet partial run (REQ-BEN-001): the case counts as not correctly handled and the run
is INCOMPLETE. The runner reads no oracle document and never asks Noema for ground truth."""
import hashlib
import json
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, pretty_bytes, sha256_of
from benchmark.oracle_client.counters import CounterUnavailable
from benchmark.oracle_client.transport import HttpError, TransportError

from .api import NotSettled
from .control import PlanRejected
from .drivers import driven, split_events
from .env import EnvironmentUnavailable, RunEnv
from .faultplan import CLOCK_START, split_plan
from .reconstruction import ReconstructionError, parse
from .suite import LoadedCase, Suite

Json = dict[str, Any]
_contracts = Contracts()


class CaseError(Exception):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


def _error_code(error: Exception) -> tuple[str, str]:
    if isinstance(error, CaseError):
        return error.code, str(error)
    if isinstance(error, NotSettled):
        return "NOT_SETTLED", str(error)
    if isinstance(error, ReconstructionError):
        return "RECONSTRUCTION_UNEXPECTED", str(error)
    if isinstance(error, CounterUnavailable):
        return "ORACLE_COUNTER_UNAVAILABLE", str(error)
    if isinstance(error, PlanRejected):
        return "FAULT_PLAN_REJECTED", f"mock-case refused the plan: {error}"
    if isinstance(error, EnvironmentUnavailable):
        return ("FIXTURE_UNAVAILABLE" if error.component == "fixture loader" else "SESSION_UNAVAILABLE"), str(error)
    if isinstance(error, HttpError):
        return ("API_REFUSED" if error.status < 500 else "API_FAILED"), str(error)
    if isinstance(error, TransportError):
        return "API_UNREACHABLE", f"no answer: {error}"
    raise error


def _path(observed: Json, split_has_plan: bool) -> str:
    if observed["clarification_asked"]:
        return "CLARIFICATION"
    if observed["escalation_opened"]:
        return "ESCALATION"
    return "FAULTED" if split_has_plan else "CLEAN"


def _digest(observed: Json, environment: Json) -> str:
    verdicts = sorted(observed["verdicts"], key=lambda v: (v["capability"], v["target"], v["verdict"]))
    return sha256_of({"disposition": observed["disposition"], "verdicts": verdicts, "effects": environment["effects"]})


def _kill_switch(env: RunEnv, case: Json, argument: str) -> None:
    if env.admin_token is None or env.tenant_id is None:
        raise CaseError("ADMIN_UNAVAILABLE", "kill_switch needs the admin credential and the tenant id")
    env.api.kill_switch(env.admin_token, env.tenant_id, argument == "on", f"benchmark case {case['case_id']}")


def _drive(env: RunEnv, token: str, case: Json, events: list[tuple[str, str]], conv: Any) -> None:
    for kind, argument in events:
        if kind == "clock_advance":
            env.control.advance(argument)
        elif kind == "confirm":
            found = env.api.confirmation_id(env.api.events(token, conv.conversation_ref, timeout_s=env.settle_timeout_s))
            if found is None:
                raise CaseError("CONFIRMATION_NOT_FOUND", "the investigation never asked for a confirmation")
            env.api.confirm(token, found, f"{case['case_id']}-confirm", argument)
        elif kind == "kill_switch":
            _kill_switch(env, case, argument)
        env.api.wait_settled(token, conv.investigation_id, timeout_s=env.settle_timeout_s, poll_s=env.poll_s, sleep=env.sleep, now=env.now)


def _execute(env: RunEnv, lc: LoadedCase) -> Json:
    case = lc.case
    started = env.now()
    undriven = [e for e in case["events_timeline"] if not driven(e["event"])]
    if undriven:
        raise CaseError("EVENT_KIND_UNSUPPORTED", "no driver for: " + ", ".join(sorted({e["event"].split(":")[0] for e in undriven})))
    split = split_plan(case, lc.plan, grant_ttl_seconds=env.grant_ttl_seconds)
    if split.unsupported:
        raise CaseError("FAULT_UNSUPPORTED", "; ".join(split.unsupported)[:290])
    principal = case["principal"]
    token = env.sessions.token_for(principal["principal_id"], principal["roles"])

    env.control.reset()
    env.fixtures.load(lc.fixture)
    if split.mock_case:
        env.control.load_plan(split.mock_case)
    baseline = env.oracle.baseline()

    before, after = split_events(case)
    texts = [u["text"] for u in sorted(case["utterances"], key=lambda u: u["turn"])]
    locale = case["locale_mix"][0]
    for kind, argument in before:
        if kind == "kill_switch":
            _kill_switch(env, case, argument)
        elif kind == "clock_advance":
            env.control.advance(argument)
    conv = env.api.start(token, f"{case['case_id']}-t1", texts[0], locale_hint=locale)
    env.api.wait_settled(token, conv.investigation_id, timeout_s=env.settle_timeout_s, poll_s=env.poll_s, sleep=env.sleep, now=env.now)
    _drive(env, token, case, after, conv)
    for number, text in enumerate(texts[1:], start=2):
        env.api.message(token, conv.conversation_ref, f"{case['case_id']}-t{number}", text=text)
        env.api.wait_settled(token, conv.investigation_id, timeout_s=env.settle_timeout_s, poll_s=env.poll_s, sleep=env.sleep, now=env.now)
    parsed = parse(env.api.reconstruction(token, conv.correlation_id))
    environment = env.oracle.readout(baseline, parsed.record_events)
    return {
        "run_id": env.run_id, "case_id": case["case_id"], "case_version": case["version"], "status": "COMPLETED", "error": None,
        "observed": parsed.observed, "environment": environment, "path": _path(parsed.observed, split.mock_case is not None or lc.plan is not None),
        "latency_ms": max(0, int((env.now() - started) * 1000)), "cost_usd_micros": parsed.cost_usd_micros,
        "outcome_digest": _digest(parsed.observed, environment),
    }


def _errored(env: RunEnv, case: Json, code: str, message: str) -> Json:
    return {"run_id": env.run_id, "case_id": case["case_id"], "case_version": case["version"], "status": "ERROR",
            "error": {"code": code, "message": (message or code)[:300]}, "observed": None, "environment": None, "path": "ERROR", "latency_ms": 0,
            "cost_usd_micros": None, "outcome_digest": None}


def run_case(env: RunEnv, lc: LoadedCase) -> Json:
    """The result document for one case. Never raises for a case-level problem: that is an ERROR result."""
    try:
        result = _execute(env, lc)
    except (CaseError, NotSettled, ReconstructionError, CounterUnavailable, PlanRejected, EnvironmentUnavailable, HttpError, TransportError) as error:
        code, message = _error_code(error)
        return _errored(env, lc.case, code, message)
    problems = _contracts.errors("benchmark-case-result", result)
    if problems:
        return _errored(env, lc.case, "RESULT_INVALID", problems[0])
    return result


def preflight(env: RunEnv) -> list[str]:
    """What stops a run before it starts. Configuration first, with no network call; the network only when everything else is in place."""
    unmet: list[str] = []
    for part in (env.fixtures, env.sessions):
        reason = getattr(part, "unavailable_reason", None)
        if reason:
            unmet.append(f"{getattr(part, 'component', 'session provider')}: {reason}")
    unmet += [f"oracle counter {missing}" for missing in env.oracle.missing_sources()]
    if unmet:
        return unmet
    try:
        env.oracle.truth.digest()
    except (TransportError, HttpError) as error:
        unmet.append(f"oracle: mock-case's bench listener is not reachable ({error})")
    return unmet


def _write(path: Path, doc: Json) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(pretty_bytes(doc))


def run_suite(env: RunEnv, suite: Suite, out: Path, *, commit_sha: str | None, providers: list[str], suite_manifest_bytes: bytes,
              only: set[str] | None = None) -> Json:
    cases = [lc for lc in suite.cases if only is None or lc.case["case_id"] in only]
    unmet = preflight(env)
    results: list[Json] = []
    if not unmet:
        for lc in cases:
            result = run_case(env, lc)
            _write(out / "results" / f"{lc.case['case_id']}.json", result)
            results.append(result)
    completed = sum(1 for r in results if r["status"] == "COMPLETED")
    errors: dict[str, int] = {}
    for r in results:
        if r["status"] == "ERROR":
            errors[r["error"]["code"]] = errors.get(r["error"]["code"], 0) + 1
    invalid = [case_id for case_id, _ in suite.invalid]
    status = "NOT_RUN" if unmet else ("COMPLETE" if not errors and not invalid else "INCOMPLETE")
    first = suite.cases[0].case if suite.cases else None
    manifest: Json = {
        "run_id": env.run_id, "status": status, "not_run_reason": ("no access: " + "; ".join(unmet))[:500] if unmet else None,
        "suite": {"suite_id": suite.manifest["suite_id"], "suite_version": suite.manifest["suite_version"],
                  "manifest_sha256": "sha256:" + hashlib.sha256(suite_manifest_bytes).hexdigest(), "generator": suite.manifest["generator"]},
        "pack_version": first["pack_version"] if first else "fin@0.0.0", "policy_version": first["policy_version"] if first else "fin.pol@0.0",
        "commit_sha": commit_sha, "providers": providers, "counter_sources": env.oracle.sources(),
        "counts": {"total": len(cases) + len(invalid), "run": len(results), "completed": completed, "error": len(results) - completed, "invalid": len(invalid)},
        "errors_by_code": dict(sorted(errors.items())), "invalid_case_ids": sorted(invalid), "clock_start": CLOCK_START,
    }
    problems = _contracts.errors("benchmark-run-manifest", manifest)
    if problems:
        raise AssertionError(f"the run manifest does not validate: {problems[0]}")
    _write(out / "run-manifest.json", manifest)
    return {"status": status, "counts": manifest["counts"], "errors_by_code": manifest["errors_by_code"], "not_run_reason": manifest["not_run_reason"]}


__all__ = ["run_case", "run_suite", "preflight", "CaseError", "json"]
