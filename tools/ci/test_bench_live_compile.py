"""The compile-acceptance workflow (.github/workflows/bench-live-compile.yml; noema#546, ruling A198) spends real money and holds a provider key, so its rules are pinned.
Read as text, like the other workflow guards. The workflow is not run here."""
import re
import unittest
from pathlib import Path

WORKFLOW = Path(__file__).resolve().parents[2] / ".github" / "workflows" / "bench-live-compile.yml"


def job_block(text: str, name: str) -> str:
    m = re.search(r"(?ms)^  " + re.escape(name) + r":\n(.*?)(?=^  [a-z][a-z-]*:\n|\Z)", text)
    assert m, f"no job {name}"
    return m.group(1)


class CompileWorkflow(unittest.TestCase):
    def setUp(self) -> None:
        self.text = WORKFLOW.read_text()
        self.code = "\n".join(line for line in self.text.splitlines() if not line.lstrip().startswith("#"))

    def test_it_is_dispatch_only_with_read_only_permissions(self) -> None:
        on = re.search(r"(?m)^on:\n((?:[ ]+.*\n|\n)+)", self.code)
        assert on
        self.assertRegex(on.group(1), r"(?m)^  workflow_dispatch:")
        for trigger in ("pull_request", "push", "schedule", "workflow_run"):
            self.assertNotRegex(on.group(1), r"(?m)^  " + trigger)
        self.assertRegex(self.code, r"(?m)^permissions:\n  contents: read\n(?!  )")

    def test_the_key_is_in_one_step_of_the_job_that_has_the_environment_and_nowhere_else(self) -> None:
        self.assertEqual(len(re.findall(r"secrets\.", self.code)), 1)
        run = job_block(self.code, "run")
        dry = job_block(self.code, "dry-run")
        self.assertIn("secrets.OPENAI_API_KEY", run)
        self.assertRegex(run, r"(?m)^    environment: bench-live$")
        self.assertNotIn("secrets.", dry)
        self.assertNotIn("environment:", dry, "the dry run must need no approval and hold no secret")
        step = re.search(r"(?ms)^      - id: call\n(.*?)(?=^      - )", run)
        assert step
        self.assertIn("OPENAI_API_KEY", step.group(1))
        self.assertNotRegex(self.code.replace("OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}", ""), r"OPENAI_API_KEY\s*[:=]")
        self.assertNotRegex(self.code, r"echo[^\n]*OPENAI_API_KEY|OPENAI_API_KEY[^\n]*>>")  # never printed or written

    def test_the_run_job_waits_for_the_dry_run_and_only_runs_when_asked(self) -> None:
        run = job_block(self.code, "run")
        self.assertRegex(run, r"(?m)^    needs: dry-run$")
        self.assertRegex(run, r"(?m)^    if: inputs\.mode == 'run'$")

    def test_every_input_reaches_the_shell_through_env_only(self) -> None:
        for line in self.code.splitlines():
            if "${{" in line and "inputs." in line:
                self.assertRegex(line, r"^\s+[A-Z_]+: \$\{\{ inputs\.[a-z_]+ \}\}$", f"an input is expanded outside env: {line.strip()}")

    def test_the_spend_is_bounded_and_the_inputs_are_validated(self) -> None:
        for fragment in ("326693", "grep -Eq '^[A-Za-z0-9_.-]{1,64}$'", "1|2|3|4|5", "openai", "--hard-stop-usd", "--prior-spent-micros"):
            self.assertIn(fragment, self.code, fragment)
        self.assertNotRegex(self.code, r"hard_stop_usd\s*\*|HARD_STOP_USD \* ")  # the runner takes whole dollars; no arithmetic that could widen it here

    def test_the_leak_scan_comes_before_the_upload_and_gates_it(self) -> None:
        self.assertLess(self.code.index("tools/ci/leakscan.py"), self.code.index("actions/upload-artifact"))
        self.assertIn("steps.scan.outputs.scan == '0'", self.code)
        self.assertRegex(self.code, r"leakscan\.py --allow benchmark/compile_acceptance/leakscan\.allow out/compile-acceptance")

    def test_only_the_runners_output_is_uploaded_and_not_a_log(self) -> None:
        uploads = re.findall(r"(?ms)uses: actions/upload-artifact@\S+.*?path: (\S+)", self.code)
        self.assertEqual(uploads, ["out/compile-acceptance/"])

    def test_a_run_that_did_not_call_the_provider_fails_and_the_label_is_stated(self) -> None:
        self.assertIn("NOT RUN", self.code)
        self.assertRegex(self.code, r"3\) echo \"::error::NOT RUN")
        self.assertIn("Understanding: compile acceptance on synthetic golden examples, recorded live output", self.code)
        self.assertNotRegex(self.code, r"continue-on-error")

    def test_actions_are_pinned_by_commit_and_nothing_reads_the_heldout_set(self) -> None:
        for use in re.findall(r"uses: (\S+)", self.code):
            self.assertRegex(use, r"^[\w./-]+@[0-9a-f]{40}$", use)
        self.assertNotIn("heldout", self.code.lower())


if __name__ == "__main__":
    unittest.main()
