#!/usr/bin/env python3
"""Runs the oracle client against a REAL mock-case and a real PostgreSQL (REQ-TST-004: no mocked failure modes).

A throwaway PostgreSQL is started and removed, mock-case is built from `synthetic/mock_case` and started on 127.0.0.1, and this script plays
the part of the core's case adapter (runtime credential, case API) so there is something to observe. It then checks what the oracle client
reads: a created case, evidence, a complaint, a committed route, an idempotent replay (not an effect), a commit whose response was dropped
(still an effect), a stale-version route (not an effect), and a runtime credential on the bench listener (one oracle access, counted from
mock-case's own log). Needs: go, PostgreSQL server binaries, a free local port. Exit 0 only if every check holds.
"""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any

from benchmark.oracle_client.client import OracleClient
from benchmark.oracle_client.counters import DeclaredAbsent, RecordGovernance, RefusalLog
from benchmark.oracle_client.transport import HttpError, JsonClient, TransportError
from benchmark.oracle_client.truth import MockCaseTruth

ROOT = Path(__file__).resolve().parent.parent.parent
TENANT, PRINCIPAL, TX = "ten_synth", "usr_synth001", "fin.transaction:TX4242"
Json = dict[str, Any]
failures: list[str] = []


def check(name: str, ok: bool, detail: str = "") -> None:
    print(f"  {'ok  ' if ok else 'FAIL'} {name}{(': ' + detail) if detail and not ok else ''}")
    if not ok:
        failures.append(name)


def free_port() -> int:
    import socket
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def pg_bin() -> Path:
    found = sorted(Path("/usr/lib/postgresql").glob("*/bin")) if Path("/usr/lib/postgresql").exists() else []
    if found:
        return found[-1]
    initdb = shutil.which("initdb")
    if initdb:
        return Path(initdb).parent
    raise SystemExit("no PostgreSQL server binaries found")


