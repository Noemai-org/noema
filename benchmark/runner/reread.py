"""BEN-05 / A476: add the `after_recovery` or `after_replay` phase to a run's `reconstructions.json`.

`after_recovery` is read after a core container restart (the workflow's step) and a wait for /readyz 200, through the audit-only route (noema#783) as an AUDIT principal of the runner's own
(`usr_` and letters, as the evidence probes use): `IdpSessionProvider(...).token_for(probe_principal(), ["audit"])` with the same five IdP flags the run takes (A502; the stack's bench-tokens
runner-args file supplies them). Without them the phase is NOT RUN (exit 3), never faked.
`after_replay` is a SECOND READ of the same route: a re-read, not a replay (INV-21 is never claimed from it), and its bytes must equal the first. Every read is added to `requests.jsonl` (the core
writes a RECORD_ACCESSED line for each), and a phase whose bytes differ is returned and NOT written. Read before the stamp is written. SYNTHETIC."""
import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from collections.abc import Callable
from pathlib import Path
from typing import Any

from .api import PublicApi
from .env import IdpSessionProvider, SessionProvider
from .evidence import ALLOWED_ROLE, probe_principal
from .request_log import RequestRecorder

Json = dict[str, Any]
PHASES = ("after_recovery", "after_replay")
BEFORE = {"after_recovery": "initial", "after_replay": "after_recovery"}
NOT_RUN = 3


class ReadyTimeout(Exception):
    pass


def wait_ready(status: Callable[[], int | None], *, timeout_s: float, poll_s: float, sleep: Callable[[float], None] = time.sleep, now: Callable[[], float] = time.monotonic) -> None:
    """Return once `status()` is 200 (and only 200); raise ReadyTimeout after `timeout_s`."""
    start = now()
    while True:
        if status() == 200:
            return
        if now() - start >= timeout_s:
            raise ReadyTimeout(f"/readyz was not 200 within {timeout_s}s")
        sleep(poll_s)


def _canon(doc: Any) -> str:
    return json.dumps(doc, sort_keys=True, separators=(",", ":"))


def reread(run: Path, phase: str, api: PublicApi, sessions: SessionProvider) -> list[str]:
    """Read every case's reconstruction with the seeded audit session and add `phase`. Returns the cases whose read differs from the phase before (after_replay only); with any, the phase is not written.
    The reads are added to `requests.jsonl`. Nothing is written when a read fails or an input is unusable."""
    if phase not in PHASES:
        raise ValueError(f"unknown phase {phase!r}")
    try:
        doc = json.loads((run / "reconstructions.json").read_text(encoding="utf-8"))
        correlations = json.loads((run / "correlations.json").read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise ValueError(f"unreadable run files: {error}") from error
    if not isinstance(doc, dict) or not isinstance(correlations, dict) or "stamp" in doc:
        raise ValueError("reconstructions.json must be the unstamped {case: {phase: doc}} map")
    if not (run / "requests.jsonl").is_file():
        raise ValueError("requests.jsonl does not exist: the runner did not write its own request record")
    previous = BEFORE[phase]
    for case, phases in doc.items():
        if not isinstance(phases, dict) or previous not in phases or phase in phases:
            raise ValueError(f"{case}: needs {previous} and not {phase}")
        if case not in correlations:
            raise ValueError(f"{case}: no correlation id")
    recorder, read = RequestRecorder(), {}
    api.recorder = recorder
    try:
        for case in doc:
            recorder.begin(case)
            try:
                read[case] = api.reconstruction(sessions.token_for(probe_principal(), [ALLOWED_ROLE]), correlations[case])
            finally:
                recorder.end(case, correlations[case])
    finally:
        recorder.append(run / "requests.jsonl")  # the reads already made are in the core's access log, so they stay in the record even when a later one fails
    differing = [c for c in doc if phase == "after_replay" and _canon(read[c]) != _canon(doc[c][previous])]
    if differing:
        return differing
    for case in doc:
        doc[case][phase] = read[case]
    (run / "reconstructions.json").write_text(json.dumps(doc, indent=2, sort_keys=False) + "\n", encoding="utf-8", newline="\n")
    return []


def _status(url: str) -> int | None:
    try:
        with urllib.request.urlopen(url, timeout=5) as r:  # noqa: S310
            return int(r.status)
    except urllib.error.HTTPError as error:
        return error.code
    except OSError:
        return None


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="benchmark.runner.reread", description=__doc__, allow_abbrev=False)
    p.add_argument("--run", type=Path, required=True)
    p.add_argument("--phase", required=True)
    p.add_argument("--api-url", required=True)
    p.add_argument("--idp-key", type=Path)
    p.add_argument("--idp-binary", type=Path)
    p.add_argument("--idp-kid")
    p.add_argument("--idp-audience")
    p.add_argument("--tenant-id")
    p.add_argument("--readyz-url")
    p.add_argument("--ready-timeout-s", type=float)
    a = p.parse_args(argv)
    if a.phase not in PHASES or not a.run.is_dir() or (a.readyz_url and not a.ready_timeout_s):
        print("refused: a known phase, an existing run directory, and a timeout with --readyz-url")
        return 2
    idp = [a.idp_key, a.idp_binary, a.idp_kid, a.idp_audience, a.tenant_id]
    if not any(idp):
        print("NOT RUN: no IdP session settings (--idp-key, --idp-binary, --idp-kid, --idp-audience, --tenant-id); the runner does not fake an audit session")
        return NOT_RUN
    if not all(idp):
        print("refused: the IdP session provider needs --idp-key, --idp-binary, --idp-kid, --idp-audience and --tenant-id together")
        return 2
    if not a.idp_key.is_file():
        print("refused: --idp-key must be the path of an existing key file, never key material on the command line")
        return 2
    sessions = IdpSessionProvider(a.idp_binary, a.idp_key, kid=a.idp_kid, audience=a.idp_audience, tenant_id=a.tenant_id)
    try:
        if a.readyz_url:
            wait_ready(lambda: _status(a.readyz_url), timeout_s=a.ready_timeout_s, poll_s=2)
        differing = reread(a.run, a.phase, PublicApi(a.api_url), sessions)
    except (ValueError, ReadyTimeout, OSError) as error:
        print(f"failed: {error}")
        return 2
    if differing:
        print(f"the second read differs for: {', '.join(differing)}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
