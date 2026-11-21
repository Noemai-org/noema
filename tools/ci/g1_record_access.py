"""Checks the core's access log of a G1 stack run (SR's first-dispatch condition 1 on noema#436; REQ-AUD-003; G1 evidence item 3).

Every Record read writes one RECORD_ACCESSED line (IDs and outcome only). The expected number of SERVED lines is DERIVED from the run's own per-request record (A354), never a constant: for each
correlation, the number of requests of kind "reconstruction" that were answered 200. A SERVED line for any other correlation, a different count, a line that is not JSON, or a line carrying a key
beyond the five that name IDs and the outcome is NOT MET. The request record (`requests.jsonl`, written by the runner: one object per HTTP call it makes, with exactly case_id, correlation_id, method,
path, status and kind in case, reconstruction or probe; no header, body or token) that is missing, unreadable, malformed or empty is NOT RUN or NOT MET, never a pass. A log with no
RECORD_ACCESSED line, an unreadable log or no correlations is NOT RUN (3), never a pass. Exit 0 met, 1 not met, 3 not run. Names IDs only; prints no log content. SYNTHETIC (AGENT-25)."""
import argparse
import json
import sys
from pathlib import Path

ALLOWED = {"time", "level", "msg", "tenant_id", "principal", "correlation_id", "outcome"}
REQUEST_FIELDS = {"case_id", "correlation_id", "method", "path", "status", "kind"}
KINDS = {"case", "reconstruction", "probe"}


def expected_counts(text: str, wanted: set[str]) -> tuple[dict[str, int], list[str]]:
    """The expected SERVED count per correlation from the runner's request record, and what is wrong with the record. Names fields, never values."""
    expect: dict[str, int] = {}
    problems: list[str] = []
    for number, raw in enumerate(text.splitlines(), 1):
        if not raw.strip():
            continue
        try:
            d = json.loads(raw)
        except ValueError:
            problems.append(f"requests line {number}: not JSON")
            continue
        if not isinstance(d, dict) or set(d) != REQUEST_FIELDS:
            extra = sorted(set(d) - REQUEST_FIELDS) if isinstance(d, dict) else ["(not an object)"]
            problems.append(f"requests line {number}: the fields are not exactly {sorted(REQUEST_FIELDS)} (extra: {extra}): no header, body or token belongs in the record")
            continue
        status, kind, corr = d["status"], d["kind"], d["correlation_id"]
        if isinstance(status, bool) or not isinstance(status, int) or kind not in KINDS or not isinstance(corr, str):
            problems.append(f"requests line {number}: a status that is not an integer, a kind outside {sorted(KINDS)} or a correlation that is not a string")
            continue
        if corr not in wanted:
            if kind == "probe":
                continue  # the evidence's unknown-correlation probe names a correlation that does not exist on purpose; it adds nothing to the expected count (Codex P2 on noema#730)
            problems.append(f"requests line {number}: a correlation that is not a case of this run")
            continue
        if kind == "reconstruction" and status == 200:
            expect[corr] = expect.get(corr, 0) + 1
    return expect, problems


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--core-log", type=Path, required=True)
    p.add_argument("--correlations", type=Path, required=True, help="correlations.json of the run: case id to correlation id")
    p.add_argument("--requests", type=Path, required=True, help="requests.jsonl of the run: the runner's own record of every HTTP call it made (the expected count is derived from it)")
    a = p.parse_args(argv)
    try:
        cases = json.loads(a.correlations.read_text(encoding="utf-8"))
        text = a.core_log.read_text(encoding="utf-8", errors="replace")
    except (OSError, ValueError):
        print("NOT RUN: the core log or the correlations file cannot be read")
        return 3
    try:
        request_text = a.requests.read_text(encoding="utf-8")
    except (OSError, ValueError):
        print("NOT RUN: the run's request record cannot be read: the expected count has no source")
        return 3
    wanted = set(cases.values()) if isinstance(cases, dict) else set()
    if not wanted:
        print("NOT RUN: the run has no correlation ids")
        return 3
    expect, problems = expected_counts(request_text, wanted)
    if not expect and not problems:
        print("NOT RUN: the request record holds no answered reconstruction request, so nothing is expected and nothing can be shown (never a pass)")
        return 3
    served: dict[str, int] = {}
    total = 0
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
        if served.get(corr, 0) != expect.get(corr, 0):
            problems.append(f"{corr}: {served.get(corr, 0)} SERVED lines, the request record says {expect.get(corr, 0)} answered reconstruction requests")
    for corr in sorted(set(served) - wanted):
        problems.append(f"{corr}: a SERVED line for a correlation that is not a case of this run")
    for item in problems:
        print("NOT MET: " + item)
    print(f"record accessed: {'MET' if not problems else 'NOT MET'}; {total} lines, {len(wanted)} cases")
    return 0 if not problems else 1


if __name__ == "__main__":
    sys.exit(main())
