"""MEM-02 (REQ-MEM-002, REQ-BEN-006; AGENTS.md section 8): the workflow that runs the regression-link check on every pull request. Written before `.github/workflows/regression-link.yml` and seen
failing. It guards the workflow's shape, because a check nobody runs, or one that reads a stale body or no label, protects nothing.

What the workflow must do: re-run when a label or the description changes (a `fix` label added after opening, or a link added to the body, must flip the result); read the LIVE pull request
(body, title, head ref, labels, files) through the API, never the event payload (a re-run replays the payload of the original event) and never by interpolating untrusted text into a script;
turn the ruling-A156 test-file alternative on (A156 is committed, docs#157); hold the least permissions; and have no secret and no `pull_request_target`."""
import os
import re
import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "regression-link.yml"
SCRIPT = ROOT / "tools" / "ci" / "check_regression_link.py"
PINNED = re.compile(r"uses: actions/[a-z-]+@[0-9a-f]{40}")


def text() -> str:
    return WORKFLOW.read_text(encoding="utf-8")


class Shape(unittest.TestCase):
    def test_it_runs_on_every_event_that_can_change_the_answer(self) -> None:
        on = text().split("jobs:")[0]
        self.assertIn("pull_request:", on)
        self.assertNotIn("pull_request_target", text())
        for event in ("opened", "synchronize", "reopened", "edited", "labeled", "unlabeled"):
            self.assertRegex(on, rf"types:\s*\[[^\]]*\b{event}\b", event)

    def test_it_follows_the_workflow_concurrency_convention(self) -> None:
        self.assertIn("group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}-${{ github.event.action == 'edited' && 'metadata' || 'code' }}", text())
        self.assertIn("cancel-in-progress: ${{ github.event_name == 'pull_request' }}", text())

    def test_it_holds_the_least_permissions_and_no_secret(self) -> None:
        t = text()
        self.assertIn("permissions:\n  contents: read\n  pull-requests: read", t)
        self.assertNotIn("secrets.", t)
        self.assertNotIn("write", t.split("jobs:")[0])

    def test_every_action_is_pinned_and_the_checkout_keeps_no_credential(self) -> None:
        t = text()
        uses = re.findall(r"uses: \S+", t)
        self.assertTrue(uses)
        for line in uses:
            self.assertRegex(line, r"@[0-9a-f]{40}", line)
        self.assertIn("persist-credentials: false", t)

    def test_it_reads_the_live_pull_request_not_the_event_payload(self) -> None:
        t = text()
        self.assertIn('gh api "repos/${GITHUB_REPOSITORY}/pulls/${PR_NUMBER}"', t)
        self.assertIn("/files", t)
        self.assertIn("--paginate", t)
        for field in ("body", "title", "head.ref", "labels"):
            self.assertNotIn("github.event.pull_request." + field, t, field)

    def test_no_untrusted_text_is_interpolated_into_a_script(self) -> None:
        """A title, a body, a branch name or a label can hold anything. Only the pull request NUMBER (an integer) may be an expression in a run block; the rest goes through jq into variables."""
        for run in re.findall(r"run: \|\n((?:[ ]{10,}.*\n)+)", text()):
            self.assertNotIn("${{", run)
        self.assertRegex(text(), r"PR_NUMBER: \$\{\{ github\.event\.pull_request\.number \}\}")

    def test_it_turns_the_a156_alternative_on_and_runs_the_script_isolated(self) -> None:
        t = text()
        self.assertIn("REGRESSION_ALLOW_TEST_FILE=1", t)
        self.assertIn("python3 -I tools/ci/check_regression_link.py", t)

    def test_no_condition_skips_the_check_on_a_label_or_description_event(self) -> None:
        self.assertNotIn("if:", text())

    def test_the_safeguards_workflow_runs_the_script_tests_and_these_ones(self) -> None:
        """The 27 script tests merged with noema#452 ran nowhere; they and this file run on every pull request."""
        safeguards = (ROOT / ".github" / "workflows" / "safeguards.yml").read_text(encoding="utf-8")
        self.assertIn("python3 -m unittest tools.ci.test_check_regression_link tools.ci.test_regression_link_workflow", safeguards)

    def test_the_names_the_script_reads_are_the_names_the_workflow_sets(self) -> None:
        script = SCRIPT.read_text(encoding="utf-8")
        t = text()
        for name in ("HEAD_REF", "PR_TITLE", "PR_BODY", "PR_LABELS", "CHANGED_FILES", "REGRESSION_ALLOW_TEST_FILE"):
            self.assertIn(name, script, name)
            self.assertIn(name, t, name)


class WithTheEnvironmentTheWorkflowBuilds(unittest.TestCase):
    """The script, run as the workflow runs it (a clean environment, only these variables, `python3 -I`), for the cases the workflow exists for."""

    def run_check(self, **env: str) -> int:
        base = {"HEAD_REF": "feat/x", "PR_TITLE": "A change", "PR_BODY": "", "PR_LABELS": "", "CHANGED_FILES": "a.py", "REGRESSION_ALLOW_TEST_FILE": "1"}
        base.update(env)
        done = subprocess.run([sys.executable, "-I", str(SCRIPT)], env={**base, "PATH": os.environ.get("PATH", "")}, capture_output=True, text=True, check=False)
        return done.returncode

    def test_a_pull_request_that_is_not_a_fix_passes_without_a_link(self) -> None:
        self.assertEqual(0, self.run_check())

    def test_a_fix_label_without_a_link_fails_even_when_the_branch_and_title_say_nothing(self) -> None:
        self.assertEqual(1, self.run_check(PR_LABELS="bug\nfix"))

    def test_a_fix_label_with_a_case_id_passes(self) -> None:
        self.assertEqual(0, self.run_check(PR_LABELS="fix", PR_BODY="Why.\n\nRegression case: BEN-AMB-0001\n"))

    def test_a_changed_test_file_counts_only_because_the_workflow_turns_the_alternative_on(self) -> None:
        body = "Regression case: tools/ci/test_x.py"
        self.assertEqual(0, self.run_check(PR_LABELS="fix", PR_BODY=body, CHANGED_FILES="tools/ci/test_x.py\nx.py"))
        self.assertEqual(1, self.run_check(PR_LABELS="fix", PR_BODY=body, CHANGED_FILES="tools/ci/test_x.py\nx.py", REGRESSION_ALLOW_TEST_FILE="0"))

    def test_a_link_to_a_test_file_the_pull_request_does_not_change_fails(self) -> None:
        self.assertEqual(1, self.run_check(PR_LABELS="fix", PR_BODY="Regression case: tools/ci/test_x.py", CHANGED_FILES="x.py"))


if __name__ == "__main__":
    unittest.main()
