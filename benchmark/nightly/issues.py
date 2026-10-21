"""One regression issue per case that was run and not correctly handled, de-duplicated by case ID (T4 item 11; PERD REQ-TST-005, REQ-MEM-002).

A case that could not run (status ERROR) is not a regression: it is a gap in the environment, listed in the run manifest by error code, and filing it nightly would
bury the real failures. A case with no recorded result has nothing to reproduce and is not filed. A case with an open issue is not filed again, whatever the issue's
title says: the case ID in its marker or title identifies it. A closed issue does not count, so a case that fails again after a fix is filed anew."""
import json
import re
import subprocess
from dataclasses import dataclass, field
from typing import Any, Callable, Protocol

Json = dict[str, Any]
LABEL = "benchmark-regression"
_CASE_ID = re.compile(r"BEN-[A-Z]{2,5}-[0-9]{4,6}")
_SEVERITY_ORDER = {"S0": 0, "S1": 1, "S2": 2, "S3": 3}


def marker(case_id: str) -> str:
    return f"<!-- bench-case: {case_id} -->"


@dataclass
class NewIssue:
    case_id: str
    title: str
    body: str
    labels: list[str]


@dataclass
class IssuePlan:
    new: list[NewIssue] = field(default_factory=list)
    already_open: dict[str, int] = field(default_factory=dict)
    could_not_run: dict[str, int] = field(default_factory=dict)


class Gh(Protocol):
    def list_open(self) -> list[Json]: ...
    def ensure_labels(self, labels: list[str]) -> None: ...
    def create(self, title: str, body: str, labels: list[str]) -> int: ...


def _open_cases(open_issues: list[Json]) -> dict[str, int]:
    found: dict[str, int] = {}
    for issue in open_issues:
        text = f"{issue.get('title', '')}\n{issue.get('body', '')}"
        marked = re.findall(r"<!-- bench-case: (" + _CASE_ID.pattern + r") -->", text)
        for case_id in marked or _CASE_ID.findall(str(issue.get("title", ""))):
            found.setdefault(case_id, int(issue["number"]))
    return found


def _body(failure: Json, run_id: str, commit_sha: str | None, run_url: str | None) -> str:
    gates = failure.get("gates") or []
    lines = [marker(failure["case_id"]), f"**Case:** `{failure['case_id']}`  **Severity if failed:** {failure['severity']}  **Run:** `{run_id}`"]
    if commit_sha:
        lines.append(f"**Commit under test:** `{commit_sha}`")
    if run_url:
        lines.append(f"**Run:** {run_url}")
    if gates:
        lines.append("**Tier 0 gates tripped:** " + ", ".join(f"`{g}`" for g in gates))
    lines += ["", "**Why it was not correctly handled:**"] + [f"- {reason}" for reason in failure.get("reasons", [])]
    lines += ["", "**Reproduce:** generate the suite with the same generator version and seed (see the run manifest), then",
              f"`python -m benchmark.runner --suite SUITE --out OUT --only {failure['case_id']} ...`",
              "", "Filed by the nightly benchmark, one issue per case; it is not filed again while this one is open. Generated layer only."]
    return "\n".join(lines)


def plan_issues(scorecard: Json, results: list[Json], open_issues: list[Json], *, run_id: str, commit_sha: str | None, run_url: str | None) -> IssuePlan:
    by_case = {r["case_id"]: r for r in results}
    plan = IssuePlan()
    plan.could_not_run = {}
    for r in results:
        if r["status"] == "ERROR":
            code = (r.get("error") or {}).get("code", "UNKNOWN")
            plan.could_not_run[code] = plan.could_not_run.get(code, 0) + 1
    open_cases = _open_cases(open_issues)
    for failure in sorted(scorecard.get("failures", []), key=lambda f: (_SEVERITY_ORDER.get(f["severity"], 9), f["case_id"])):
        case_id = failure["case_id"]
        result = by_case.get(case_id)
        if result is None or result["status"] != "COMPLETED":
            continue
        if case_id in open_cases:
            plan.already_open[case_id] = open_cases[case_id]
            continue
        labels = [LABEL, f"severity:{failure['severity']}"] + (["tier-0"] if failure.get("gates") else [])
        plan.new.append(NewIssue(case_id, f"[benchmark] {case_id}: not correctly handled ({failure['severity']})",
                                 _body(failure, run_id, commit_sha, run_url), labels))
    return plan


def file_issues(plan: IssuePlan, gh: Gh, *, dry_run: bool) -> dict[str, int]:
    """Creates the planned issues. Returns case ID to issue number; a dry run creates nothing."""
    if dry_run or not plan.new:
        return {}
    gh.ensure_labels(sorted({label for issue in plan.new for label in issue.labels}))
    return {issue.case_id: gh.create(issue.title, issue.body, issue.labels) for issue in plan.new}


def _run_gh(args: list[str]) -> str:
    return subprocess.run(["gh", *args], capture_output=True, text=True, check=True, timeout=120).stdout


class GhCli:
    """The `gh` CLI, authenticated by GH_TOKEN. Inject `runner` to test."""

    def __init__(self, runner: Callable[[list[str]], str] = _run_gh) -> None:
        self._run = runner

    def list_open(self) -> list[Json]:
        out = self._run(["issue", "list", "--label", LABEL, "--state", "open", "--json", "number,title,body", "--limit", "1000"])
        return list(json.loads(out))

    def ensure_labels(self, labels: list[str]) -> None:
        for label in labels:
            try:
                self._run(["label", "create", label, "--force", "--description", "Filed by the nightly benchmark"])
            except subprocess.CalledProcessError:
                pass  # the label exists, or labels cannot be created here: `create` fails loudly if a label is really missing

    def create(self, title: str, body: str, labels: list[str]) -> int:
        out = self._run(["issue", "create", "--title", title, "--body", body, *(a for label in labels for a in ("--label", label))])
        match = re.search(r"/issues/([0-9]+)", out)
        return int(match.group(1)) if match else 0
