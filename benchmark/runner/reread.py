"""BEN-05 / A476: add the `after_recovery` or `after_replay` phase to a run's `reconstructions.json`.

`after_recovery` is read after a core container restart (the workflow's step) and a wait for /readyz 200, through the audit-only route (noema#783) as an audit principal. `after_replay` is a SECOND READ of
the same route: it is a re-read, not a replay, and its bytes must equal the first (INV-21 is never claimed from this). Read before the stamp is written. SYNTHETIC."""
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
from .env import SessionProvider
from .evidence import ALLOWED_ROLE, probe_principal

Json = dict[str, Any]
PHASES = ("after_recovery", "after_replay")
BEFORE = {"after_recovery": "initial", "after_replay": "after_recovery"}


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
    """Read every case's reconstruction as an audit principal and add `phase`. Returns the cases whose bytes differ from the phase before (after_replay only). Writes nothing on an error."""
    if phase not in PHASES:
        raise ValueError(f"unknown phase {phase!r}")
    try:
        doc = json.loads((run / "reconstructions.json").read_text(encoding="utf-8"))
        correlations = json.loads((run / "correlations.json").read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise ValueError(f"unreadable run files: {error}") from error
    if not isinstance(doc, dict) or not isinstance(correlations, dict) or "stamp" in doc:
        raise ValueError("reconstructions.json must be the unstamped {case: {phase: doc}} map")
    previous = BEFORE[phase]
    for case, phases in doc.items():
        if not isinstance(phases, dict) or previous not in phases or phase in phases:
            raise ValueError(f"{case}: needs {previous} and not {phase}")
        if case not in correlations:
            raise ValueError(f"{case}: no correlation id")
    read = {case: api.reconstruction(sessions.token_for(probe_principal(), [ALLOWED_ROLE]), correlations[case]) for case in doc}
    differing = [c for c in doc if phase == "after_replay" and _canon(read[c]) != _canon(doc[c][previous])]
    for case in doc:
        doc[case][phase] = read[case]
    (run / "reconstructions.json").write_text(json.dumps(doc, indent=2, sort_keys=False) + "\n", encoding="utf-8")
    return differing


def _status(url: str) -> int | None:
    try:
        with urllib.request.urlopen(url, timeout=5) as r:  # noqa: S310
            return int(r.status)
    except urllib.error.HTTPError as error:
        return error.code
    except OSError:
        return None


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="benchmark.runner.reread", description=__doc__)
    p.add_argument("--run", type=Path, required=True)
    p.add_argument("--phase", required=True)
    p.add_argument("--api-url", required=True)
    p.add_argument("--tokens-dir", type=Path)
    p.add_argument("--idp-key")
    p.add_argument("--readyz-url")
    p.add_argument("--ready-timeout-s", type=float)
    a = p.parse_args(argv)
    if a.phase not in PHASES or not a.run.is_dir() or bool(a.tokens_dir) == bool(a.idp_key) or (a.readyz_url and not a.ready_timeout_s):
        print("refused: a known phase, an existing run directory, exactly one session source, and a timeout with --readyz-url")
        return 2
    if a.idp_key:
        print("refused: the IdP session source is not wired here; use --tokens-dir")
        return 2
    from .env import DirSessionProvider
    try:
        if a.readyz_url:
            wait_ready(lambda: _status(a.readyz_url), timeout_s=a.ready_timeout_s, poll_s=2)
        differing = reread(a.run, a.phase, PublicApi(a.api_url), DirSessionProvider(a.tokens_dir))
    except (ValueError, ReadyTimeout, OSError) as error:
        print(f"failed: {error}")
        return 2
    if differing:
        print(f"the second read differs for: {', '.join(differing)}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
