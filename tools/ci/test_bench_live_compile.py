"""The compile-acceptance workflow (.github/workflows/bench-live-compile.yml; noema#546, ruling A198) spends real money and holds a provider key, so its rules are pinned.
Read as text, like the other workflow guards. The workflow is not run here."""
import os
import re
import shutil
import subprocess
import tempfile
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
        for fragment in ("326693", "[[ \"$run_id\" =~ ^[A-Za-z0-9_.-]{1,64}$ ]]", "1|2|3|4|5", "openai", "--hard-stop-usd", "--prior-spent-micros"):
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

    def test_the_dry_run_fails_when_its_runner_fails(self) -> None:
        # Codex P2 on noema#569: `cmd | tee file` is a successful step under `set -eu` when cmd fails, so the paid job could proceed without a cost. pipefail makes the pipeline fail.
        dry = job_block(self.code, "dry-run")
        self.assertIn("set -euo pipefail", dry)
        self.assertNotRegex(dry, r"(?m)^\s+set -eu$")

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_the_dry_run_accepts_exit_3_and_no_other_code(self) -> None:
        # A285 and Codex P2 on noema#654: the runner's `--dry-run` calls no provider, so it exits 3 (NOT RUN). The job treats exactly that as "dry run complete"; 0, 1 and 2 fail it,
        # so a changed runner cannot turn the job green by accident. The real text of the call and the check runs here with the runner replaced by a stub that exits N.
        block = job_block(self.text, "dry-run")
        step = re.search(r"(?ms)^        run: \|\n((?:(?!^      - ).*\n)*?          code=0\n.*?)(?=^          \{ echo)", block)
        assert step, "the dry-run step has no `code=0` guard"
        lines = [line[10:] for line in step.group(1).splitlines()]
        start = next(i for i, line in enumerate(lines) if line == "code=0")
        text = "\n".join(lines[start:])
        self.assertEqual(len(re.findall(r"\| tee out/dry-run.txt", text)), 1)
        for n, ok in ((0, False), (1, False), (2, False), (3, True), (4, False)):
            stub = re.sub(r"(?s)/tmp/venv/bin/python -m benchmark\.compile_acceptance .*?(?= \| tee out/dry-run.txt)", f"(echo dry-run-output; exit {n})", text)
            with self.subTest(runner_exit=n):
                self.assertNotEqual(stub, text, "the runner call was not replaced")
                with tempfile.TemporaryDirectory() as tmp:
                    r = subprocess.run(["bash", "-c", "set -euo pipefail\nmkdir -p out\n" + stub + "\necho STEP_PASSED"], cwd=tmp, capture_output=True, text=True, check=False)
                    self.assertEqual(r.returncode == 0 and "STEP_PASSED" in r.stdout, ok, (r.returncode, r.stdout, r.stderr))
                    self.assertIn("dry-run-output", Path(tmp, "out", "dry-run.txt").read_text())

    def test_the_manifest_reaches_the_step_summary_only_after_a_clean_scan(self) -> None:
        # Codex P1 and the Security Reviewer's blocking 2 on noema#569: the summary is public like the artifact.
        summary = re.search(r"(?ms)- name: Summary.*?(?=^      - name:)", self.code)
        assert summary
        cats = [line for line in summary.group(0).splitlines() if "manifest.json" in line and "cat " in line]
        self.assertEqual(len(cats), 1)
        self.assertIn('[ "$SCAN" = 0 ]', cats[0])

    def test_the_checkouts_do_not_keep_the_repository_token(self) -> None:
        # The run job holds the provider key and runs repository code: no stored token in .git/config (Security Reviewer's should-fix on noema#569).
        checkouts = re.findall(r"uses: actions/checkout@\S+[^\n]*\n\s+with:\n\s+persist-credentials: false", self.code)
        self.assertEqual(len(checkouts), len(re.findall(r"uses: actions/checkout@", self.code)))
        self.assertEqual(len(checkouts), 2)

    GOOD = {"PROVIDER": "openai", "PACK_REF": "fin@1.0.0", "PRIOR": "326693", "HARD_STOP_USD": "5", "GIVEN_RUN_ID": "", "GITHUB_RUN": "42"}

    def validation(self, job: str, stop: str) -> str:
        """The workflow's own input checks: the step's script from its first line up to the line that starts the call (`stop`), so the tests run the real text, not a copy."""
        block = job_block(self.text, job)
        steps = [m for m in re.findall(r"(?ms)^        run: \|\n(.*?)(?=^      - |\Z)", block) if "run_id=" in m]
        assert len(steps) == 1
        lines = [line[10:] for line in steps[0].splitlines()]
        return "\n".join(lines[: next(i for i, line in enumerate(lines) if line.startswith(stop))])

    def run_bash(self, script: str, **env: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(["bash", "-c", script], env={"PATH": os.environ.get("PATH", ""), **self.GOOD, **env}, capture_output=True, text=True, check=False)

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's validation text")
    def test_the_dry_run_checks_every_input_on_the_whole_string(self) -> None:
        # noema#569 (the flaw of noema#577): grep matches a line at a time, so a value with a newline passed. The real validation text of the dry-run job runs here.
        script = self.validation("dry-run", "mkdir -p out")
        self.assertEqual(self.run_bash(script).returncode, 0)
        for env in ({"GIVEN_RUN_ID": "g1.run-2_x"}, {"GIVEN_RUN_ID": "a" * 64}):
            self.assertEqual(self.run_bash(script, **env).returncode, 0, env)
        bad = [{"GIVEN_RUN_ID": v} for v in ("ok\nINJECT=1", "ok\n", "\nok", "a b", "a;b", "x/y", "a" * 65)]
        bad += [{"PACK_REF": v} for v in ("fin@1.0.0\nx", "fin@1.0.0\n", "Fin@1.0.0", "fin@1.0", "fin@1.0.0 ")]
        bad += [{"PRIOR": v} for v in ("326693\nx", "326692", "", "-1", "1e6")] + [{"HARD_STOP_USD": v} for v in ("5\nx", "6", "0", "", "05")] + [{"PROVIDER": v} for v in ("openai\nx", "gemini")]
        for env in bad:
            self.assertEqual(self.run_bash(script, **env).returncode, 2, env)

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's validation text")
    def test_the_run_job_checks_the_same_way_and_never_writes_a_bad_run_id_to_the_output(self) -> None:
        script = self.validation("run", "code=2") + '\ncode=2\nif [ "$valid" = 1 ]; then echo VALID; else run_id=invalid; fi\necho "run_id=$run_id"\n'
        good = self.run_bash(script)
        self.assertEqual((good.returncode, good.stdout.split()), (0, ["VALID", "run_id=compile-gh42"]), good.stderr)
        bad = [{"GIVEN_RUN_ID": v} for v in ("ok\nINJECT=1", "ok\n", "a b", "a" * 65)]
        bad += [{"PACK_REF": v} for v in ("fin@1.0.0\nx", "fin@1.0")] + [{"PRIOR": v} for v in ("326693\nx", "326692", "", "1" * 16)] + [{"HARD_STOP_USD": v} for v in ("5\nx", "6", "")]
        bad += [{"PROVIDER": v} for v in ("openai\nx", "gemini")]
        for env in bad:
            r = self.run_bash(script, **env)
            self.assertEqual(r.stdout.split(), ["run_id=invalid"], (env, r.stdout, r.stderr))
        self.assertRegex(job_block(self.code, "run"), r"(?s)run_id=invalid.*?echo \"run_id=\$run_id\" >> \"\$GITHUB_OUTPUT\"")
        self.assertNotIn("grep -Eq", self.code)

    def step_script(self, prefix: str) -> str:
        """The run script of one step of the run job, de-indented: the real text, so the tests run it and not a copy."""
        block = job_block(self.text, "run")
        step = re.search(r"(?ms)^      - (?:id: \w+\n        )?name: " + re.escape(prefix) + r"[^\n]*\n(.*?)(?=^      - |\Z)", block)
        assert step, prefix
        body = re.search(r"(?ms)^        run: \|\n(.*)\Z", step.group(1))
        assert body, prefix
        return "\n".join(line[10:] for line in body.group(1).splitlines())

    def run_step(self, prefix: str, summary_text: str | None, manifest: bool = False, **env: str) -> tuple[subprocess.CompletedProcess[str], str]:
        script = self.step_script(prefix)
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp, "out", "compile-acceptance")
            (out / "recordings").mkdir(parents=True)
            if summary_text is not None:
                (out / "summary.md").write_text(summary_text, encoding="utf-8")
            if manifest:
                (out / "recordings" / "manifest.json").write_text('{"calls": 1}', encoding="utf-8")
            step_summary = Path(tmp, "step-summary.md")
            r = subprocess.run(["bash", "-c", script], cwd=tmp, capture_output=True, text=True, check=False,
                               env={"PATH": os.environ["PATH"], "GITHUB_STEP_SUMMARY": str(step_summary), "RUN_ID": "compile-gh1", **env})
            return r, step_summary.read_text(encoding="utf-8") if step_summary.exists() else ""

    TABLE = "FAILING: G-A G-C G-D G-B G-E\n\n| case | status | problems |\n| G-A | MISSED | requested_remedy: present where none is expected |\n"

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_the_job_summary_shows_the_failing_goldens_after_a_clean_scan_only(self) -> None:
        # UND-16, run #6: the failing goldens were only in the log. The summary shows the runner's own `summary.md` (ids, statuses, fixed texts), as it shows the manifest: only after a clean scan.
        clean, shown = self.run_step("Summary", self.TABLE, CODE="1", SCAN="0")
        self.assertEqual(clean.returncode, 0, clean.stderr)
        self.assertIn("FAILING: G-A G-C G-D G-B G-E", shown)
        self.assertIn("| G-A | MISSED |", shown)
        dirty, hidden = self.run_step("Summary", self.TABLE, CODE="1", SCAN="1")
        self.assertEqual(dirty.returncode, 0, dirty.stderr)
        self.assertNotIn("G-A", hidden)
        missing, _ = self.run_step("Summary", None, CODE="2", SCAN="0")
        self.assertEqual(missing.returncode, 0, missing.stderr)  # a run with no summary (bad input, no key) still gets its summary

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_the_result_step_names_the_failing_goldens_in_its_error(self) -> None:
        failed, _ = self.run_step("Result", self.TABLE, CODE="1", SCAN="0")
        self.assertEqual(failed.returncode, 1)
        self.assertIn("::error::", failed.stdout)
        self.assertIn("G-A G-C G-D G-B G-E", failed.stdout)
        self.assertEqual(self.run_step("Result", self.TABLE, CODE="0", SCAN="0")[0].returncode, 0)
        self.assertEqual(self.run_step("Result", self.TABLE, CODE="3", SCAN="0")[0].returncode, 1)
        absent, _ = self.run_step("Result", None, CODE="1", SCAN="0")
        self.assertEqual(absent.returncode, 1)
        self.assertIn("::error::", absent.stdout)
        unscanned, _ = self.run_step("Result", self.TABLE, CODE="1", SCAN="1")
        self.assertEqual(unscanned.returncode, 1)
        self.assertNotIn("G-A", unscanned.stdout)  # nothing from an unscanned file is quoted

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_the_quoted_line_cannot_inject_a_workflow_command(self) -> None:
        hostile = "FAILING: G-A ::set-output name=x::y %0A\n"
        failed, _ = self.run_step("Result", hostile, CODE="1", SCAN="0")
        self.assertEqual(failed.returncode, 1)
        self.assertEqual(failed.stdout.count("::"), 1 + 1, failed.stdout)  # only the `::error::` pair
        self.assertNotIn("%", failed.stdout)

    def test_actions_are_pinned_by_commit_and_nothing_reads_the_heldout_set(self) -> None:
        for use in re.findall(r"uses: (\S+)", self.code):
            self.assertRegex(use, r"^[\w./-]+@[0-9a-f]{40}$", use)
        self.assertNotIn("heldout", self.code.lower())


if __name__ == "__main__":
    unittest.main()
