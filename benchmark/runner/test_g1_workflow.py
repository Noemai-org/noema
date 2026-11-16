"""The G1 stack workflow (.github/workflows/bench-g1-stack.yml) must score the run and fail on a Tier 0 non-zero (Codex P1 on noema#444; SoT §38.3, INV-22).
The workflow is not run in a unit test; this reads it, like the other workflow guards, and checks the rules that make its result trustworthy."""
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

WORKFLOW = Path(__file__).resolve().parents[2] / ".github" / "workflows" / "bench-g1-stack.yml"


class G1StackWorkflow(unittest.TestCase):
    def setUp(self) -> None:
        self.text = WORKFLOW.read_text()
        self.script = "\n".join(line for line in self.text.splitlines() if not line.lstrip().startswith("#"))

    # Session tokens (the Founders' order of 14 Nov, after noema#444): the runner issues each principal's token with the Edge's synthetic issuer. The core holds the public key
    # only; the private key and the issuer binary stay under benchmark/out/idp and are never uploaded (Security Reviewer's notes on noema#444).
    def test_the_runner_gets_the_five_idp_flags_with_the_issuers_paths(self) -> None:
        for flag in ("--idp-binary /bench-out/idp/synthetic-idp", "--idp-key /bench-out/idp/key/private.pem", "--idp-kid bench-k1", "--idp-audience noema-core", "--tenant-id ten_alpha"):
            self.assertIn(flag, self.script)
        self.assertNotIn("--tokens-dir", self.script)

    def test_the_stack_is_started_by_make_bench_up_which_makes_the_issuer_first(self) -> None:
        self.assertRegex(self.script, r"(?m)^\s+run: make bench-up$")
        makefile = (WORKFLOW.parents[2] / "Makefile").read_text()
        recipe = re.search(r"(?ms)^bench-up:\n(.*?)(?=^\S)", makefile)
        assert recipe
        self.assertIn("$(MAKE) bench-tokens", recipe.group(1))
        self.assertIn("BENCH_MAX_BODY_BYTES is required", recipe.group(1))

    def test_the_body_limit_is_a_required_input_with_no_default_and_is_validated(self) -> None:
        block = re.search(r"(?ms)^      max_body_bytes:\n(.*?)(?=^permissions:|^      [a-z_]+:\n)", self.text)
        assert block
        self.assertIn("required: true", block.group(1))
        self.assertNotRegex(block.group(1), r"(?m)^\s+default:")
        self.assertIn('case "$BENCH_MAX_BODY_BYTES" in', self.script)

    def validate(self, run_id: str, body: str) -> int:
        """Runs the workflow's own 'Validate the inputs' step in bash with these two values."""
        step = re.search(r"(?ms)- name: Validate the inputs\n\s+run: \|\n(.*?)(?=^      - )", self.text)
        assert step
        script = "\n".join(line[10:] for line in step.group(1).splitlines())
        return subprocess.run(["bash", "-c", script], env={"PATH": os.environ.get("PATH", ""), "GIVEN_RUN_ID": run_id, "BENCH_MAX_BODY_BYTES": body}, capture_output=True, check=False).returncode

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's validation step")
    def test_the_run_id_and_the_body_limit_are_validated_on_the_whole_string(self) -> None:
        # Codex P1 and the Security Reviewer on noema#577: grep passed any value with one matching line (a newline injected a $GITHUB_OUTPUT entry) and rejected the empty default.
        for run_id in ("", "g1-stack-gh12.3_a", "a" * 64):
            self.assertEqual(self.validate(run_id, "16384"), 0, repr(run_id))
        for run_id in ("ok\nGITHUB_OUTPUT_INJECTION=1", "ok\n", "\nok", "a b", "a;b", "$(x)", "a" * 65, "x/y", "x\ry"):
            self.assertEqual(self.validate(run_id, "16384"), 2, repr(run_id))
        for body in ("1", "16384", "9" * 16):
            self.assertEqual(self.validate("", body), 0, repr(body))
        for body in ("", "0", "016384", "16384\n99", "16384\n", "-1", "1.5", "1e3", "9" * 17, "1 2"):
            self.assertEqual(self.validate("", body), 2, repr(body))

    def test_the_checkout_does_not_keep_the_repository_token(self) -> None:
        self.assertRegex(self.text, r"uses: actions/checkout@\S+[^\n]*\n\s+with:\n\s+persist-credentials: false")

    def test_the_uploaded_artifacts_are_leak_scanned_first_and_a_failed_scan_fails_the_job(self) -> None:
        scan = re.search(r"python3 -I tools/ci/leakscan\.py (.*?)\n\s*echo \"scan=", self.script, re.S)
        assert scan
        scanned = {t.rstrip("/") for t in re.findall(r"benchmark/out/[A-Za-z0-9_./-]+", scan.group(1))}
        uploaded = re.search(r"(?ms)uses: actions/upload-artifact@\S+.*?path: \|\n(.*?)(?=^\s+if-no-files-found)", self.text)
        assert uploaded
        for path in (line.strip() for line in uploaded.group(1).splitlines() if line.strip()):
            self.assertIn(path.rstrip("/"), scanned, f"{path} is uploaded but not leak-scanned")
        self.assertIn("--allow tools/ci/g1_leakscan.allow", scan.group(1))
        self.assertIn("if: always() && steps.scan.outputs.scan == '0'", self.script)
        self.assertLess(self.script.index("tools/ci/leakscan.py"), self.script.index("actions/upload-artifact"))
        result = re.search(r"(?ms)- name: Result.*", self.script)
        assert result
        self.assertRegex(result.group(0), r'\[ "\$\{SCAN:-missing\}" = 0 \]')

    def scan(self, target: Path, use_allow: bool = True) -> int:
        allow = WORKFLOW.parents[2] / "tools" / "ci" / "g1_leakscan.allow"
        scanner = WORKFLOW.parents[2] / "tools" / "ci" / "leakscan.py"
        args = [sys.executable, "-I", str(scanner)] + (["--allow", str(allow)] if use_allow else []) + [str(target)]
        return subprocess.run(args, capture_output=True, check=False).returncode

    def test_the_scorers_real_output_passes_the_scan(self) -> None:
        # Codex P2 on noema#577 (the Wilson and rule-of-three bounds) and C2's read (exact_upper95 and the mean): the scan must pass what the scorer's own CLI writes, not numbers copied
        # into the test. This runs the CLI on the scorer's own fixture with four repeat runs (so pass^k is present) and scans scorecard.json and report.md.
        from benchmark.scoring.test_cli import run_cli, write_repeat, write_tree

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            extra: list[str] = []
            for i in range(2, 6):
                extra += ["--repeat-results", str(write_repeat(root, f"r{i}", f"run_{i}"))]
            code, _ = run_cli(root, root / "out", *extra)
            self.assertEqual(code, 0)
            self.assertEqual(self.scan(root / "out"), 0)
            self.assertEqual(self.scan(root / "out", use_allow=False), 1, "without the allow file the statistics are findings")

    def test_the_scorers_decimal_fields_pass_in_every_shape_it_can_write(self) -> None:
        # The fixture's numbers are small; these lines are built by the scorer's own functions for a full run (n = 320, k = 0), and by its own formats for a mean that does not terminate.
        from benchmark.scoring.rates import make_rate

        card = json.dumps({"gate": make_rate(0, 320, failure=True), "rate": make_rate(150, 320), "turns": {"n": 3, "mean": round(4 / 3, 6), "median": 1, "p90": 2, "max": 2}}, indent=2)
        report = "- turns to resolution (customer turns, cases auto-resolved correctly): n = 3; mean 1.333333, median 1, 90th percentile 2, maximum 2"
        with tempfile.TemporaryDirectory() as tmp:
            f = Path(tmp) / "x.txt"
            f.write_text(card + "\n" + report + "\n", encoding="utf-8")
            self.assertEqual(self.scan(f), 0)
            for bad in ('  "account_number": 0.123456,', '  "rate": 12345.5,', '  0.1234567,', '  "note": 0.123456,', '  "seed": 1234567890123456,', '  "latency_ms": 1234567890,',
                        '  "cost_usd_micros": 12345678901,', "- turns to resolution (customer turns, cases auto-resolved correctly): n = 3; mean 1.333333, median 1, 90th percentile 2, maximum 2 acct 1234567"):
                f.write_text(bad + "\n", encoding="utf-8")
                self.assertEqual(self.scan(f), 1, bad)

    def test_the_allow_file_lets_the_scorers_own_metadata_through_and_nothing_else(self) -> None:
        # Codex P2 on noema#591: the score report prints the generator's seed and the latency and cost lines in Markdown, and the scorecard carries aggregate cost and percentiles.
        allow = WORKFLOW.parents[2] / "tools" / "ci" / "g1_leakscan.allow"
        scanner = WORKFLOW.parents[2] / "tools" / "ci" / "leakscan.py"
        good = [
            "- How they were generated: benchmark-generator 1.3.0, seed 20261020, suite suite-v1 1.0.0.",
            "- provider cost: 326693 millionths of a US dollar in total; 54448 per correct outcome (rounded down)",
            "- provider cost: 326693 millionths of a US dollar in total; no correct outcome to divide by",
            "- latency, all paths (n=320): P50 12000 ms, P95 123456 ms, P99 234567 ms",
            "- latency, clean_write (n=12): P50 120 ms, P95 12345 ms, P99 23456 ms",
            '  "seed": 20261103,', '  "cost_usd_micros": 326693,', '  "cost_usd_micros_total": 1234567', '  "cost_usd_micros_per_correct_outcome": 123456,',
            '  "latency_ms": 12345,', '  "p50_ms": 12000,', '  "p95_ms": 123456,', '  "p99_ms": 234567',
        ]
        bad = ["- How they were generated: benchmark-generator 1.3.0, seed 20261020, suite suite-v1 1.0.0. account 1234567890", "- note 123456 here",
               '  "account_number": 1234567890,', '  "seed_note": "12345678"', "- latency, all paths (n=320): P50 12000 ms, P95 123456 ms, P99 234567 ms and 12345678"]
        with tempfile.TemporaryDirectory() as tmp:
            def scan(lines: list[str], use_allow: bool) -> int:
                f = Path(tmp) / "x.txt"
                f.write_text("\n".join(lines) + "\n", encoding="utf-8")
                args = [sys.executable, "-I", str(scanner)] + (["--allow", str(allow)] if use_allow else []) + [str(f)]
                return subprocess.run(args, capture_output=True, check=False).returncode
            self.assertEqual(scan(good, True), 0)
            self.assertEqual(scan(good, False), 1, "without the allow file the same lines are findings")
            for line in bad:
                self.assertEqual(scan([line], True), 1, line)

    def test_the_private_key_and_the_issuer_are_never_uploaded(self) -> None:
        uploaded = re.search(r"(?ms)uses: actions/upload-artifact@\S+.*?path: \|\n(.*?)(?=^\s+if-no-files-found)", self.text)
        assert uploaded
        paths = [line.strip() for line in uploaded.group(1).splitlines() if line.strip()]
        for path in paths:
            self.assertNotIn("idp", path)
            self.assertNotIn("private", path)
            self.assertNotEqual(path.rstrip("/"), "benchmark/out", "the whole output directory holds the issuer and the private key")

    def test_the_core_and_the_token_script_name_the_key_id_and_audience_the_same_way(self) -> None:
        # C2 and Codex on noema#577: the core read NOEMA_BENCH_KID and NOEMA_BENCH_AUDIENCE while bench_tokens.sh read BENCH_KID and BENCH_AUDIENCE, so a changed value made the issuer
        # sign for one audience and the core expect another. One name on both sides, one default on both sides, and the workflow's runner flags carry the same defaults.
        compose = (WORKFLOW.parents[2] / "deploy" / "compose" / "bench-stack.yml").read_text()
        script = (WORKFLOW.parents[2] / "deploy" / "compose" / "bench_tokens.sh").read_text()
        for setting, variable, flag in (("NOEMA_SESSION_KID", "BENCH_KID", "--idp-kid"), ("NOEMA_SESSION_AUDIENCE", "BENCH_AUDIENCE", "--idp-audience")):
            in_compose = re.search(setting + r': "\$\{([A-Z_]+):-([A-Za-z0-9_.-]+)\}"', compose)
            in_script = re.search(r'="\$\{(' + variable + r'):-([A-Za-z0-9_.-]+)\}"', script)
            assert in_compose and in_script, setting
            self.assertEqual(in_compose.groups(), in_script.groups(), setting)
            self.assertRegex(self.script, flag + " " + re.escape(in_script.group(2)) + r"(?=\s|$)")

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
