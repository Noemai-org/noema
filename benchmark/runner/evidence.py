"""G1 evidence helpers, two of the Security Reviewer's criteria on noema#436 (14 and 15 Nov): number 6, the fault is a FaultPlan with its plan and seed in the evidence, and number 9,
identity and tenant are exercised, not assumed (one negative probe of each kind per case). The leak scan with a planted canary (number 8) is in the workflow, which runs
tools/ci/leakscan.py; its logic is tested in tools/ci/g1_leakscan.py (tested in tools/ci/test_g1_leakscan.py).

Two commands, each writing one JSON file under --out and exiting 0 (met), 1 (a check failed: the evidence says NOT MET), 2 (bad input) or 3 (NOT RUN: what it needs is missing, never a pass):

  python -m benchmark.runner.evidence fault-plans --suite DIR --run DIR --out DIR
      For every case of the run: its fault plan's id, seed, producer (`source`), effects and the SHA-256 of the plan document, read from the generated suite (never the oracle).
      A case that declares a plan needs a plan with a seed and a real producer (TUSHE, MOCK_CASE or PROXY: the producers of AGENT-12, never a test double); the run needs at least one
      plan with the effect TIMEOUT_AFTER_COMMIT (US-06).
  python -m benchmark.runner.evidence probes --suite DIR --run DIR --api-url URL --idp-* ... --tenant-id T --other-tenant-id T2 --out DIR
      For every case of the run, on the case's own reconstruction (GET /v1/reconstructions/{correlation_id}, ops and audit only): a control with an audit session of the case's principal
      and tenant must be served and its Record must carry the token's tenant and the principal's own actions only; a session with a role the route does not list (customer) must get
      404 NOT_FOUND_OR_NOT_AUTHORIZED; a session of another tenant must get 401 SESSION_INVALID (the verifier refuses a tenant the deployment does not serve before any handler). The
      control proves the route exists and serves this case, so the two refusals are not the answer to an unmounted route.

The correlation ids come from `correlations.json`, which the runner writes into its run directory. SYNTHETIC (AGENT-25)."""
import argparse
import json
import re
import secrets
import sys
from collections.abc import Callable, Mapping, Sequence
from pathlib import Path
from typing import Any

from benchmark.contracts import sha256_of
from benchmark.oracle_client.transport import HttpError, JsonClient, TransportError
from benchmark.runner.env import EnvironmentUnavailable, IdpSessionProvider
from benchmark.runner.suite import SuiteError, load_suite

Json = dict[str, Any]
PRODUCERS = frozenset({"TUSHE", "MOCK_CASE", "PROXY"})  # the real producers of a fault (AGENT-12, REQ-TST-004)
US06_EFFECT = "TIMEOUT_AFTER_COMMIT"
ROUTE = "/v1/reconstructions/{}"
ALLOWED_ROLE = "audit"  # a role the reconstruction route lists (ops and audit only; PERD 39.4, the authorization matrix)
WRONG_ROLE = "customer"  # a role it does not list
NOT_FOUND = (404, "NOT_FOUND_OR_NOT_AUTHORIZED")
SESSION_INVALID = (401, "SESSION_INVALID")


class NotRun(Exception):
    pass


def fault_plan_evidence(suite: Any, only: set[str] | None) -> Json:
    rows: list[Json] = []
    problems: list[str] = []
    us06 = False
    for lc in suite.cases:
        case_id = lc.case["case_id"]
        if only is not None and case_id not in only:
            continue
        declared = lc.case["fault_plan"]
        row: Json = {"case_id": case_id, "fault_plan_id": declared}
        if declared != "fp_none":
            plan = lc.plan
            if plan is None:
                problems.append(f"{case_id}: declares {declared} but no plan was loaded")
            else:
                effects = sorted({f["effect"] for f in plan["faults"]})
                row.update(seed=plan["seed"], deterministic=plan["deterministic"], source=plan["source"], effects=effects, plan_sha256=sha256_of(plan))
                if not isinstance(plan["seed"], int) or isinstance(plan["seed"], bool):
                    problems.append(f"{case_id}: the plan has no integer seed")
                if plan["source"] not in PRODUCERS:
                    problems.append(f"{case_id}: the plan's producer {plan['source']!r} is not one of {sorted(PRODUCERS)}")
                us06 = us06 or US06_EFFECT in effects
        rows.append(row)
    if not us06:
        problems.append(f"no plan of the run has the effect {US06_EFFECT}: the timeout-after-write case has no recorded fault")
    return {"met": not problems, "problems": problems, "cases": rows}


def _expect(error: Exception, want: tuple[int, str]) -> Json:
    if isinstance(error, HttpError):
        return {"status": error.status, "code": error.code, "message": error.message, "ok": (error.status, error.code) == want}
    return {"status": None, "code": None, "ok": False, "detail": type(error).__name__}


