#!/usr/bin/env python3
"""PR trace check (PLT-02; AGENTS.md section 11, REQ-AGT-001).

The PR body must carry a task ID, at least one PERD REQ ID and at least one SoT ID.
A body that starts a CONFLICT DETECTED block must fill every field of it (REQ-AGT-003, AGENT-27).
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


# The six fields of the block (PERD REQ-AGT-003), each with the placeholder text of .github/pull_request_template.md.
CONFLICT_PLACEHOLDERS = {
    "Affected IDs": "<SoT IDs, REQ IDs, SOT-C-xx if listed>",
    "Statements in conflict": "<quote A (source, section)> vs <quote B (source, section)>",
    "Evidence": "<code, test output, or document lines>",
    "Proposed alternatives": "<1..n, each with blast radius>",
    "Safer reading applied meanwhile (AGENT-20)": "<which>",
    "Decision required from": "<founders | tech lead | benchmark owner>",
}
MARKER = re.compile(r"^[ \t>*_]*CONFLICT DETECTED[ \t*_]*$")
FENCE = re.compile(r"^\s*(```|~~~)")


def conflict_gaps(body: str) -> list[str]:
    """Gaps in a CONFLICT DETECTED block (REQ-AGT-003, AGENT-27). No marker line means no gap: the check only
    refuses a block that is started and not filled. Prose and a '## ' heading are not markers. A field value runs
    to the next field label or the closing fence, and a template placeholder or an empty value is a gap."""
    lines = body.splitlines()
    start = next((i for i, line in enumerate(lines) if MARKER.match(line)), None)
    if start is None:
        return []
    values: dict[str, list[str]] = {}
    current = None
    for line in lines[start + 1 :]:
        if FENCE.match(line):
            break
        for name in CONFLICT_PLACEHOLDERS:
            label = name.split(" (")[0]
            m = re.match(rf"^\s*{re.escape(label)}(?: \(AGENT-20\))?\s*:(.*)$", line)
            if m:
                current = name
                values.setdefault(name, []).append(m.group(1))
                break
        else:
            if current is not None:
                values[current].append(line)
    gaps = []
    for name, placeholder in CONFLICT_PLACEHOLDERS.items():
        text = " ".join(values.get(name, [])).strip()
        label = name.split(" (")[0]
        if name not in values:
            gaps.append(f"CONFLICT DETECTED block: field '{label}' is missing")
        elif not text or placeholder in text:
            gaps.append(f"CONFLICT DETECTED block: field '{label}' is empty or still the template placeholder")
    return gaps


def main() -> int:
    body = os.environ["PR_BODY"] if "PR_BODY" in os.environ else sys.stdin.read()
    gaps = missing(body)
    if gaps:
        print("pr-trace FAIL: the PR body is missing: " + "; ".join(gaps))
        return 1
    block = conflict_gaps(body)
    if block:
        print("pr-trace FAIL: the CONFLICT DETECTED block is incomplete (PERD REQ-AGT-003): " + "; ".join(block))
        return 1
    print("pr-trace OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
