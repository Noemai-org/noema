#!/usr/bin/env python3
"""Regression link check (MEM-02; REQ-MEM-002, REQ-BEN-006; AGENTS.md section 8: every bug has its regression case committed before the fix merges).

A fix PR must carry one line `Regression case: <ref>` in its body, where `<ref>` is a benchmark case ID (`BEN-<CATEGORY>-<4 digits>`) or a test file the PR changes
(`path/to/test_x.py`, `path/to/x_test.go` or a `.test.ts` file, optionally with `::name`). "none", "n/a", "TBD" and an empty value are refused. A line inside a code block or a quote
does not count. The format is a proposed default (AGENT-02; [OPEN] with the PR): the PR's files say which tests exist, but not their order, so this check cannot show the test was
committed before the fix; the reviewer reads the commits (AGENTS.md section 8).

Inputs: HEAD_REF, PR_TITLE, PR_BODY and CHANGED_FILES (one path per line) from the environment. Not wired into a workflow here: `.github` is not on this task's path
(AGENTS.md section 11), so adding the job needs a Founder's approval."""
import os
import re
import sys
from collections.abc import Sequence

FIX_BRANCH = re.compile(r"^fix/")
FIX_TITLE = re.compile(r"(?<![A-Za-z])(?:fix|fixes|fixed|fixing)(?![A-Za-z])", re.IGNORECASE)
LINK = re.compile(r"^Regression case:[ \t]*(?P<ref>.*?)[ \t]*$", re.MULTILINE)
CASE_ID = re.compile(r"^BEN-[A-Z]{2,6}-\d{4}$")
TEST_FILE = re.compile(r"(?:^|/)(?:test_[^/]+\.py|[^/]+_test\.py|[^/]+_test\.go|[^/]+\.test\.tsx?)$")
EMPTY = {"", "none", "n/a", "na", "tbd", "todo", "-"}


def is_fix_pr(head_ref: str, title: str) -> bool:
    return bool(FIX_BRANCH.match(head_ref) or FIX_TITLE.search(title))


FENCE_OPEN = re.compile(r"^ {0,3}(?P<fence>`{3,}|~{3,})(?P<info>.*)$")


def _outside_code(body: str) -> str:
    """The body without fenced code blocks and quoted lines: a link written there is an example, not a link.

    Fences follow CommonMark: an opening fence is three or more backticks or tildes after at most three spaces; a backtick fence's info string has no backtick. It closes at a line
    of the same character, at least as long, with nothing after it but spaces. A fence never closed runs to the end of the body. Other fence lines inside a block are code."""
    kept: list[str] = []
    fence: str | None = None
    for line in body.splitlines():
        if fence is not None:
            stripped = line.strip(" ")
            if re.fullmatch(re.escape(fence[0]) + "{" + str(len(fence)) + ",}", stripped) and len(line) - len(line.lstrip(" ")) <= 3:
                fence = None
            continue
        opened = FENCE_OPEN.match(line)
        if opened and not (opened["fence"][0] == "`" and "`" in opened["info"]):
            fence = opened["fence"]
            continue
        if not line.lstrip().startswith(">"):
            kept.append(line)
    return "\n".join(kept)


def check(head_ref: str, title: str, body: str, changed_files: Sequence[str]) -> list[str]:
    if not is_fix_pr(head_ref, title):
        return []
    links = LINK.findall(_outside_code(body))
    if not links:
        return ["a fix PR needs a line 'Regression case: <case ID or test file>' in its body (REQ-MEM-002)"]
    out = []
    for ref in links:
        path = ref.split("::", 1)[0].strip()
        if ref.strip().lower() in EMPTY:
            out.append(f"'Regression case: {ref}' names no case; a fix needs one (REQ-MEM-002)")
        elif CASE_ID.fullmatch(ref.strip()):
            continue
        elif not TEST_FILE.search(path):
            out.append(f"'{path}' is not a benchmark case ID (BEN-<CATEGORY>-<4 digits>) or a test file")
        elif path not in changed_files:
            out.append(f"'{path}' is a test file not changed by this PR: the regression case is added by the fix PR")
    return out


def main() -> int:
    head_ref, title, body = os.environ.get("HEAD_REF", ""), os.environ.get("PR_TITLE", ""), os.environ.get("PR_BODY", "")
    files = [line.strip() for line in os.environ.get("CHANGED_FILES", "").splitlines() if line.strip()]
    problems = check(head_ref, title, body, files)
    for problem in problems:
        print(f"regression-link FAIL: {problem}")
    if not problems:
        print("regression-link OK" if is_fix_pr(head_ref, title) else "regression-link OK (not a fix PR)")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
