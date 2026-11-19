"""No bench workflow can select the Terra harness configuration (A362, A374; REQ-AGT-005, AGENT-16).

The frozen harness configuration `benchmark/decisions/harness-gpt-6.1-terra.json` is not selectable from any workflow that starts a bench run. A workflow that names it, or that passes a
harness configuration chosen at dispatch, would let a run pick a model, a rate card and a token ceiling that nobody approved for that workflow. This reads the real workflow files."""
import re
import unittest
from pathlib import Path

WORKFLOWS = Path(__file__).resolve().parents[2] / ".github" / "workflows"
BENCH = sorted(p for p in WORKFLOWS.glob("*.yml") if p.name.startswith("bench") or p.name == "benchmark.yml")


class NoTerraInBenchWorkflows(unittest.TestCase):
    def test_there_are_bench_workflows_to_read(self) -> None:
        self.assertGreaterEqual(len(BENCH), 4, [p.name for p in BENCH])  # a glob that finds nothing proves nothing

    def test_no_bench_workflow_names_the_terra_harness_file_or_model(self) -> None:
        for path in BENCH:
            text = path.read_text(encoding="utf-8")
            self.assertNotRegex(text, r"(?i)harness-gpt-6\.1-terra", path.name)
            self.assertNotRegex(text, r"(?i)gpt-6\.1-terra", path.name)

    def test_no_bench_workflow_takes_a_harness_configuration_from_a_dispatch_input(self) -> None:
        for path in BENCH:
            text = path.read_text(encoding="utf-8")
            self.assertNotRegex(text, r"(?m)^\s+harness(_config)?:\s*$", f"{path.name}: a harness input")
            for line in text.splitlines():
                if "--harness-config" in line:
                    self.assertNotIn("${{", line, f"{path.name}: --harness-config is built from an expression: {line.strip()}")
                    self.assertNotRegex(line, r"--harness-config\s+\"?\$", f"{path.name}: --harness-config is built from a variable: {line.strip()}")

    def test_the_checks_do_find_a_violation(self) -> None:
        # The patterns are not vacuous: they match the text a selectable Terra input would add.
        self.assertRegex("      harness:\n", r"(?m)^\s+harness(_config)?:\s*$")
        self.assertRegex("--harness-config benchmark/decisions/harness-gpt-6.1-terra.json", r"(?i)harness-gpt-6\.1-terra")
        self.assertRegex('--harness-config "benchmark/decisions/harness-$HARNESS.json"', r"--harness-config\s+\"?[^\s]*\$")  # a variable inside the value


if __name__ == "__main__":
    unittest.main()