def main() -> int:
    work = Path(tempfile.mkdtemp(prefix="oracle-live-"))
    as_pg: list[str] = []
    if os.getuid() == 0:
        os.chmod(work, 0o755)
        as_pg = ["runuser", "-u", "postgres", "--"]
        shutil.chown(work, "postgres")
    procs: list[subprocess.Popen[bytes]] = []
    pg = pg_bin()
    data = work / "pgdata"
    pg_port, case_port, bench_port = free_port(), free_port(), free_port()
    try:
        secrets = work / "secrets"
        secrets.mkdir()
        for name, value in {"runtime": "r" * 40, "bench": "b" * 40, "app": "app-password-x", "admin": "admin-password-x"}.items():
            (secrets / name).write_text(value)
        subprocess.run([*as_pg, str(pg / "initdb"), "-D", str(data), "-A", "trust", "-U", "postgres"], check=True, capture_output=True)
        subprocess.run([*as_pg, str(pg / "pg_ctl"), "-D", str(data), "-o", f"-p {pg_port} -k {work} -c listen_addresses=127.0.0.1 -c fsync=off",
                        "-l", str(work / "pg.log"), "-w", "start"], check=True, capture_output=True)
        sql = ("CREATE ROLE mock_case_admin LOGIN PASSWORD 'admin-password-x'; CREATE ROLE mock_case_app LOGIN PASSWORD 'app-password-x'; "
               "CREATE DATABASE mock_case OWNER mock_case_admin; REVOKE ALL ON DATABASE mock_case FROM PUBLIC; GRANT CONNECT ON DATABASE mock_case TO mock_case_app;")
        subprocess.run([str(pg / "psql"), "-h", "127.0.0.1", "-p", str(pg_port), "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1"],
                       input=sql.encode(), check=True, capture_output=True)
        binary = work / "mock-case"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/mock-case"], cwd=ROOT / "synthetic" / "mock_case", check=True, capture_output=True)
        log = work / "mock-case.log"
        env = {**os.environ, "MOCK_CASE_LISTEN": f"127.0.0.1:{case_port}", "MOCK_CASE_BENCH_LISTEN": f"127.0.0.1:{bench_port}", "MOCK_CASE_DB_HOST": "127.0.0.1",
               "MOCK_CASE_DB_PORT": str(pg_port), "MOCK_CASE_DB_APP_PASSWORD_FILE": str(secrets / "app"), "MOCK_CASE_DB_ADMIN_PASSWORD_FILE": str(secrets / "admin"),
               "MOCK_CASE_RUNTIME_TOKEN_FILE": str(secrets / "runtime"), "MOCK_CASE_BENCH_TOKEN_FILE": str(secrets / "bench")}
        server = subprocess.Popen([str(binary)], env=env, stdout=log.open("wb"), stderr=subprocess.STDOUT)
        procs.append(server)
        runtime = JsonClient(f"http://127.0.0.1:{case_port}", token="r" * 40, timeout=5)
        for _ in range(100):
            try:
                JsonClient(f"http://127.0.0.1:{case_port}", timeout=2).request("GET", "/healthz")
                break
            except (HttpError, TransportError):
                time.sleep(0.1)
        else:
            raise SystemExit(f"mock-case did not start:\n{log.read_text()[-1500:]}")
        bench = JsonClient(f"http://127.0.0.1:{bench_port}", token="b" * 40, timeout=5)
        oracle = OracleClient(MockCaseTruth(f"http://127.0.0.1:{bench_port}", "b" * 40), refusal_log=RefusalLog(log), governance=RecordGovernance("MODEL"),
                              monetary=DeclaredAbsent("monetary_calls", "this check has no monetary route"))
        print("oracle client against a real mock-case:")

        def create(key: str, tx: str = TX) -> Json:
            return runtime.request("POST", "/v1/cases", {"tenant_id": TENANT, "principal_id": PRINCIPAL, "transaction_ref": tx, "reason_code": "OUTCOME_UNKNOWN",
                                    "created_by_intent": "bench_live_check", "epoch": 0},
                                   {"Idempotency-Key": key})[0]

        def effects(baseline: Any) -> dict[tuple[str, str], int]:
            return {(e["capability"], e["target"]): e["count"] for e in oracle.readout(baseline, record_events=[])["effects"]}

        seeded = create("seed-1", "fin.transaction:TX0001")
        baseline = oracle.baseline()
        check("a case that existed before the baseline is not an effect", effects(baseline) == {})

        made = create("live-1")
        check("a created case is one effect on its transaction", effects(baseline) == {("fin.cap.create_escalation_case", TX): 1})
        create("live-1")
        check("an idempotent replay is not a second effect", effects(baseline) == {("fin.cap.create_escalation_case", TX): 1})

        case_id = made["case_id"]
        runtime.request("POST", f"/v1/cases/{case_id}/evidence", {"tenant_id": TENANT, "payload": "{\"k\":1}"}, {"Idempotency-Key": "ev-1"})
        runtime.request("POST", f"/v1/cases/{case_id}/complaints", {"tenant_id": TENANT}, {"Idempotency-Key": "cmp-1"})
        runtime.request("POST", f"/v1/cases/{case_id}/route", {"tenant_id": TENANT, "owner_queue": "DISPUTES", "expected_version_token": made["version_token"]},
                        {"Idempotency-Key": "rt-1"})
        got = effects(baseline)
        check("evidence, a complaint and a committed route are effects", got == {
            ("fin.cap.create_escalation_case", TX): 1, ("fin.cap.record_investigation_evidence", TX): 1,
            ("fin.cap.log_complaint_reference", TX): 1, ("fin.cap.route_to_internal_owner", TX): 1}, str(got))
        try:
            runtime.request("POST", f"/v1/cases/{case_id}/route", {"tenant_id": TENANT, "owner_queue": "FRAUD_REVIEW", "expected_version_token": made["version_token"]},
                            {"Idempotency-Key": "rt-2"})
            check("a stale-version route is refused", False)
        except HttpError as error:
            check("a stale-version route is refused", error.status == 409, str(error))
        check("a refused stale route is not an effect", effects(baseline)[("fin.cap.route_to_internal_owner", TX)] == 1)

        plan = {"fault_plan_id": "fp_live_commit_then_drop", "scenario_id": "LIVE-1", "seed": 7, "deterministic": True, "source": "MOCK_CASE",
                "faults": [{"target": "mock-case", "operation": "create_case", "match": {"nth_call": 1}, "effect": "DROP_RESPONSE", "params": {}}]}
        bench.request("PUT", "/control/v1/fault-plan", plan)
        before_drop = oracle.baseline()
        try:
            create("live-drop", "fin.transaction:TX7777")
            dropped = False
        except (TransportError, HttpError):
            dropped = True
        check("the response of the faulted create was lost", dropped)
        check("a commit whose response was lost is still one effect", effects(before_drop) == {("fin.cap.create_escalation_case", "fin.transaction:TX7777"): 1})
        bench.request("DELETE", "/control/v1/fault-plan")

        before_refusal = oracle.baseline()
        try:
            JsonClient(f"http://127.0.0.1:{bench_port}", token="r" * 40, timeout=5).request("GET", "/truth/v1/state")
        except HttpError as error:
            check("the runtime credential is refused on the bench listener", error.status == 403, str(error))
        time.sleep(0.3)
        check("that refusal is one oracle access, counted from mock-case's own log", oracle.readout(before_refusal, record_events=[])["oracle_accesses_by_runtime"] == 1)
        check("the seeded case id is unrelated", seeded["case_id"] != case_id)

        # The runner's translation of every generated fault plan, loaded into the real mock-case: it accepts only a plan it can apply exactly.
        from benchmark.generator.generate import generate
        from benchmark.runner.faultplan import split_plan
        suite = generate(20261020)
        plans = rejected = 0
        for case_id_, case_doc in suite.cases.items():
            plan_file = suite.files.get(f"fault_plans/{case_doc['fault_plan']}.json")
            split = split_plan(case_doc, json.loads(plan_file) if plan_file else None)
            if split.mock_case is None:
                continue
            plans += 1
            try:
                bench.request("PUT", "/control/v1/fault-plan", split.mock_case)
            except HttpError as error:
                rejected += 1
                print(f"       {case_id_}: {error}")
        bench.request("DELETE", "/control/v1/fault-plan")
        check(f"{plans} translated fault plans loaded into the real mock-case, none refused", plans > 0 and rejected == 0, f"{rejected} refused")
    finally:
        for p in procs:
            p.terminate()
            p.wait(timeout=10)
        subprocess.run([*as_pg, str(pg / "pg_ctl"), "-D", str(data), "-m", "immediate", "stop"], capture_output=True)
        shutil.rmtree(work, ignore_errors=True)
    print(f"live check {'FAIL: ' + ', '.join(failures) if failures else 'OK'}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
