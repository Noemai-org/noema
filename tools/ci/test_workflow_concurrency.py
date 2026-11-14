"""Every workflow that runs on pull requests cancels its superseded runs, and no run that is not a pull-request run is ever cancelled (CI speed, noema).

A push to a pull request branch starts a new run of every PR workflow; without a concurrency group the older runs finish for nothing and queue the runners. The group is per workflow
and per pull request number, and `cancel-in-progress` is true only for the pull_request event. A run that is not a pull-request run (a push to main, a dispatch, a schedule) gets a group
that no other run shares, `github.run_id`: GitHub also cancels a *pending* run when a newer one enters the same group, even with `cancel-in-progress: false`, so a shared group would drop
the post-merge checks of the middle of three quick merges (Codex P1 and Security Reviewer on noema#509). The checks read the top-level `concurrency:` block only, not the whole file."""
import re
import unittest
from pathlib import Path

WORKFLOWS = Path(__file__).resolve().parents[2] / ".github" / "workflows"
BASE = "${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}"
EDITED_SPLIT = "-${{ github.event.action == 'edited' && 'metadata' || 'code' }}"
CANCEL = "${{ github.event_name == 'pull_request' }}"
# The five bench workflows (bench-g1-stack is noema#444's) run only on dispatch or schedule, never on pull requests, and keep a deliberate single-flight named group (they spend money or hold the bench stack, so
# one queued run replacing an older queued one is intended).
OWN_GROUP = {"bench-g1-stack.yml", "bench-live.yml", "bench-model-up.yml", "bench-nightly.yml", "bench-suites.yml"}


def triggers(text: str) -> set[str]:
    m = re.search(r"^on:\n((?:[ ]+.*\n|\n)+)", text, re.M)
    block = m.group(1) if m else ""
    return {k for k in ("pull_request", "push", "workflow_dispatch", "schedule") if re.search(r"^\s+" + k + r"\b", block, re.M)}


def concurrency(text: str) -> dict[str, str]:
    """The `group` and `cancel-in-progress` lines of the top-level concurrency block (a comment elsewhere in the file does not count)."""
    m = re.search(r"(?m)^concurrency:\n((?:[ ]+\S.*\n)+)", text)
    out: dict[str, str] = {}
    for line in (m.group(1) if m else "").splitlines():
        key, _, value = line.strip().partition(": ")
        out[key] = value
    return out


class Concurrency(unittest.TestCase):
    def test_every_pull_request_workflow_cancels_superseded_runs_only_on_pull_requests(self) -> None:
        checked = 0
        for path in sorted(WORKFLOWS.glob("*.yml")):
            text = path.read_text()
            if "pull_request" not in triggers(text):
                continue
            checked += 1
            block = concurrency(text)
            self.assertTrue(block, f"{path.name}: no top-level concurrency")
            self.assertIn(block.get("group"), (BASE, BASE + EDITED_SPLIT), f"{path.name}: the concurrency group")
            self.assertEqual(block.get("cancel-in-progress"), CANCEL, f"{path.name}: cancel-in-progress must be true only for pull_request")
        self.assertGreaterEqual(checked, 15)

    def test_a_metadata_edit_never_shares_a_group_with_the_code_run(self) -> None:
        # Codex P1 on the CI-speed PR: a title or description edit fires `edited`, which skips the real checks; if it shared the group it would cancel the full run
        # of the current commit and nothing would replace it. Workflows that listen to `edited` give those events their own group.
        found = 0
        for path in sorted(WORKFLOWS.glob("*.yml")):
            text = path.read_text()
            if re.search(r"types:\s*\[[^\]]*edited", text):
                found += 1
                self.assertEqual(concurrency(text).get("group"), BASE + EDITED_SPLIT, f"{path.name}: edited events must have their own concurrency group")
        self.assertGreaterEqual(found, 2)

    def test_a_run_that_is_not_a_pull_request_run_never_shares_a_group_or_is_cancelled(self) -> None:
        # Pending runs in a shared group are cancelled by GitHub when a newer run enters it, so the group of a push, dispatch or schedule run must be unique to the run.
        for path in sorted(WORKFLOWS.glob("*.yml")):
            text = path.read_text()
            if not triggers(text) & {"push", "workflow_dispatch", "schedule"}:
                continue
            block = concurrency(text)
            if not block or path.name in OWN_GROUP:
                continue  # no group at all: nothing is ever cancelled
            group = block.get("group", "")
            self.assertIn("github.run_id", group, f"{path.name}: the non-pull-request group must be unique per run")
            self.assertNotIn("github.ref", group, f"{path.name}: a shared ref in the group cancels pending runs on main")
            self.assertNotRegex(block.get("cancel-in-progress", ""), r"^true$", f"{path.name}: a push to main must never be cancelled")


if __name__ == "__main__":
    unittest.main()