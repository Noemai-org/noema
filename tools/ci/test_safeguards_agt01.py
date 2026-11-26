"""AGT-01 (REQ-AGT-003, REQ-AGT-002): the tests of the PR template and its trace check run in CI.

`tools/ci/test_pr_template.py` proves that an unfilled PR template fails the trace check on all three IDs. Nothing ran it: the safeguards workflow ran the trace check on a PR's body and its
canaries, never the template's own tests, so a change that let the template pass unfilled would have merged green. This guard keeps the step in the safeguards workflow, skipped only on a
description edit like its neighbours, and keeps it from being turned into a step that cannot fail."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TEXT = (ROOT / ".github" / "workflows" / "safeguards.yml").read_text(encoding="utf-8")


def step_running(script: str) -> str:
    for m in re.finditer(r"(?ms)^      - name: [^\n]*\n(.*?)(?=^      - |\Z)", TEXT):
        if re.search(r"^\s+run: .*" + re.escape(script), m.group(1), re.M):
            return m.group(0)
    return ""


class Agt01Safeguards(unittest.TestCase):
    def test_the_pr_template_tests_run_in_the_safeguards_workflow(self) -> None:
        step = step_running("tools/ci/test_pr_template.py")
        self.assertTrue(step, "no safeguards step runs tools/ci/test_pr_template.py")
        self.assertIn("if: github.event.action != 'edited'", step)
        self.assertNotIn("continue-on-error", step)
        self.assertNotRegex(step, r"\|\|\s*true")

    def test_this_guard_runs_with_them(self) -> None:
        self.assertTrue(step_running("tools/ci/test_safeguards_agt01.py"), "the guard is not run by CI")

    def test_the_tests_it_names_exist(self) -> None:
        self.assertTrue((ROOT / "tools" / "ci" / "test_pr_template.py").is_file())


if __name__ == "__main__":
    unittest.main()
