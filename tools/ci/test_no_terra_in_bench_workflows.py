"""No bench workflow can select the Terra harness configuration (A362, A374; REQ-AGT-005, AGENT-16).

The frozen harness configuration `benchmark/decisions/harness-gpt-6.1-terra.json` is not selectable from any workflow that starts a bench run. A workflow that names it, or that passes a
harness configuration chosen at dispatch, would let a run pick a model, a rate card and a token ceiling that nobody approved for that workflow. This reads the real workflow files."""
import re
import unittest
from pathlib import Path

WORKFLOWS = Path(__file__).resolve().parents[2] / ".github" / "workflows"
# The one detector, used by the guard and by its self-test (Codex P2 on noema#791): a `--harness-config` whose value holds an expansion ANYWHERE (a shell variable, a command substitution or an
# expression) can select a configuration that no workflow names. A fixed path has no `$` and no backtick in it.
HARNESS_VALUE = re.compile(r"""--harness-config(?:\s+|=)("[^"]*"|'[^']*'|\S+)""")


def harness_config_is_not_a_fixed_path(line: str) -> bool:
    return any(re.search(r"[$`]", m.group(1)) for m in HARNESS_VALUE.finditer(line))
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
                    self.assertFalse(harness_config_is_not_a_fixed_path(line), f"{path.name}: --harness-config is not a fixed path: {line.strip()}")

    def test_the_checks_do_find_a_violation(self) -> None:
        # The patterns are not vacuous: they match the text a selectable Terra input would add.
        self.assertRegex("      harness:\n", r"(?m)^\s+harness(_config)?:\s*$")
        self.assertRegex("--harness-config benchmark/decisions/harness-gpt-6.1-terra.json", r"(?i)harness-gpt-6\.1-terra")
        # The detector is the guard's own, not a broader copy: each of these selects a configuration without naming it.
        for bad in ('--harness-config "benchmark/decisions/harness-$HARNESS.json"', "--harness-config benchmark/decisions/harness-$HARNESS.json", '--harness-config="benchmark/decisions/${H}.json"',
                    "--harness-config 'a/$X.json'", "--harness-config benchmark/decisions/$(cat f).json", "--harness-config benchmark/decisions/`cat f`.json", '--harness-config "${{ inputs.x }}"', "--harness-config $X",
                    '/tmp/venv/bin/python -m x --harness-config "benchmark/decisions/harness-$HARNESS.json" --out y'):
            self.assertTrue(harness_config_is_not_a_fixed_path(bad), bad)
        for good in ("--harness-config benchmark/decisions/harness-gpt-6-luna.json", '--harness-config "benchmark/decisions/harness-gpt-6-luna.json"', "python -m x --out y"):
            self.assertFalse(harness_config_is_not_a_fixed_path(good), good)


if __name__ == "__main__":
    unittest.main()