KIND_OF_ROLE = {"customer": "customer", "ops": "operator", "audit": "auditor"}  # the generator's roles and the Record's principal kinds (A149: ops is operator, audit is auditor)


def _kinds(roles: Sequence[str]) -> tuple[str, ...]:
    unknown = [r for r in roles if r not in KIND_OF_ROLE]
    if unknown:
        raise NotRun(f"a case's principal has a role with no known principal kind: {unknown}")
    return tuple(KIND_OF_ROLE[r] for r in roles)


def _record_problems(body: Json, tenant: str, principal: str, correlation_id: str, kinds: Sequence[str] = ("customer",)) -> list[str]:
    """`kinds` are the principal kinds of the case's own roles (a customer case: customer; an ops case: operator; an audit case: auditor): the Record must carry the principal's own
    action as `<kind>:<principal>` and no action of that kind by anyone else."""
    problems: list[str] = []
    if body.get("tenant_id") != tenant:
        problems.append("the reconstruction's tenant is not the token's")
    if body.get("correlation_id") != correlation_id:
        problems.append("the reconstruction answers another correlation")
    events = body.get("events")
    if not isinstance(events, list) or not events:
        return problems + ["the reconstruction carries no events"]
    own = 0
    for event in events:
        if event.get("tenant_id") != tenant:
            problems.append(f"event {event.get('event_id')} is of another tenant")
        actor = str(event.get("actor", ""))
        if any(actor.startswith(f"{kind}:") for kind in kinds):
            if actor in {f"{kind}:{principal}" for kind in kinds}:
                own += 1
            else:
                problems.append(f"event {event.get('event_id')} names a {actor.split(':')[0]} other than the token's principal")
    if own == 0:
        problems.append(f"no event carries the principal's own action ({' or '.join(f'{k}:<id>' for k in kinds)})")
    return problems


def negative_probes(base_url: str, issue: Callable[[str, str, str], str], cases: list[tuple[str, str, str]], tenant: str, other_tenant: str, *, timeout: float = 10.0,
                    roles: Mapping[str, Sequence[str]] | None = None) -> Json:
    """`cases` are (case_id, principal_id, correlation_id). `issue(tenant, principal, role)` returns a session token. `roles` maps a case id to the roles of its principal (the case's
    `principal.roles`); a case not in it is a customer case."""
    rows: list[Json] = []
    kinds = {case_id: _kinds((roles or {}).get(case_id, ("customer",))) for case_id, _, _ in cases}  # before any request: an unknown role is NOT RUN, not a failed row
    for case_id, principal, correlation in cases:
        path = ROUTE.format(correlation)
        row: Json = {"case_id": case_id, "principal_id": principal, "correlation_id": correlation}
        try:
            body, _ = JsonClient(base_url, token=issue(tenant, principal, ALLOWED_ROLE), timeout=timeout).request("GET", path)
            problems = _record_problems(body, tenant, principal, correlation, kinds[case_id])
            row["control"] = {"status": 200, "ok": not problems, "problems": problems}
        except (HttpError, TransportError) as error:
            row["control"] = _expect(error, (200, ""))
        for label, token_tenant, role, want in (("wrong_role", tenant, WRONG_ROLE, NOT_FOUND), ("foreign_tenant", other_tenant, ALLOWED_ROLE, SESSION_INVALID)):
            try:
                JsonClient(base_url, token=issue(token_tenant, principal, role), timeout=timeout).request("GET", path)
                row[label] = {"status": 200, "code": None, "ok": False, "detail": "the request was served"}
            except (HttpError, TransportError) as error:
                row[label] = _expect(error, want)
        # A correlation that does not exist, asked by an allowed role, must be answered exactly as a wrong role is: the same status, code and message. If it differed, the refusal would tell
        # a caller whether a reconstruction exists (an oracle); this is the "no oracle" half of the Security Reviewer's criterion 9 (condition 2 on noema#591).
        unknown = "corr_" + secrets.token_hex(13)
        try:
            JsonClient(base_url, token=issue(tenant, principal, ALLOWED_ROLE), timeout=timeout).request("GET", ROUTE.format(unknown))
            row["unknown_correlation"] = {"status": 200, "code": None, "ok": False, "detail": "a correlation that does not exist was served"}
        except (HttpError, TransportError) as error:
            got = _expect(error, NOT_FOUND)
            same = all(got.get(k) == row["wrong_role"].get(k) for k in ("status", "code", "message"))
            got["ok"] = got["ok"] and same
            if not same:
                got["detail"] = "the answer differs from the wrong-role answer"
            row["unknown_correlation"] = got
        row["ok"] = all(row[k]["ok"] for k in ("control", "wrong_role", "foreign_tenant", "unknown_correlation"))
        rows.append(row)
    return {"met": bool(rows) and all(r["ok"] for r in rows), "cases": rows}


