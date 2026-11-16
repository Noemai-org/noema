"""Capture of mock-case's log for one G1 leg, passed to the runner as `--mock-case-log` (REQ-ENV-002).

The runner counts oracle-access refusals by reading this file incrementally and fails closed if it shrinks or cannot be read, so the file must exist before the
runner marks it and must be streamed for the whole leg. A capture that cannot start, or that dies mid-leg, makes the log incomplete: the leg is NOT RUN, never a clean zero.
Assumes the compose project is the one `make model-bench-up` started, with the bench profile (the stack's reachability is the Integration Lead's, noema#436)."""
import contextlib
import subprocess
import time
from collections.abc import Callable, Iterator, Sequence
from pathlib import Path
from typing import Any

COMPOSE_FILE = "deploy/compose/docker-compose.yml"
SETTLE_SECONDS = 1.0


class CaptureFailed(Exception):
    pass


def capture_command() -> list[str]:
    return ["docker", "compose", "-f", COMPOSE_FILE, "--profile", "bench", "logs", "--follow", "--no-log-prefix", "--no-color", "mock-case"]


def supplies_log(passthrough: Sequence[str]) -> bool:
    return any(a == "--mock-case-log" or a.startswith("--mock-case-log=") for a in passthrough)


def with_log_argument(passthrough: Sequence[str], path: Path) -> list[str]:
    """The caller's own `--mock-case-log` wins; otherwise the captured file is passed."""
    if supplies_log(passthrough):
        return list(passthrough)
    return ["--mock-case-log", str(path), *passthrough]


@contextlib.contextmanager
def mock_case_log(path: Path, *, popen: Callable[..., Any] = subprocess.Popen, sleep: Callable[[float], None] = time.sleep) -> Iterator[Path]:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"")
    with path.open("ab") as sink:
        try:
            proc = popen(capture_command(), stdout=sink, stderr=subprocess.DEVNULL)
        except OSError as error:
            raise CaptureFailed(f"the mock-case log cannot be captured: {error}") from None
        try:
            sleep(SETTLE_SECONDS)
            if proc.poll() is not None:
                raise CaptureFailed("the mock-case log capture exited at start (is the bench stack up?): the refusal counter would have no source")
            yield path
            if proc.poll() is not None:
                raise CaptureFailed("the mock-case log capture died during the leg: the log is incomplete, so its refusal count would be too low")
        finally:
            proc.terminate()
            with contextlib.suppress(Exception):
                proc.wait(timeout=5)
