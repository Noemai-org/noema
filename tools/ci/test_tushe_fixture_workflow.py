"""The fixture-loader workflow (ENV-16; DEC-2026-11-04 R12 and R17): it runs from main only, behind the `tushe-fixture` Environment (the Founder is its required reviewer), reads no secret
outside that Environment, records each run, and never reports a pass for a load that was not made. Read like the other workflow guards; the workflow is not run here.
Usage: python3 -I tools/ci/test_tushe_fixture_workflow.py"""
import re
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "tushe-fixture-load.yml"
RUNTIME_FILES = [ROOT / "deploy" / "compose" / "docker-compose.yml", ROOT / "deploy" / "compose" / "bench-stack.yml"] + sorted((ROOT / "deploy" / "compose" / "services").glob("*.yml"))


class FixtureLoaderWorkflow(unittest.TestCase):
    def setUp(self) -> None:
        self.text = WORKFLOW.read_text()
        self.script = "\n".join(line for line in self.text.splitlines() if not line.lstrip().startswith("#"))

    def test_it_runs_only_by_hand_from_main_and_the_job_refuses_any_other_ref(self) -> None:  # R12
        self.assertRegex(self.script, r"(?m)^on:\n  workflow_dispatch:")
        self.assertNotRegex(self.script, r"(?m)^  (push|pull_request|pull_request_target|schedule|workflow_run):")
        self.assertRegex(self.script, r"if: github\.ref == 'refs/heads/main'")

    def test_the_job_waits_in_the_tushe_fixture_environment_for_the_founder(self) -> None:  # R12 (l), R17
        self.assertRegex(self.script, r"(?m)^    environment: tushe-fixture$")

    def test_no_secret_is_read_here_so_none_is_read_outside_the_environment(self) -> None:  # R17: the load step's secret names wait for the endpoint (status/ENV-16.md)
        self.assertNotRegex(self.script, r"\bsecrets\b")
        self.assertNotRegex(self.script, r"\bvars\.")

    def test_permissions_are_read_only_and_the_checkout_keeps_no_token(self) -> None:
        self.assertRegex(self.script, r"(?m)^permissions:\n  contents: read\n")
        self.assertRegex(self.script, r"persist-credentials: false")
        self.assertNotRegex(self.script, r"(?m)^\s+(contents|actions|id-token|packages): write")

    def test_the_inputs_are_checked_on_the_whole_string_and_the_run_is_recorded(self) -> None:
        self.assertRegex(self.script, r"case \"\$SCENARIO_KEY\" in ''\|\*\[!A-Za-z0-9_.-\]\*\)")
        self.assertRegex(self.script, r"case \"\$SEED\" in ''\|\*\[!0-9\]\*\)")
        self.assertRegex(self.script, r"GITHUB_STEP_SUMMARY")
        for field in ("GITHUB_SHA", "GITHUB_RUN_ID", "GITHUB_ACTOR"):
            self.assertIn(field, self.script)

    def test_a_load_that_was_not_made_is_never_a_pass(self) -> None:  # the endpoint is not in the documents; the step exits 3 and says so
        self.assertRegex(self.script, r"NOT RUN: .*endpoint")
        self.assertRegex(self.script, r"exit 3")

    def test_no_runtime_compose_file_names_the_environment_the_workflow_or_a_tushe_fixture_setting(self) -> None:  # unreachable from the runtime profiles
        for path in RUNTIME_FILES:
            text = path.read_text()
            self.assertNotIn("tushe-fixture", text, str(path))
            self.assertNotRegex(text, r"TUSHE_FIXTURE", str(path))


if __name__ == "__main__":
    unittest.main(argv=sys.argv[:1], verbosity=1)
