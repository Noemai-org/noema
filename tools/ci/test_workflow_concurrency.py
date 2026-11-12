"""Every workflow that runs on pull requests cancels its superseded runs, and never cancels a run on main (CI speed, noema).

A push to a pull request branch starts a new run of every PR workflow; without a concurrency group the older runs finish for nothing and queue the runners. The group is per workflow
and per pull request; `cancel-in-progress` is true only for the pull_request event, so a push to main (the merge audit, the post-merge runs) is never cancelled. Read as text so that
the standard library is enough."""
import re
import unittest
from pathlib import Path

WORKFLOWS = Path(__file__).resolve().parents[2] / ".github" / "workflows"
GROUP = "group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
EDITED_SPLIT = "-${{ github.event.action == 'edited' && 'metadata' || 'code' }}"
CANCEL = "cancel-in-progress: ${{ github.event_name == 'pull_request' }}"


def triggers(text: str) -> set[str]:
    m = re.search(r"^on:\n((?:[ ]+.*\n|\n)+)", text, re.M)
    block = m.group(1) if m else ""
    return {k for k in ("pull_request", "push", "workflow_dispatch", "schedule") if re.search(r"^\s+" + k + r"\b", block, re.M)}


class Concurrency(unittest.TestCase):
    def test_every_pull_request_workflow_cancels_superseded_runs_only_on_pull_requests(self) -> None:
        checked = 0
        for path in sorted(WORKFLOWS.glob("*.yml")):
            text = path.read_text()
            if "pull_request" not in triggers(text):
                continue
            checked += 1
            self.assertRegex(text, r"(?m)^concurrency:\n", f"{path.name}: no top-level concurrency")
            self.assertIn(GROUP, text, f"{path.name}: the concurrency group")
            self.assertIn(CANCEL, text, f"{path.name}: cancel-in-progress must be true only for pull_request")
        self.assertGreaterEqual(checked, 15)

    def test_a_metadata_edit_never_shares_a_group_with_the_code_run(self) -> None:
        # Codex P1 on the CI-speed PR: a title or description edit fires `edited`, which skips the real checks; if it shared the group it would cancel the full run
        # of the current commit and nothing would replace it. Workflows that listen to `edited` give those events their own group.
        found = 0
        for path in sorted(WORKFLOWS.glob("*.yml")):
            text = path.read_text()
            if re.search(r"types:\s*\[[^\]]*edited", text):
                found += 1
                self.assertIn(EDITED_SPLIT, text, f"{path.name}: edited events must have their own concurrency group")
        self.assertGreaterEqual(found, 2)

    def test_no_workflow_that_runs_on_push_cancels_unconditionally(self) -> None:
        for path in sorted(WORKFLOWS.glob("*.yml")):
            text = path.read_text()
            if "push" in triggers(text):
                self.assertNotRegex(text, r"cancel-in-progress:\s*true", f"{path.name}: a push to main must never be cancelled")


if __name__ == "__main__":
    unittest.main()
