"""TST-02 (REQ-TST-005; ruling A776): the 30-case smoke suite has a make target, `bench-smoke-30`, and a dispatch-only entry in the bench-suites workflow. It becomes a per-merge job only
after the first /readyz 200 (A673, A776); until then nothing runs it on a merge, and no workflow trigger other than workflow_dispatch may start it.
The target is the suite recipe with the suite `smoke`: reset, the bench stack, generate, select, run, score, record, cost directory passed. `make bench-smoke` is a different, older script and
is left alone. Usage: python3 -I -m unittest benchmark.suites.test_smoke_30_target"""
import json
import re
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class Smoke30(unittest.TestCase):
    def recipe(self) -> str:
        text = (ROOT / "Makefile").read_text(encoding="utf-8")
        start = text.index("\nbench-smoke-30:")
        end = text.find("\n\n", start + 1)
        return text[start:end if end != -1 else None]

    def test_the_target_runs_the_smoke_suite_the_way_the_other_suites_run(self) -> None:
        recipe = self.recipe()
        self.assertRegex(recipe, r"suite=smoke;")
        self.assertIn("$(MAKE) reset", recipe)
        self.assertLess(recipe.index("$(MAKE) reset"), recipe.index("$(MAKE) $(BENCH_UP)"))
        self.assertIn("--cost-dir $(BENCH_OUT)/cost", recipe)
        self.assertIn("-m benchmark.suites run $$suite", recipe)
        self.assertNotIn("heldout", recipe)

    def test_the_suite_it_names_exists_and_selects_at_least_thirty_cases(self) -> None:
        suites = json.loads((ROOT / "benchmark" / "suites" / "suites.json").read_text(encoding="utf-8"))["suites"]
        self.assertIn("smoke", suites)
        self.assertGreaterEqual(suites["smoke"]["min_cases"], 30)

    def test_make_knows_the_target_and_it_is_not_the_old_smoke_script(self) -> None:
        # `make -pq` reads the Makefile and prints its rule database without running a recipe (a dry run would still run the line that calls $(MAKE)).
        out = subprocess.run(["make", "-pq", "bench-smoke-30"], cwd=ROOT, capture_output=True, text=True)
        self.assertRegex(out.stdout, r"(?m)^bench-smoke-30: bench-venv$")
        self.assertIn("benchmark.suites run", out.stdout)
        self.assertNotIn("benchmark/smoke.py", out.stdout.split("bench-smoke-30:", 1)[1].split("\n\n", 1)[0])

    def test_the_workflow_offers_it_to_a_hand_dispatch_and_nothing_else_starts_it(self) -> None:
        text = (ROOT / ".github" / "workflows" / "bench-suites.yml").read_text(encoding="utf-8")
        options = re.search(r"options: \[([^\]]*)\]", text)
        assert options
        self.assertIn("smoke-30", [o.strip() for o in options.group(1).split(",")])
        on = re.search(r"(?ms)^on:\n(.*?)^\S", text + "\nx").group(1)  # type: ignore[union-attr]
        for trigger in ("push", "pull_request", "schedule", "workflow_run"):
            self.assertNotRegex(on, rf"(?m)^  {trigger}:", trigger)
        self.assertRegex(on, r"(?m)^  workflow_dispatch:")
        self.assertIn('make "bench-$SUITE"', text)  # the suite name reaches make as bench-smoke-30


if __name__ == "__main__":
    unittest.main()
