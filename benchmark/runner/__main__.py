"""python -m benchmark.runner --suite DIR --out DIR ...

Exit 0: every case completed. 1: the run is incomplete (an ERROR or an excluded case). 2: an input is unusable, or an argument names the held-out set.
3: not run, no access (a required part of the environment is missing); the run manifest says which and nothing was faked."""
import argparse
import os
import subprocess
import sys
from pathlib import Path

from benchmark.oracle_client.client import OracleClient
from benchmark.oracle_client.counters import DeclaredAbsent, RecordGovernance, RefusalLog, Unavailable
from benchmark.oracle_client.transport import read_secret
from benchmark.oracle_client.truth import MockCaseTruth

from .api import PublicApi
from .control import MockCaseControl
from .env import DirSessionProvider, RunEnv, UnavailableFixtureLoader
from .run import run_suite
from .suite import SuiteError, load_suite
from .support import report as support_report

FORBIDDEN = "benchmark-heldout"
NO_LOADER = "no fixture loader is configured: Tushe's sandbox is not reachable from this build"


def _commit() -> str | None:
    try:
        sha = subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return None
    return sha if len(sha) == 40 else None


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="benchmark.runner", description=__doc__)
    p.add_argument("--suite", type=Path, required=True, help="a suite root written by `python -m benchmark.generator generate`")
    p.add_argument("--out", type=Path, required=True)
    p.add_argument("--run-id", default="run")
    p.add_argument("--api-url", default="http://core:8080", help="Noema's public API (PERD §39.2)")
    p.add_argument("--tokens-dir", type=Path, help="a directory with one session token file per principal id")
    p.add_argument("--admin-token-file", type=Path)
    p.add_argument("--bench-url", default="http://mock-case:8091", help="mock-case's bench listener (bench_net)")
    p.add_argument("--bench-credential-file", type=Path, help="the bench credential, a file (never an argument value)")
    p.add_argument("--mock-case-log", type=Path, help="mock-case's JSON log, captured with `docker compose logs --no-log-prefix mock-case`")
    p.add_argument("--model-writer", help="the Record `written_by` name of the model service")
    p.add_argument("--declare-no-monetary-route", metavar="REASON", help="declare that this run exposes no monetary route, with the reason; recorded, not observed")
    p.add_argument("--tenant-id", help="the tenant the admin API addresses, for kill_switch events")
    p.add_argument("--grant-ttl-seconds", type=int, help="the loaded trust profile's grant lifetime, for faults relative to it")
    p.add_argument("--provider", action="append", default=[], help="a model provider used in this run (repeatable)")
    p.add_argument("--only", action="append", help="run only this case id (repeatable)")
    p.add_argument("--commit-sha", default=None)
    p.add_argument("--support", action="store_true", help="print which cases this build can run and what stops the rest, then exit")
    args = p.parse_args(argv)

    named = [a for a in sys.argv[1:] + (argv or []) if FORBIDDEN in a] + [k for k, v in os.environ.items() if FORBIDDEN in v]
    if named:
        print(f"refused: {FORBIDDEN} is referenced by {named}")
        return 2
    try:
        suite = load_suite(args.suite)
        manifest_bytes = (args.suite / "manifest.json").read_bytes()
    except SuiteError as error:
        print(f"unusable suite: {error}")
        return 2
    if args.support:
        found = support_report(suite, grant_ttl_seconds=args.grant_ttl_seconds)
        print(f"{found['runnable']} of {found['total']} cases can run now ({found['blocked']} blocked; {found['without_faults_or_events']} need no fault and no event)")
        for reason, count in found["blocked_by"].items():
            print(f"  {count:4d}  {reason}")
        return 0
    credential = read_secret(args.bench_credential_file) if args.bench_credential_file else ""
    monetary = DeclaredAbsent("monetary_calls", args.declare_no_monetary_route) if args.declare_no_monetary_route else Unavailable(
        "monetary_calls", "no source: Tushe's access log is not reachable and the operator declared nothing")
    oracle = OracleClient(MockCaseTruth(args.bench_url, credential or "unset"), refusal_log=RefusalLog(args.mock_case_log) if args.mock_case_log else None,
                          governance=RecordGovernance(args.model_writer or ""), monetary=monetary)
    env = RunEnv(api=PublicApi(args.api_url), control=MockCaseControl(args.bench_url, credential or "unset"), oracle=oracle,
                 fixtures=UnavailableFixtureLoader(NO_LOADER), sessions=DirSessionProvider(args.tokens_dir or Path("/nonexistent-token-dir")), run_id=args.run_id,
                 admin_token=read_secret(args.admin_token_file) if args.admin_token_file else None, tenant_id=args.tenant_id, grant_ttl_seconds=args.grant_ttl_seconds)
    summary = run_suite(env, suite, args.out, commit_sha=args.commit_sha or _commit(), providers=args.provider, suite_manifest_bytes=manifest_bytes,
                        only=set(args.only) if args.only else None)
    counts = summary["counts"]
    if summary["status"] == "NOT_RUN":
        print(f"not run: {summary['not_run_reason']}")
        print(f"{counts['total']} cases were not run; nothing was faked. The run manifest is {args.out / 'run-manifest.json'}")
        return 3
    print(f"{summary['status']}: {counts['completed']} of {counts['total']} cases completed, {counts['error']} errors, {counts['invalid']} excluded; {summary['errors_by_code']}")
    return 0 if summary["status"] == "COMPLETE" else 1


if __name__ == "__main__":
    sys.exit(main())
