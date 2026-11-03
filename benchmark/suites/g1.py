"""The G1 evidence run (BEN-05), pre-staged: two legs of the g1 suite, each from a reset, then the replay diff. Written as testable Python, not shell (Codex review of noema#117).

Every failure is kept. A leg that reports an incomplete run or a Tier 0 gate not at zero (exit 1) does not stop the run, so the replay report is still written, but the
run as a whole fails. A leg that did not run (3) or had bad input (2) stops the run at once: there is nothing to compare. A replay that is not IDENTICAL, or cannot be made, fails it."""
from collections.abc import Callable, Sequence
from pathlib import Path

LEGS = ("a", "b")


def combine(leg_codes: Sequence[int], replay_code: int) -> int:
    """The exit code of the whole run: not-run and bad-input codes first (a leg that did not run), then the replay's, then 1 if any leg reported exit 1."""
    for code in leg_codes:
        if code not in (0, 1):
            return code
    if replay_code != 0:
        return replay_code
    return 1 if any(code == 1 for code in leg_codes) else 0


def run_g1(out: Path, run_leg: Callable[[str, Path], int], run_replay: Callable[[Path, Path, Path], int], reset: Callable[[], int] | None) -> int:
    codes: list[int] = []
    for leg in LEGS:
        if reset is not None:
            reset_code = reset()
            if reset_code != 0:
                return reset_code
        code = run_leg(leg, out / leg)
        codes.append(code)
        if code not in (0, 1):
            return combine(codes, 0)
    replay_code = run_replay(out / "a" / "run", out / "b" / "run", out / "replay")
    return combine(codes, replay_code)
