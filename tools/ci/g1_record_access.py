"""Checks the core's access log of a G1 stack run (SR's first-dispatch condition 1 on noema#436; REQ-AUD-003; G1 evidence item 3).

Every Record read writes one RECORD_ACCESSED line (IDs and outcome only). Each case of the run is read once with a role the route lists (the control), so its correlation must have exactly one
SERVED line; a SERVED line for any other correlation, a second one, a line that is not JSON, or a line carrying a key beyond the five that name IDs and the outcome is NOT MET. A log with no
RECORD_ACCESSED line, an unreadable log or no correlations is NOT RUN (3), never a pass. Exit 0 met, 1 not met, 3 not run. Names IDs only; prints no log content. SYNTHETIC (AGENT-25)."""
import argparse
import json
import sys
from pathlib import Path

ALLOWED = {"time", "level", "msg", "tenant_id", "principal", "correlation_id", "outcome"}


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--core-log", type=Path, required=True)
    p.add_argument("--correlations", type=Path, required=True, help="correlations.json of the run: case id to correlation id")
    a = p.parse_args(argv)
    try:
        cases = json.loads(a.correlations.read_text(encoding="utf-8"))
        text = a.core_log.read_text(encoding="utf-8", errors="replace")
    except (OSError, ValueError):
        print("NOT RUN: the core log or the correlations file cannot be read")
        return 3
    wanted = set(cases.values()) if isinstance(cases, dict) else set()
    if not wanted:
        print("NOT RUN: the run has no correlation ids")
        return 3
    served: dict[str, int] = {}
    total, problems = 0, []
    for number, raw in enumerate(text.splitlines(), 1):
        if "RECORD_ACCESSED" not in raw:
            continue
        try:
            d = json.loads(raw)
        except ValueError:
            problems.append(f"line {number}: a RECORD_ACCESSED line is not JSON")
            continue
        if not isinstance(d, dict) or d.get("msg") != "RECORD_ACCESSED":
            continue
        total += 1
        extra = sorted(set(d) - ALLOWED)
        if extra:
            problems.append(f"line {number}: a RECORD_ACCESSED line carries {extra}: IDs and the outcome only")
        if d.get("outcome") == "SERVED":
            served[str(d.get("correlation_id"))] = served.get(str(d.get("correlation_id")), 0) + 1
    if total == 0 and not problems:
        print("NOT RUN: no RECORD_ACCESSED line in the core log (a dead collector or an unwired access log reads as zero, never as a pass)")
        return 3
    for corr in sorted(wanted):
        if served.get(corr, 0) != 1:
            problems.append(f"{corr}: {served.get(corr, 0)} SERVED lines, want exactly 1")
    for corr in sorted(set(served) - wanted):
        problems.append(f"{corr}: a SERVED line for a correlation that is not a case of this run")
    for item in problems:
        print("NOT MET: " + item)
    print(f"record accessed: {'MET' if not problems else 'NOT MET'}; {total} lines, {len(wanted)} cases")
    return 0 if not problems else 1


if __name__ == "__main__":
    sys.exit(main())
