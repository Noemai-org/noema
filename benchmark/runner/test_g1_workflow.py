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
        self.assertRegex(self.script, r"grep -Eq '\^\[1-9\]\[0-9\]\{0,15\}\$'")

    def test_the_run_id_is_validated_before_use(self) -> None:
        self.assertIn("grep -Eq '^[A-Za-z0-9_.-]{0,64}$'", self.script)

    def test_the_private_key_and_the_issuer_are_never_uploaded(self) -> None:
        uploaded = re.search(r"(?ms)uses: actions/upload-artifact@\S+.*?path: \|\n(.*?)(?=^\s+if-no-files-found)", self.text)
        assert uploaded
        paths = [line.strip() for line in uploaded.group(1).splitlines() if line.strip()]
        for path in paths:
            self.assertNotIn("idp", path)
            self.assertNotIn("private", path)
            self.assertNotEqual(path.rstrip("/"), "benchmark/out", "the whole output directory holds the issuer and the private key")

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
