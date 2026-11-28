"""A770: a push that changes only documentation or status notes skips the heavy jobs (CI throughput; ruling A770 on A455).

`tools/ci/docs_only.py` decides, from the list of changed files, whether the heavy jobs must run. It must be safe by default: anything it cannot classify runs the jobs, a `.md`
file under `contracts/`, `packs/`, `tools/` or `.github/` is code (a checker may read it), and only a pull_request event ever skips. The workflows that use it are checked here too:
every heavy job of arch-lint, compose, contracts and the safeguards images and codegen-diff must wait for the `changed` job and run only when it says so, and the secret scan
(`checks`) is never gated. Usage: python3 -I tools/ci/test_docs_only.py"""
import importlib.util
import os
import re
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("docs_only", ROOT / "tools" / "ci" / "docs_only.py")
docs_only = importlib.util.module_from_spec(spec) if spec else None  # type: ignore[arg-type]
if spec and spec.loader and (ROOT / "tools" / "ci" / "docs_only.py").exists():
    spec.loader.exec_module(docs_only)  # type: ignore[union-attr]
WORKFLOWS = ROOT / ".github" / "workflows"
GATED = {"arch-lint.yml": ["arch-lint"], "compose.yml": ["grantdrill", "compose"], "contracts.yml": ["contracts"], "safeguards.yml": ["images", "codegen-diff"]}


class Classification(unittest.TestCase):
    def test_a_docs_or_status_only_change_skips_the_heavy_jobs(self) -> None:
        for files in (["status/ENV-04.md"], ["docs/README.md", "status/VOI-02.md"], ["README.md"], ["services/core/README.md"], ["status/new-file.txt"]):
            self.assertFalse(docs_only.heavy_needed(files), files)

    def test_any_code_or_workflow_or_checked_markdown_runs_them(self) -> None:
        for files in (["services/core/main.go"], ["status/A.md", "Makefile"], [".github/workflows/core.yml"], ["tools/ci/check_pins.py"], ["contracts/README.md"], ["packs/fintech/README.md"],
                      ["tools/lint/README.md"], [".github/pull_request_template.md"], ["deploy/compose/README.md"], ["benchmark/README.md", "benchmark/x.py"], ["AGENTS.md", "services/model/x.py"]):
            self.assertTrue(docs_only.heavy_needed(files), files)

    def test_nothing_known_runs_them(self) -> None:  # an empty or unreadable list is never a reason to skip
        self.assertTrue(docs_only.heavy_needed([]))
        self.assertTrue(docs_only.heavy_needed([""]))

    def test_only_a_pull_request_ever_skips(self) -> None:
        self.assertTrue(docs_only.decide("push", ["status/A.md"]))
        self.assertTrue(docs_only.decide("workflow_dispatch", ["status/A.md"]))
        self.assertFalse(docs_only.decide("pull_request", ["status/A.md"]))
        self.assertTrue(docs_only.decide("pull_request", ["status/A.md", "services/core/x.go"]))

    def test_a_path_that_climbs_out_or_hides_in_a_name_is_code(self) -> None:
        for files in (["status/../services/core/x.go"], ["docs/x.md.go"], ["notes.md/../Makefile"]):
            self.assertTrue(docs_only.heavy_needed(files), files)


class Workflows(unittest.TestCase):
    def jobs(self, name: str) -> dict[str, str]:
        text = (WORKFLOWS / name).read_text()
        body = text.split("\njobs:\n", 1)[1]
        parts = re.split(r"(?m)^  ([a-z0-9-]+):\n", body)
        return {parts[i]: parts[i + 1] for i in range(1, len(parts) - 1, 2)}

    def test_every_heavy_job_waits_for_the_changed_job_and_runs_only_when_it_says_so(self) -> None:
        for name, heavy in GATED.items():
            jobs = self.jobs(name)
            self.assertIn("changed", jobs, name)
            self.assertIn("docs_only.py", jobs["changed"], name)
            for job in heavy:
                self.assertIn(job, jobs, f"{name}: {job}")
                self.assertRegex(jobs[job], r"(?m)^    needs: changed$", f"{name}: {job}")
                self.assertIn("needs.changed.outputs.run == 'true'", jobs[job], f"{name}: {job}")

    def test_the_secret_scan_and_the_pr_trace_are_never_gated(self) -> None:
        checks = self.jobs("safeguards.yml")["checks"]
        self.assertNotIn("needs:", checks)
        self.assertNotIn("changed", checks)

    def test_the_changed_job_fetches_history_and_passes_the_event_and_the_base(self) -> None:
        for name in GATED:
            job = self.jobs(name)["changed"]
            self.assertIn("fetch-depth: 0", job, name)
            self.assertIn("github.event_name", job, name)
            self.assertIn("github.event.pull_request.base.sha", job, name)
            self.assertIn("persist-credentials: false", job, name)


if __name__ == "__main__":
    unittest.main(argv=sys.argv[:1], verbosity=1)