_SHA = re.compile(r"[0-9a-f]{40}")
_RUN_ID = re.compile(r"[0-9]{1,20}")


def _stamped(doc: Json, sha: str | None, run_id: str | None) -> Json:
    """The document with the dispatch SHA and run id in a top-level "stamp", for the G1 package to compare with the run it reads (noema#717). With neither asked for, the document as it was."""
    if sha is None and run_id is None:
        return doc
    return {**doc, "stamp": {"sha": sha, "run_id": run_id}}


def _write(out: Path, name: str, doc: Json) -> None:
    out.mkdir(parents=True, exist_ok=True)
    (out / name).write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _run_cases(run: Path) -> set[str]:
    results = run / "results"
    if not results.is_dir():
        raise NotRun(f"no results under {run}")
    ids = {p.stem for p in results.glob("*.json")}
    if not ids:
        raise NotRun("the run produced no result")
    return ids


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="benchmark.runner.evidence")
    sub = p.add_subparsers(dest="command", required=True)
    for name in ("fault-plans", "probes"):
        s = sub.add_parser(name)
        s.add_argument("--suite", type=Path, required=True)
        s.add_argument("--run", type=Path, required=True, help="the runner's output directory (results/ and correlations.json)")
        s.add_argument("--out", type=Path, required=True)
        s.add_argument("--sha", help="the dispatch commit SHA (40 lowercase hexadecimal); stamped into the evidence file; with --run-id, both or neither")
        s.add_argument("--run-id", help="the workflow run id (digits); stamped into the evidence file")
    probes = sub.choices["probes"]
    probes.add_argument("--api-url", required=True)
    probes.add_argument("--idp-binary", type=Path, required=True)
    probes.add_argument("--idp-key", type=Path, required=True)
    probes.add_argument("--idp-kid", required=True)
    probes.add_argument("--idp-audience", required=True)
    probes.add_argument("--tenant-id", required=True)
    probes.add_argument("--other-tenant-id", required=True)
    a = p.parse_args(argv)
    if (a.sha is None) != (a.run_id is None) or (a.sha is not None and not (_SHA.fullmatch(a.sha) and _RUN_ID.fullmatch(a.run_id or ""))):
        print("refused: --sha (40 lowercase hexadecimal) and --run-id (digits) are given together and well formed, or not at all")
        return 2
    if a.command == "probes" and a.other_tenant_id == a.tenant_id:
        print("refused: the other tenant must differ from the run's tenant")
        return 2
    try:
        suite = load_suite(a.suite)
        ran = _run_cases(a.run)
        if a.command == "fault-plans":
            doc = fault_plan_evidence(suite, ran)
            _write(a.out, "fault-plans.json", _stamped(doc, a.sha, a.run_id))
            print(f"fault plans: {'MET' if doc['met'] else 'NOT MET'}: {'; '.join(doc['problems']) or 'every plan has its id, seed and producer'}")
            return 0 if doc["met"] else 1
        correlations = json.loads((a.run / "correlations.json").read_text(encoding="utf-8")) if (a.run / "correlations.json").is_file() else {}
        by_case = {lc.case["case_id"]: lc.case for lc in suite.cases}
        wanted = [(cid, by_case[cid]["principal"]["principal_id"], correlations[cid]) for cid in sorted(ran) if cid in by_case and cid in correlations]
        if not wanted or len(wanted) != len(ran):
            raise NotRun("not every case of the run has a correlation id (correlations.json): nothing to probe")
        sessions = {t: IdpSessionProvider(a.idp_binary, a.idp_key, kid=a.idp_kid, audience=a.idp_audience, tenant_id=t) for t in (a.tenant_id, a.other_tenant_id)}
        for provider in sessions.values():
            if provider.unavailable_reason:
                raise NotRun(provider.unavailable_reason)
        doc = negative_probes(a.api_url, lambda tenant, principal, role: sessions[tenant].token_for(principal, [role]), wanted, a.tenant_id, a.other_tenant_id, roles={cid: by_case[cid]["principal"]["roles"] for cid, _, _ in wanted})
        _write(a.out, "negative-probes.json", _stamped(doc, a.sha, a.run_id))
        bad = [r["case_id"] for r in doc["cases"] if not r["ok"]]
        print(f"negative probes: {'MET' if doc['met'] else 'NOT MET'} for {len(doc['cases'])} cases" + (f"; failing: {', '.join(bad)}" if bad else ""))
        return 0 if doc["met"] else 1
    except (NotRun, EnvironmentUnavailable) as error:
        print(f"NOT RUN: {error}")
        return 3
    except SuiteError as error:
        print(f"refused: {error}")
        return 2


if __name__ == "__main__":
    sys.exit(main())
