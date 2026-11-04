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


LABELS = ("stand-in", "tushe")


def evidence_record(label: str, run_id: str, code: int, report: dict | None) -> dict:  # type: ignore[type-arg]
    """The G1 evidence record, labelled with where its data came from (Founder, 4 Nov 2026). Only a clean run (exit 0) whose replay is IDENTICAL is evidence; a run that did not run,
    a failed leg or a replay that is not identical is not, whatever the label. Stand-in evidence says so in its headline and must be rerun against live Tushe."""
    if label not in LABELS:
        raise ValueError(f"data source must be one of {LABELS}, not {label!r}")
    stand_in = label == "stand-in"
    verdict = report["verdict"] if report else None
    evidence = bool(report) and code == 0 and verdict == "IDENTICAL"
    if report is None:
        headline = f"NOT RUN: no evidence ({label} data). Exit code {code}."
    else:
        cov = report["case_coverage"]
        scope = f"on {cov['covered']} of {cov['of']} cases" + (f" ({len(report['open_exclusions'])} open exclusion{'' if len(report['open_exclusions']) == 1 else 's'})" if not report["full_coverage"] else "")
        prefix = "G1 evidence (STAND-IN data, not Tushe)" if stand_in else "G1 evidence (Tushe data)"
        headline = f"{prefix}: {verdict} {scope}" if evidence else f"NOT EVIDENCE ({label} data): replay {verdict} {scope}; exit code {code}"
    return {
        "data_source": label, "headline": headline, "is_evidence": evidence, "rerun_against_tushe_required": stand_in, "exit_code": code, "replay_verdict": verdict,
        "full_coverage": report["full_coverage"] if report else None, "run_ids": [report["first"]["run_id"], report["second"]["run_id"]] if report else [f"{run_id}-a", f"{run_id}-b"],
        "commit_sha": report["commit_sha"] if report else None, "seed": report["seed"] if report else None,
        "open_exclusions": [e["case_id"] for e in report["open_exclusions"]] if report else [],
    }
