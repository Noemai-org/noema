"""The G1 stack workflow (.github/workflows/bench-g1-stack.yml) must score the run and fail on a Tier 0 non-zero (Codex P1 on noema#444; SoT §38.3, INV-22).
The workflow is not run in a unit test; this reads it, like the other workflow guards, and checks the rules that make its result trustworthy."""
import re
import unittest
from pathlib import Path

WORKFLOW = Path(__file__).resolve().parents[2] / ".github" / "workflows" / "bench-g1-stack.yml"


class G1StackWorkflow(unittest.TestCase):
    def setUp(self) -> None:
        self.text = WORKFLOW.read_text()
        self.script = "\n".join(line for line in self.text.splitlines() if not line.lstrip().startswith("#"))

    # The Security Reviewer's criteria 6, 8 and 9 on noema#436 (the Founders' order of 15 Nov): the recorded FaultPlans, a leak scan with a planted canary over every artifact, and two
    # negative probes per case. Each must be run and each must fail the job.
    def test_the_fault_plans_are_recorded_and_the_identity_probes_run_in_the_runner_container(self) -> None:
        self.assertRegex(self.script, r"exec -T bench-runner python -m benchmark\.runner\.evidence fault-plans --suite /bench-out/suite --run /bench-out/run --out /bench-out/evidence")
        probes = re.search(r"python -m benchmark\.runner\.evidence probes (.*?)(?=\n\s*echo \"probes=)", self.script, re.S)
        assert probes
        for flag in ("--api-url http://core:8080", "--tenant-id ten_alpha", "--other-tenant-id ten_other", "--idp-binary", "--idp-key", "--idp-kid", "--idp-audience"):
            self.assertIn(flag, probes.group(1))

    def test_the_result_fails_unless_the_run_the_evidence_and_the_scan_are_all_met(self) -> None:
        result = re.search(r"(?ms)- name: Result.*", self.script)
        assert result
        for variable in ("FAULT_PLANS", "PROBES", "SCAN", "CODE"):
            self.assertIn(variable, result.group(0))
        self.assertRegex(result.group(0), r'\[ "\$\{FAULT_PLANS:-missing\}" = 0 \]')
        self.assertRegex(result.group(0), r'\[ "\$\{PROBES:-missing\}" = 0 \]')
        self.assertRegex(result.group(0), r'\[ "\$\{SCAN:-missing\}" = 0 \]')
        self.assertIn('exit "$fail"', result.group(0))

    def test_the_leak_scan_runs_the_canary_probe_and_covers_every_uploaded_path_and_the_summary(self) -> None:
        scan = re.search(r"python3 -I tools/ci/g1_leakscan\.py (.*?)\n\s*echo \"scan=", self.script, re.S)
        assert scan
        scanned = {t.rstrip("/") for t in re.findall(r"benchmark/out/[A-Za-z0-9_./-]+", scan.group(1))}
        self.assertIn("$GITHUB_STEP_SUMMARY", scan.group(1))
        uploaded = re.search(r"(?ms)uses: actions/upload-artifact@\S+.*?path: \|\n(.*?)(?=^\s+if-no-files-found)", self.text)
        assert uploaded
        for path in (line.strip() for line in uploaded.group(1).splitlines() if line.strip()):
            self.assertIn(path.rstrip("/"), scanned, f"{path} is uploaded but not leak-scanned")
        self.assertIn("--allow tools/ci/g1_leakscan.allow", scan.group(1))

    def test_nothing_is_uploaded_unless_the_scan_is_clean_and_the_scan_comes_first(self) -> None:
        self.assertIn("if: always() && steps.scan.outputs.scan == '0'", self.script)
        self.assertLess(self.script.index("tools/ci/g1_leakscan.py"), self.script.index("actions/upload-artifact"))
        self.assertLess(self.script.index("name: Summary"), self.script.index("tools/ci/g1_leakscan.py"), "the step summary is scanned after it is written")

    def test_the_leak_scan_allow_file_names_only_three_integer_fields(self) -> None:
        allow = (WORKFLOW.parents[2] / "tools" / "ci" / "g1_leakscan.allow").read_text().split()
        self.assertEqual(sorted(allow), sorted([r'^\s*"seed":\s*[0-9]+,?\s*$', r'^\s*"cost_usd_micros":\s*[0-9]+,?\s*$', r'^\s*"latency_ms":\s*[0-9]+,?\s*$']))

    def test_the_run_is_scored_by_the_scorer_in_the_runner_container(self) -> None:
        self.assertRegex(self.script, r"exec -T bench-runner python -m benchmark\.scoring")
        for flag in ("--cases", "--oracles", "--results", "--pack packs/fintech_v1.json", "--manifest", "--run-id", "--out"):
            self.assertIn(flag, self.script)

    def test_the_job_result_is_the_scorers_exit_when_the_run_ran(self) -> None:
        # runner 0 or 1 -> score; the scorer's exit (0 only when complete and Tier 0 is all zero) becomes the recorded code. Runner 2 and 3 are never scored.
        self.assertRegex(self.script, r'\[ "\$code" -le 1 \]')
        self.assertRegex(self.script, r"score_code=\$\?")
        self.assertRegex(self.script, r'code="\$score_code"')

    def test_not_run_and_a_nonzero_result_fail_the_job(self) -> None:
        self.assertIn("3) echo", self.script)
        self.assertRegex(self.script, r"\*\) echo \"::error::The run ended with exit code")
        self.assertNotRegex(self.script, r"continue-on-error")

    def test_a_dead_log_collector_invalidates_the_run(self) -> None:
        # Codex P1 on noema#444 (thread 4564863706): a collector that never started or died leaves a readable file and the refusal counters would report zero accesses.
        # The collector is checked after it starts and again before scoring, and a dead one makes the run NOT RUN (exit 3, never scored).
        checks = re.findall(r'kill -0 "\$logpid"', self.script)
        self.assertGreaterEqual(len(checks), 2, "the collector must be checked at the start and before scoring")
        self.assertIn("collector_dead=1", self.script)
        self.assertRegex(self.script, r'\[ "\$collector_dead" -eq 1 \] && code=3')
        self.assertNotRegex(self.script, r"logs -f[^\n]*2>/dev/null &")  # its errors are no longer thrown away

    def test_the_score_is_uploaded_and_no_secret_or_heldout_is_referenced(self) -> None:
        self.assertIn("benchmark/out/score/", self.text)
        self.assertNotRegex(self.text, r"secrets\.")
        self.assertNotIn("heldout", self.text.lower().replace("never mounted or read", ""))


if __name__ == "__main__":
    unittest.main()
