#!/usr/bin/env python3
"""PR trace check (PLT-02; AGENTS.md section 11, REQ-AGT-001).

The PR body must carry a task ID, at least one PERD REQ ID and at least one SoT ID.
Body is read from the PR_BODY environment variable, or from stdin when unset.
"""
import os
import re
import sys

TASK = re.compile(
    r"\b(?:AGT|AMB|API|ARM|BEN|BIZ|CAP|CON|DATA|DEM|EDG|ENV|EVD|EXE|EXP|GOV|INP|MEM|OBS|OPS|OUT|PCK|PLN|PLT|PRE"
    r"|RCV|REC|REL|RES|SEC|STM|TRU|TST|UI|UND|VOI)-\d{2}\b"
)
REQ = re.compile(r"\bREQ-[A-Z]+-\d{3}\b")
SOT = re.compile(r"\b(?:INV|BD|PLAT)-\d+\b|\bPOC-FR-\d+\b|§\s?\d")


def missing(body: str) -> list[str]:
    out = []
    if not TASK.search(body):
        out.append("task ID (for example PLT-02)")
    if not REQ.search(body):
        out.append("PERD REQ ID (for example REQ-OPS-001)")
    if not SOT.search(body):
        out.append("SoT ID (INV-, BD-, PLAT-, POC-FR- or a § section)")
    return out


def main() -> int:
    body = os.environ["PR_BODY"] if "PR_BODY" in os.environ else sys.stdin.read()
    gaps = missing(body)
    if gaps:
        print("pr-trace FAIL: the PR body is missing: " + "; ".join(gaps))
        return 1
    print("pr-trace OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
