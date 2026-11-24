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
    def test_the_runner_gets_the_five_idp_flags_from_the_bench_tokens_file_in_every_step_that_issues_a_token(self) -> None:
        # A517 and A572's order: the five IdP flags (--idp-binary, --idp-key, --idp-kid, --idp-audience, --tenant-id) are the ones `make bench-tokens` wrote to benchmark/out/idp/runner-args, with the
        # host path mapped to the runner's mount, in the run, the probes and both re-reads: one source, so a change of the key id, the audience or the tenant cannot reach one step and miss another.
        for step in ("run", "evidence", "recovery", "replay"):
            body = re.search(r"(?ms)- id: " + step + r"\n(.*?)(?=^      - )", self.text)
            assert body, step
            self.assertIn('idp_args="$(sed "s#$PWD/benchmark/out#/bench-out#g" benchmark/out/idp/runner-args', body.group(1), step)
            self.assertIn("$idp_args", body.group(1), step)
        for hard_coded in ("--idp-binary /bench-out/idp/synthetic-idp", "--idp-key /bench-out/idp/key/private.pem", "--idp-kid bench-k1", "--idp-audience noema-core"):
            self.assertNotIn(hard_coded, self.script, "a hard-coded IdP flag")
        for step in ("run", "evidence"):  # the access check (record-access) takes tenant ids only: it issues no token and is not one of the five
            body = re.search(r"(?ms)- id: " + step + r"\n(.*?)(?=^      - )", self.text)
            assert body
            self.assertNotIn("--tenant-id ten_alpha", body.group(1), step)
        self.assertIn("--other-tenant-id ten_other", self.script)  # the probes' second tenant is not one of the five
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

    def validate(self, run_id: str, body: str, mode: str = "full") -> int:
        """Runs the workflow's own 'Validate the inputs' step in bash with these two values."""
        step = re.search(r"(?ms)- name: Validate the inputs\n\s+run: \|\n(.*?)(?=^      - )", self.text)
        assert step
        script = "\n".join(line[10:] for line in step.group(1).splitlines())
        return subprocess.run(["bash", "-c", script], env={"PATH": os.environ.get("PATH", ""), "GIVEN_RUN_ID": run_id, "BENCH_MAX_BODY_BYTES": body, "MODE": mode}, capture_output=True, check=False).returncode

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's validation step")

    def test_the_run_id_and_the_body_limit_are_validated_on_the_whole_string(self) -> None:
        # Codex P1 and the Security Reviewer on noema#577: grep passed any value with one matching line (a newline injected a $GITHUB_OUTPUT entry) and rejected the empty default.
        for run_id in ("", "g1-stack-gh12.3_a", "a" * 64):
            self.assertEqual(self.validate(run_id, "16384"), 0, repr(run_id))
        for run_id in ("ok\nGITHUB_OUTPUT_INJECTION=1", "ok\n", "\nok", "a b", "a;b", "$(x)", "a" * 65, "x/y", "x\ry"):
            self.assertEqual(self.validate(run_id, "16384"), 2, repr(run_id))
        self.assertEqual(self.validate("", "16384", "stack-check"), 0)
        for mode in ("", "FULL", "full\nstack-check", "stack-check;x", "other"):
            self.assertEqual(self.validate("", "16384", mode), 2, repr(mode))
        for body in ("1", "16384", "9" * 16):
            self.assertEqual(self.validate("", body), 0, repr(body))
        for body in ("", "0", "016384", "16384\n99", "16384\n", "-1", "1.5", "1e3", "9" * 17, "1 2"):
            self.assertEqual(self.validate("", body), 2, repr(body))

    def test_the_checkout_does_not_keep_the_repository_token(self) -> None:
        self.assertRegex(self.text, r"uses: actions/checkout@\S+[^\n]*\n\s+with:\n\s+persist-credentials: false")

    # The Security Reviewer's criteria 6, 8 and 9 on noema#436 (the Founders' order of 15 Nov): the recorded FaultPlans, a leak scan with a planted canary over every artifact, and two
    # negative probes per case. Each must be run and each must fail the job.
    def test_the_fault_plans_are_recorded_and_the_identity_probes_run_in_the_runner_container(self) -> None:
        self.assertRegex(self.script, r"exec -T bench-runner python -m benchmark\.runner\.evidence fault-plans --suite /bench-out/suite --run /bench-out/run --out /bench-out/evidence")
        probes = re.search(r"python -m benchmark\.runner\.evidence probes (.*?)(?=\n\s*echo \"probes=)", self.script, re.S)
        assert probes
        for flag in ("--api-url http://core:8080", "$idp_args", "--other-tenant-id ten_other"):  # the five IdP flags arrive in $idp_args, from runner-args
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
        scan = re.search(r"(set -- benchmark/out/run .*?)\n\s*echo \"scan=", self.script, re.S)
        assert scan
        paths_only = re.sub(r"--inventory \S+|--root \S+", "", scan.group(1))  # the inventory is the scan's own output and the root a directory: neither is a scanned path
        scanned = {t.rstrip("/") for t in re.findall(r"benchmark/out/[A-Za-z0-9_./-]+", paths_only)}
        self.assertIn("benchmark/out/summary.md", scan.group(1))
        self.assertNotIn("$GITHUB_STEP_SUMMARY", scan.group(1), "that variable names a different file in each step (Codex P2 on noema#591)")
        uploaded = re.search(r"(?ms)uses: actions/upload-artifact@\S+.*?path: \|\n(.*?)(?=^\s+if-no-files-found)", self.text)
        assert uploaded
        for path in (line.strip() for line in uploaded.group(1).splitlines() if line.strip().startswith("benchmark/out/")):
            if path == "benchmark/out/leak-scan.txt":
                continue  # the scan's own output (file, line, kind, length and fingerprint only: never a value); it cannot be scanned before it exists. BO's package row 8 reads it.
            self.assertIn(path.rstrip("/"), scanned, f"{path} is uploaded but not leak-scanned")
        self.assertIn('--allow "$allow"', scan.group(1))

    def test_the_scan_output_is_kept_for_the_package_and_the_scan_exit_code_survives_the_pipe(self) -> None:
        # BO's package row 8 (A355) is computed from the scan's own output, so the workflow saves it. The pipe must not hide the scan's exit code.
        step = re.search(r"(?ms)- id: scan\n(.*?)(?=^      - )", self.text)
        assert step
        self.assertRegex(step.group(1), r'(?m)"\$@" \| tee -a benchmark/out/leak-scan\.txt$')
        self.assertIn('echo "scan=${PIPESTATUS[0]}" >> "$GITHUB_OUTPUT"', step.group(1))
        uploaded = re.search(r"(?ms)- name: Upload the stamped evidence.*?path: \|\n(.*?)(?=^\s+if-no-files-found)", self.text)
        assert uploaded
        self.assertIn("benchmark/out/leak-scan.txt", uploaded.group(1), "the scan output travels in the stamped-evidence artifact: it is written by the scan, so it is not one of the scanned files (row 12)")

    def test_the_evidence_carries_the_dispatch_sha_and_run_id_for_the_package_to_compare(self) -> None:
        # noema#717 (BO's package, relayed by SR): the first line of leak-scan.txt is `stamp: <sha> <run id>`, and the evidence files carry the same two values in a top-level "stamp".
        self.assertIn('echo "stamp: $GITHUB_SHA $GITHUB_RUN_ID" > benchmark/out/leak-scan.txt', self.script)
        self.assertLess(self.script.index('> benchmark/out/leak-scan.txt'), self.script.index("| tee -a benchmark/out/leak-scan.txt"), "the stamp line is written first; the scan appends")
        self.assertEqual(self.script.count('--sha "$GITHUB_SHA" --run-id "$GITHUB_RUN_ID"'), 3, "both evidence commands (fault plans and probes) and the package step are stamped")
        for command in ("fault-plans", "probes"):
            self.assertIn("python -m benchmark.runner.evidence " + command, self.script)

    # A445 (interim): the stack-check reports /readyz and the netcheck now, and requires them once the final composition PR merges.
    def test_the_stack_check_reports_readyz_and_the_netcheck_and_does_not_require_them_yet(self) -> None:
        block = re.search(r'(?s)if \[ "\$MODE" = stack-check \]; then(.*?)\n          else\n', self.script)
        assert block, "no stack-check block"
        self.assertIn('HTTPConnection("core", 8080', block.group(1))
        self.assertIn('"/readyz"', block.group(1))
        self.assertIn("deploy/compose/netcheck/profiles.py", block.group(1))
        self.assertIn('echo "readyz=${readyz:-not-checked}" >> "$GITHUB_OUTPUT"', self.script)
        self.assertIn('echo "netcheck=${netcheck:-not-checked}" >> "$GITHUB_OUTPUT"', self.script)
        result = re.search(r"(?ms)- name: Result .*?(?=^\S|\Z)", self.text)
        assert result
        self.assertIn('READINESS_REQUIRED: "false"', result.group(0))  # flipped to "true" by the final composition PR (A445)
        self.assertRegex(result.group(0), r'(?s)\[ "\$READINESS_REQUIRED" = true \].*READYZ.*NETCHECK')
        summary = re.search(r"(?ms)- name: Summary.*?(?=^      - name:)", self.text)
        assert summary
        self.assertIn("/readyz", summary.group(0))
        self.assertIn("netcheck", summary.group(0))

    # A444 (BO, noema#821): the package step stamps the run directory and the probes file and counts each case's writes from the runner's own results.
    def test_every_step_that_calls_compose_exports_the_variables_the_overlay_requires(self) -> None:
        # Codex P2 on noema#730: exports of one step do not reach the next, and bench-stack.yml requires NOEMA_BENCH_OUT, NOEMA_BENCH_UID, NOEMA_BENCH_GID and NOEMA_BENCH_MAX_BODY_BYTES with `${...:?}`,
        # so a step that calls `$compose` without them fails at configuration, before it runs anything. Each step that defines the compose command exports all four, from the same expressions.
        steps = re.split(r"(?m)^      - ", self.text)
        called = [s for s in steps if 'compose="docker compose' in s]
        self.assertGreaterEqual(len(called), 4, "the run, evidence, package and access steps call compose")
        for step in called:
            name = step.splitlines()[0][:70]
            for variable in ('NOEMA_BENCH_OUT="$PWD/benchmark/out"', 'NOEMA_BENCH_UID="$(id -u)"', 'NOEMA_BENCH_GID="$(id -g)"', "NOEMA_BENCH_MAX_BODY_BYTES="):
                self.assertIn(variable, step, f"{name}: calls compose without {variable.split('=')[0]}")
            self.assertRegex(step, r"export NOEMA_BENCH_OUT=.*STANDIN_TENANT_ID=ten_alpha", name)

    # A476 and A502 (relayed): the core is restarted after the cases, then BO's reread adds after_recovery and after_replay before the package stamps the file.
    def test_the_core_is_restarted_after_the_evidence_and_the_two_rereads_follow_before_the_package(self) -> None:
        order = [self.text.index(f"- id: {i}") for i in ("evidence", "restart", "recovery", "replay", "package")]
        self.assertEqual(order, sorted(order), "evidence, restart, recovery, replay, package")
        restart = re.search(r"(?ms)- id: restart\n(.*?)(?=^      - )", self.text)
        assert restart
        self.assertIn("if: always() && inputs.mode == 'full'", restart.group(1))
        self.assertIn("$compose restart core >> benchmark/out/joblog/restart.log 2>&1", restart.group(1))
        for step, phase, needs in (("recovery", "after_recovery", "restart"), ("replay", "after_replay", "recovery")):
            body = re.search(r"(?ms)- id: " + step + r"\n(.*?)(?=^      - )", self.text)
            assert body
            text = body.group(1)
            self.assertIn(f"if: always() && inputs.mode == 'full' && steps.{needs}.outputs.{needs} == '0'", text)
            self.assertIn("python -m benchmark.runner.reread --run /bench-out/run --phase " + phase, text)
            self.assertIn("benchmark/out/idp/runner-args", text, "the five IdP flags come from the bench-tokens runner-args file (A517)")
            self.assertIn("$idp_args", text)
            self.assertNotIn("--tokens-dir", text)
            self.assertNotIn("--idp-key", text, "no hard-coded IdP flag")
            for flag in ("--api-url http://core:8080", "--readyz-url http://core:8080/readyz", "--ready-timeout-s 120"):
                self.assertIn(flag, text, flag)
            self.assertIn(">> benchmark/out/joblog/reread.log 2>&1", text)
            self.assertNotIn("audit-credential", text, "A502: no credential file, no seeded principal")
            self.assertIn(f'echo "{step}=$?" >> "$GITHUB_OUTPUT"', text)
        result = re.search(r"(?ms)- name: Result .*?(?=^\S|\Z)", self.text)
        assert result
        for name, step in (("RESTART", "restart"), ("RECOVERY", "recovery"), ("REPLAY", "replay")):
            self.assertIn(f"{name}: ${{{{ steps.{step}.outputs.{step} }}}}", result.group(0))
            self.assertRegex(result.group(0), r'\[ "\$\{' + name + r':-missing\}" = 0 \]')

    def test_the_package_step_runs_after_the_evidence_and_before_the_scan_and_a_failure_fails_the_result(self) -> None:
        step = re.search(r"(?ms)- id: package\n(.*?)(?=^      - )", self.text)
        assert step, "no package step"
        body = step.group(1)
        self.assertIn("if: always() && inputs.mode == 'full'", body)
        self.assertIn("python -m benchmark.runner.package_artifacts", body)
        self.assertIn('--sha "$GITHUB_SHA" --run-id "$GITHUB_RUN_ID"', body)
        self.assertIn("--run /bench-out/run", body)
        self.assertIn("--probes /bench-out/evidence/negative-probes.json", body)
        self.assertIn('echo "package=$?" >> "$GITHUB_OUTPUT"', body)
        self.assertLess(self.text.index("- id: evidence"), self.text.index("- id: package"))
        self.assertLess(self.text.index("- id: package"), self.text.index("- id: scan"), "the new files are scanned and uploaded")
        self.assertLess(self.text.index("- id: package"), self.text.index("name: Stop the stack"), "the stack is still up")
        result = re.search(r"(?ms)- name: Result .*?(?=^\S|\Z)", self.text)
        assert result
        self.assertIn("PACKAGE: ${{ steps.package.outputs.package }}", result.group(0))
        self.assertRegex(result.group(0), r'\[ "\$\{PACKAGE:-missing\}" = 0 \]')

    def test_the_record_access_check_takes_the_runners_request_record(self) -> None:
        # A354: the expected RECORD_ACCESSED count is derived from the run's own request record (benchmark/out/run/requests.jsonl), not a constant.
        self.assertIn("--requests benchmark/out/run/requests.jsonl", self.script)

    def test_nothing_is_uploaded_unless_the_scan_is_clean_and_the_scan_comes_first(self) -> None:
        self.assertIn("if: always() && steps.scan.outputs.scan == '0'", self.script)
        self.assertLess(self.script.index("tools/ci/g1_leakscan.py"), self.script.index("actions/upload-artifact"))
        self.assertLess(self.script.index("name: Summary"), self.script.index("tools/ci/g1_leakscan.py"), "the step summary is scanned after it is written")

    def test_the_step_summary_is_published_only_after_a_clean_scan(self) -> None:
        publish = re.search(r"(?ms)- name: Publish the step summary.*?(?=^      - name:)", self.script)
        assert publish
        self.assertIn('[ "${SCAN:-missing}" = 0 ]', publish.group(0))
        self.assertLess(self.script.index("tools/ci/g1_leakscan.py"), self.script.index("name: Publish the step summary"))
        self.assertEqual(self.script.count("cat benchmark/out/summary.md"), 1)
        self.assertNotRegex(self.script.split("name: Publish the step summary")[0], r">>\s*\"\$GITHUB_STEP_SUMMARY\"")

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

    def test_the_leak_scan_allow_file_names_only_numeric_fields_and_the_scorers_metadata_lines(self) -> None:
        allow = (WORKFLOW.parents[2] / "tools" / "ci" / "g1_leakscan.allow").read_text().splitlines()
        self.assertEqual(len([l for l in allow if l.strip()]), 14)
        self.assertNotIn('"run_id"', "\n".join(allow), "the run id is allowed per run by the workflow, never by a standing pattern (C1 on noema#730)")
        for line in allow:
            if line.strip():
                self.assertTrue(line.startswith("^") and line.endswith("$"), f"unanchored allow pattern: {line}")

    def test_the_scan_allows_the_stamp_of_this_run_only_by_an_exact_per_run_pattern(self) -> None:
        # C1 on noema#730 (P3): a standing `[0-9]{1,20}` pattern lets any 20-digit string through under a "run_id" key. The workflow builds the allow file for the run: the standing file plus one line
        # that names this run's own id, so only that id passes.
        step = re.search(r"(?ms)- id: scan\n(.*?)(?=^      - )", self.text)
        assert step
        self.assertIn('cp tools/ci/g1_leakscan.allow "$allow"', step.group(1))
        self.assertIn('"$GITHUB_RUN_ID"', step.group(1))
        self.assertRegex(step.group(1), r"""printf '%s\\n' '\^\\s\*"run_id":\\s\*"'"\$GITHUB_RUN_ID"'",\?\\s\*\$' >> "\$allow\"""")
        standing = (WORKFLOW.parents[2] / "tools" / "ci" / "g1_leakscan.allow").read_text().splitlines()
        run_id = "44318438906"
        per_run = '^\\s*"run_id":\\s*"' + run_id + '",?\\s*$'
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            allow = root / "allow"
            allow.write_text("\n".join(standing + [per_run]) + "\n", encoding="utf-8")
            (root / "out").mkdir()
            stamp = '{\n  "stamp": {\n    "run_id": "%s",\n    "sha": "0123456789abcdef0123456789abcdef01234567"\n  }\n}\n'
            (root / "out" / "p.json").write_text(stamp % run_id, encoding="utf-8")
            code = subprocess.run([sys.executable, "-I", str(WORKFLOW.parents[2] / "tools" / "ci" / "leakscan.py"), "--allow", str(allow), str(root / "out")], capture_output=True, text=True, check=False).returncode
            self.assertEqual(code, 0, "this run's own id passes")
            for other in ("44318438907", "01234567890123456789", "4431843890644318438906"):
                (root / "out" / "p.json").write_text(stamp % other, encoding="utf-8")
                code = subprocess.run([sys.executable, "-I", str(WORKFLOW.parents[2] / "tools" / "ci" / "leakscan.py"), "--allow", str(allow), str(root / "out")], capture_output=True, text=True, check=False).returncode
                self.assertEqual(code, 1, f"another digit string under run_id ({other}) must be found")

    def test_the_standing_allow_file_no_longer_passes_a_run_id_and_the_other_leak_shapes_are_still_found(self) -> None:
        # C1 on noema#730 (P3): the standing allow file holds no run id (the workflow adds this run's own line, tested above), so the stamp is a finding under it.
        # Codex P2 on noema#730: the stamp carries the run id (digits) and the commit SHA (40 hexadecimal, which can hold a run of five digits). The allow file lets exactly those two lines through the
        # digit-run rule; a number under any other key, or a 12-digit value under the stamp's key that is not a run id, is still found.
        stamp = '{\n  "met": true,\n  "stamp": {\n    "run_id": "44318438906",\n    "sha": "0123456789abcdef0123456789abcdef01234567"\n  }\n}\n'
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "out").mkdir()
            (root / "out" / "negative-probes.json").write_text(stamp, encoding="utf-8")
            self.assertEqual(self.scan(root / "out"), 1, "the standing allow file does not pass a run id")
            self.assertEqual(self.scan(root / "out", use_allow=False), 1, "without the allow file the stamp is a finding")
            for leak in ('  "account": "0123456789"\n', '    "run_id": "x 0123456789"\n', '    "sha": "0123456789abcdef0123456789abcdef01234567 0123456789"\n'):  # the SHA is one hexadecimal token, which the digit rule does not read as a run
                (root / "out" / "negative-probes.json").write_text(stamp + leak, encoding="utf-8")
                self.assertEqual(self.scan(root / "out"), 1, leak)

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's log-capture step")
    def test_a_start_that_failed_before_the_run_step_still_leaves_both_collector_files_for_the_scan(self) -> None:
        # The stack-check of 24 Nov (run 45423718092) failed at `make bench-up` (the core exited 1), so the run step never made benchmark/out/mock-case.log or mock-case-collector.err. The scan
        # names both files and refuses a missing one (exit 2), which left the core's own log unscanned, not uploaded and not shown: the failure had no visible reason. The capture step runs on
        # every outcome and now leaves an empty file where the run step made none, so the service logs are scanned and, if clean, uploaded. The job still fails on the start step; an existing
        # file (a real collector's) is never touched.
        step = re.search(r"(?ms)- id: logs\n.*?run: \|\n(.*?)(?=^      - )", self.text)
        assert step
        script = "\n".join(line[10:] for line in step.group(1).splitlines())
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "bin").mkdir()
            stub = root / "bin" / "docker"
            stub.write_text('#!/bin/sh\necho "a log line"\n', encoding="utf-8")
            stub.chmod(0o755)
            env = {"PATH": f"{root / 'bin'}{os.pathsep}{os.environ.get('PATH', '')}", "GITHUB_OUTPUT": str(root / "gh_out")}
            out = root / "benchmark" / "out"
            subprocess.run(["bash", "-c", script], cwd=root, check=False, capture_output=True, text=True, env=env)
            for name in ("mock-case.log", "mock-case-collector.err"):
                self.assertTrue((out / name).is_file(), f"{name} is missing after a start that never reached the run step")
                self.assertEqual((out / name).read_text(encoding="utf-8"), "")
            (out / "mock-case.log").write_text("real collector line\n", encoding="utf-8")
            subprocess.run(["bash", "-c", script], cwd=root, check=False, capture_output=True, text=True, env=env)
            self.assertEqual((out / "mock-case.log").read_text(encoding="utf-8"), "real collector line\n", "an existing collector file is never touched")

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_a_stack_check_shows_the_tail_of_the_cores_log_only_after_a_clean_scan(self) -> None:
        # The first two stack-checks on main ended with the core exiting 1 and the reason only inside an artifact. The tail of the core's own log is shown in the stack-check's job log, after the
        # leak scan that already covered the file and only if it was clean; the job-log scan reads it again. A full run shows no service log.
        show = re.search(r"(?ms)- name: Show the run output \(only if the leak scan is clean\)\n.*?        run: \|\n(.*?)(?=^      - )", self.text)
        assert show
        script = "\n".join(line[10:] for line in show.group(1).splitlines())
        def run(scan: str, mode: str) -> str:
            with tempfile.TemporaryDirectory() as tmp:
                logs = Path(tmp, "benchmark", "out", "service-logs")
                logs.mkdir(parents=True)
                (logs / "core.log").write_text("".join(f"core line {i}\n" for i in range(200)), encoding="utf-8")
                (logs / "model.log").write_text("model line\n", encoding="utf-8")
                r = subprocess.run(["bash", "-e", "-c", script], cwd=tmp, capture_output=True, text=True, check=False, env={"PATH": os.environ["PATH"], "SCAN": scan, "MODE": mode})
                self.assertEqual(r.returncode, 0, f"the show step must not fail a run (scan {scan}, mode {mode}): {r.stderr}")  # a group ends with its last command's status (the 23 Nov Summary step)
                return r.stdout
        out = run("0", "stack-check")
        self.assertIn("core line 199", out)
        self.assertNotIn("core line 100", out, "only the tail is shown")
        self.assertNotIn("model line", out, "only the core's log is shown")
        self.assertNotIn("core line", run("2", "stack-check"), "a scan that did not pass shows nothing")
        self.assertNotIn("core line", run("0", "full"), "a full run shows no service log")

    def test_a_captured_service_log_is_not_scanned_with_the_model_access_log_on(self) -> None:
        # Codex P1 on noema#591: uvicorn's access line carries the client's ephemeral port, five digits, on every request and on the health check every three seconds, so a captured model
        # log would fail the scan of an otherwise clean run. The workflow captures the model's log; the bench overlay must start it without the access log.
        captured = re.search(r"for svc in ([a-z -]+); do", self.script)
        assert captured
        self.assertIn("model", captured.group(1).split())
        overlay = (WORKFLOW.parents[2] / "deploy" / "compose" / "bench-model.yml").read_text()
        entrypoint = re.search(r"entrypoint: \[(.*?)\]", overlay)
        assert entrypoint
        self.assertIn('"--no-access-log"', entrypoint.group(1))

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's log-capture step")
    def test_a_service_log_that_could_not_be_collected_fails_the_job(self) -> None:
        # Codex P2 on noema#591: set +e and exit 0 hid a failed `docker compose logs`, leaving an empty file the scan passed. The real step runs here against a stub `docker`.
        step = re.search(r"(?ms)- id: logs\n.*?run: \|\n(.*?)(?=^      - )", self.text)
        assert step
        script = "\n".join(line[10:] for line in step.group(1).splitlines())
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "bin").mkdir()
            stub = root / "bin" / "docker"
            stub.write_text('#!/bin/sh\nfor a in "$@"; do last="$a"; done\n[ "$last" = "$FAIL_SERVICE" ] && exit 1\n[ "$last" = "$EMPTY_SERVICE" ] && exit 0\necho "a log line of $last"\n', encoding="utf-8")
            stub.chmod(0o755)
            for fail, empty, want in (("", "", "collected=0"), ("model", "", "collected=1"), ("", "standin", "collected=1")):
                out = root / f"out_{fail or 'none'}_{empty or 'none'}.txt"  # a fresh name per case, never pre-created: Windows can hold a just-written file when bash appends to it
                run_ = subprocess.run(["bash", "-c", script], cwd=root, check=False, capture_output=True, text=True,
                               env={"PATH": f"{root / 'bin'}{os.pathsep}{os.environ.get('PATH', '')}", "GITHUB_OUTPUT": str(out), "FAIL_SERVICE": fail or "-", "EMPTY_SERVICE": empty or "-"})
                self.assertEqual(out.read_text(encoding="utf-8").strip(), want, (fail, empty, run_.returncode, run_.stderr[-300:]))
        result = re.search(r"(?ms)- name: Result.*", self.script)
        assert result
        self.assertIn("COLLECTED: ${{ steps.logs.outputs.collected }}", self.text)
        self.assertRegex(result.group(0), r'\[ "\$\{COLLECTED:-missing\}" = 0 \]')

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
            # The workflow no longer carries a copy of the default: its steps read the flags the token script wrote (runner-args), so the two cannot differ.
            self.assertNotRegex(self.script, flag + " " + re.escape(in_script.group(2)) + r"(?=\s|$)")

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

    # SR's first-dispatch conditions and the stack-check mode (A323, A325 as relayed; noema#436).
    def test_mode_is_a_choice_of_full_or_stack_check_and_defaults_to_full(self) -> None:
        block = re.search(r"(?ms)^      mode:\n(.*?)(?=^      [a-z_]+:\n|^permissions:)", self.text)
        assert block
        self.assertIn("type: choice", block.group(1))
        self.assertIn("default: full", block.group(1))
        for option in ("- full", "- stack-check"):
            self.assertIn(option, block.group(1))

    def test_stack_check_never_runs_the_suite_and_never_counts_as_evidence(self) -> None:
        self.assertRegex(self.script, r'MODE" = stack-check')
        self.assertIn("STACK-CHECK", self.text)
        self.assertIn("not evidence", self.text.lower())
        evidence = re.search(r"(?ms)- id: evidence\n(.*?)(?=^      - )", self.text)
        assert evidence
        self.assertIn("inputs.mode == 'full'", evidence.group(1))

    def test_every_record_read_is_counted_in_the_core_log_in_full_mode(self) -> None:
        self.assertIn("tools/ci/g1_record_access.py", self.script)
        self.assertIn("--core-log benchmark/out/service-logs/core.log", self.script)
        self.assertIn("--correlations benchmark/out/run/correlations.json", self.script)
        self.assertIn("RECORD_ACCESSED", self.text)
        self.assertIn("steps.count.outputs.record_accessed", self.script)

    def test_a_stack_check_run_scans_only_what_it_produced(self) -> None:
        # Codex P2 on noema#730: a stack-check run has no access check, so record-accessed.txt never exists; naming it in the scan made a healthy run fail before the Result step.
        step = re.search(r"(?ms)- id: scan\n(.*?)(?=^      - )", self.text)
        assert step
        body = step.group(1)
        self.assertIn("MODE: ${{ inputs.mode }}", body)
        self.assertRegex(body, r'\[ "\$MODE" = stack-check \] \|\| set -- "\$@" benchmark/out/record-accessed\.txt')
        base = re.search(r"set -- (benchmark/out/run [^\n]*)", body)
        assert base
        self.assertNotIn("record-accessed", base.group(1), "the base list is what every mode produces")

    def test_the_job_log_scan_turns_off_only_the_digit_rule(self) -> None:
        # Codex P2 on noema#730: the runner writes its own digits into the log (image version, run id, byte counts, the dispatch inputs it echoes, the seed in the command), which the digit-run
        # rule cannot be allowed for without a broad exemption. The flag turns that one rule off for that one file; every other rule is still probed (tools/ci/test_g1_leakscan.py).
        job = re.search(r"(?ms)^  joblog-scan:\n(.*)", self.text)
        assert job
        line = re.search(r"python3 -I tools/ci/g1_leakscan\.py ([^\n]*)", job.group(1))
        assert line
        self.assertIn("--no-digit-rule", line.group(1))
        # The one allow file the job log may use is its own, with the single Go-download line shape (run 45128374794; test_the_job_log_scan_uses_its_own_allow_file_and_not_the_artifact_one);
        # the standing allow file of the artifact scans is never used for it.
        self.assertEqual(re.findall(r"--allow (\S+)", line.group(1)), ["tools/ci/g1_joblog.allow"])
        self.assertEqual(len(re.findall(r"python3 -I tools/ci/g1_leakscan\.py --no-digit-rule", self.text)), 1, "only the job log is scanned without the digit rule")

    # A388: Playwright against the running stack, after the stack is up, opt-in until the wiring merges. FE owns the specs.
    def test_the_browser_checks_are_an_opt_in_boolean_that_defaults_to_off(self) -> None:
        inp = re.search(r"(?ms)^      playwright:\n(.*?)(?=^      \w+:\n|^permissions:)", self.text)
        assert inp, "no playwright input"
        self.assertIn("type: boolean", inp.group(1))
        self.assertIn("default: false", inp.group(1))

    def test_the_browser_step_runs_only_when_asked_before_the_stack_stops_and_says_not_run_without_a_stack_config(self) -> None:
        step = re.search(r"(?ms)- id: playwright\n(.*?)(?=^      - )", self.text)
        assert step, "no playwright step"
        body = step.group(1)
        self.assertIn("if: always() && inputs.playwright", body)
        self.assertLess(self.text.index("- id: playwright"), self.text.index("name: Stop the stack"), "the stack must still be up")
        self.assertIn("playwright.stack.config.mjs", body)
        self.assertRegex(body, r"(?s)NOT RUN.*playwright=3")  # no config for a stack run: NOT RUN (3), never a pass; the Result step fails a requested check that is not 0
        self.assertIn("NOEMA_UI_URL: http://127.0.0.1:8082", body)
        self.assertNotIn("secrets.", body)
        self.assertNotIn("npm install", body)  # npm ci only: the lock file decides what runs
        self.assertIn("setup-node@", self.text)

    def test_a_requested_browser_check_that_did_not_pass_fails_the_run_and_is_never_g1_evidence(self) -> None:
        result = re.search(r"(?ms)- name: Result .*?(?=^\S|\Z)", self.text)
        assert result
        self.assertIn("PLAYWRIGHT: ${{ steps.playwright.outputs.playwright }}", result.group(0))
        self.assertRegex(result.group(0), r'\[ "\$\{PLAYWRIGHT:-missing\}" = 0 \]')
        self.assertIn("not evidence for G1", self.text.split("id: playwright")[1].split("- name: Stop the stack")[0])

    def test_the_job_log_is_scanned_by_a_job_that_starts_after_it_ends(self) -> None:
        job = re.search(r"(?ms)^  joblog-scan:\n(.*)", self.text)
        assert job, "no joblog-scan job"
        body = job.group(1)
        self.assertIn("needs: g1-stack", body)
        self.assertIn("if: always()", body)
        self.assertIn("actions: read", body)
        self.assertIn("tools/ci/g1_leakscan.py", body)
        self.assertIn("/logs", body)
        self.assertNotIn("contents: write", body)

    # SR's two first-dispatch conditions on noema#436 (A323): the job log is scanned, and the access log shows the handler ran for no refusal.
    def commands(self) -> list[str]:
        """The workflow's shell commands, each backslash continuation joined to its line."""
        return [c for c in re.sub(r"\\\n\s*", " ", self.script).splitlines() if c.strip()]

    def test_nothing_the_runner_the_generator_the_scorer_or_the_evidence_prints_reaches_the_job_log(self) -> None:
        execs = [c for c in self.commands() if "$compose exec" in c]
        self.assertGreaterEqual(len(execs), 6, "the generator, the runner, the scorer and the three evidence commands")
        for command in execs:
            self.assertRegex(command, r">> benchmark/out/joblog/[a-z]+\.log 2>&1\s*$", command[:120])

    def test_the_job_log_files_are_scanned_uploaded_and_shown_only_after_a_clean_scan(self) -> None:
        scan = re.search(r"(?ms)- id: scan\n.*?(?=^      - )", self.script)
        upload = re.search(r"(?ms)- name: Upload the artifacts.*?(?=^      - )", self.text)
        assert scan and upload
        self.assertIn("benchmark/out/joblog", scan.group(0))
        self.assertIn("benchmark/out/joblog/", upload.group(0))
        show = re.search(r"(?ms)- name: Show the run output \(only if the leak scan is clean\)\n(.*?)(?=^      - )", self.text)
        assert show
        self.assertRegex(show.group(1), r'if \[ "\$\{SCAN:-missing\}" = 0 \]')
        self.assertLess(self.text.index("- id: scan"), self.text.index("Show the run output"), "the output is shown after the scan, never before")
        # the steps that run the stack print no run output before the scan: no `cat` of a joblog file outside the show step
        outside = self.text.replace(show.group(0), "")
        self.assertNotRegex(outside, r"cat [^\n]*joblog")

    def test_the_access_log_step_runs_after_the_logs_are_collected_and_before_the_stack_stops_and_fails_the_job(self) -> None:
        order = [self.text.index(x) for x in ("- id: evidence", "- id: logs", "- id: access", "name: Stop the stack")]
        self.assertEqual(order, sorted(order), "the probes, then the log capture, then the access check, then the stop")
        step = re.search(r"(?ms)- id: access\n(.*?)(?=^      - )", self.text)
        assert step
        self.assertRegex(step.group(1), r"benchmark\.runner\.evidence record-access --probes ")
        self.assertIn("/bench-out/evidence/negative-probes.json", step.group(1))
        self.assertIn("/bench-out/service-logs/core.log", step.group(1))
        self.assertIn("--other-tenant-id ten_other", step.group(1))
        self.assertIn("if: always()", step.group(1))
        result = re.search(r"(?ms)- name: Result .*", self.text)
        assert result
        self.assertIn("RECORD_ACCESS: ${{ steps.access.outputs.record_access }}", result.group(0))
        self.assertRegex(result.group(0), r'\[ "\$\{RECORD_ACCESS:-missing\}" = 0 \] \|\| \{[^\n]*fail=1')

    # BO's package inputs (row 12, noema#875): the scan's stamped inventory, the stamped list of the uploaded files, and the two compared before the run can pass.
    def step_run(self, step_id: str) -> str:
        body = re.search(r"(?ms)- id: " + step_id + r"\n(.*?)(?=^      - )", self.text)
        assert body, step_id
        run = re.search(r"(?ms)^        run: \|\n(.*)\Z", body.group(1))
        assert run, step_id
        return "\n".join(line[10:] for line in run.group(1).splitlines())

    def test_the_scan_writes_the_stamped_inventory_of_the_files_it_read(self) -> None:
        scan = self.step_run("scan")
        self.assertIn('--inventory benchmark/out/scan-inventory.txt --stamp "$GITHUB_SHA $GITHUB_RUN_ID" --root benchmark/out', scan)
        self.assertIn("tee -a benchmark/out/leak-scan.txt", scan)

    LAYOUT = ("run/results/a.json", "score/scorecard.json", "evidence/negative-probes.json", "service-logs/core.log", "joblog/runner.log", "mock-case.log", "mock-case-collector.err", "summary.md", "record-accessed.txt")

    def run_files_step(self, inventory_names: tuple[str, ...] | None, present: tuple[str, ...] = LAYOUT) -> tuple[subprocess.CompletedProcess[str], Path, str]:
        script = self.step_run("files")
        tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, tmp, True)
        out = tmp / "benchmark" / "out"
        for name in present:
            (out / name).parent.mkdir(parents=True, exist_ok=True)
            (out / name).write_text("x\n", encoding="utf-8")
        (out / "run" / ".hidden").write_text("x\n", encoding="utf-8")  # the scanner reads a hidden file, so the artifact must carry it (include-hidden-files) and the list must name it
        if inventory_names is not None:
            (out / "scan-inventory.txt").write_text("stamp: abc 123\n" + "".join(n + "\n" for n in inventory_names), encoding="utf-8")
        gh = tmp / "gh_output"
        r = subprocess.run(["bash", "-c", script], cwd=tmp, capture_output=True, text=True, check=False,
                           env={"PATH": os.environ["PATH"], "GITHUB_SHA": "abc", "GITHUB_RUN_ID": "123", "GITHUB_OUTPUT": str(gh), "MODE": "full"})
        return r, out, gh.read_text(encoding="utf-8") if gh.exists() else ""

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_the_artifact_file_list_is_stamped_lists_what_is_uploaded_and_matches_the_inventory(self) -> None:
        r, out, gh = self.run_files_step(self.LAYOUT)
        self.assertEqual(r.returncode, 0, r.stderr)
        listed = (out / "artifact-files.txt").read_text(encoding="utf-8").splitlines()
        self.assertEqual(listed[0], "stamp: abc 123")
        self.assertEqual(sorted(listed[1:]), sorted(self.LAYOUT + ("run/.hidden",)))
        self.assertIn("files_match=1", gh, "the inventory in this case lacks the hidden file the scanner would have read")
        r2, out2, gh2 = self.run_files_step(self.LAYOUT + ("run/.hidden",))
        self.assertIn("files_match=0", gh2)

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_a_file_scanned_but_not_uploaded_or_uploaded_but_not_scanned_fails_the_match(self) -> None:
        missing = tuple(n for n in self.LAYOUT if n != "summary.md")
        self.assertIn("files_match=1", self.run_files_step(missing)[2], "uploaded but not scanned")
        self.assertIn("files_match=1", self.run_files_step(self.LAYOUT + ("run/results/ghost.json",))[2], "scanned but not uploaded")
        self.assertIn("files_match=1", self.run_files_step(None)[2], "no inventory at all is not a match")

    @unittest.skipIf(shutil.which("bash") is None, "bash is needed to run the workflow's text")
    def test_a_stack_check_has_no_access_file_and_still_matches(self) -> None:
        without = tuple(n for n in self.LAYOUT if n != "record-accessed.txt")
        r, _, gh = self.run_files_step(without + ("run/.hidden",), present=without)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("files_match=0", gh)

    def test_the_artifact_holds_exactly_the_scanned_files_and_the_three_stamped_outputs_go_in_a_second_one(self) -> None:
        uploads = re.findall(r"(?ms)- name: Upload (?:the artifacts|the stamped evidence)[^\n]*\n(.*?)(?=^      - |\Z)", self.text)
        assert len(uploads) == 2, len(uploads)
        scanned, stamped = uploads
        for text in uploads:
            self.assertIn("if: always() && steps.scan.outputs.scan == '0'", text)
        self.assertIn("name: bench-g1-stack-${{ github.run_id }}\n", scanned)
        self.assertIn("include-hidden-files: true", scanned, "the scanner reads hidden files, so they must be uploaded or the two sets differ")
        self.assertIn("benchmark/out/summary.md", scanned, "the summary is scanned, so it is uploaded: the two sets are the same set (row 12)")
        for out_of_scan in ("leak-scan.txt", "scan-inventory.txt", "artifact-files.txt"):
            self.assertNotIn(out_of_scan, scanned, f"{out_of_scan} is written by or after the scan: it cannot be in the scanned artifact")
            self.assertIn("benchmark/out/" + out_of_scan, stamped)
        self.assertIn("name: bench-g1-stack-evidence-${{ github.run_id }}", stamped)
        self.assertEqual(len(re.findall(r"benchmark/out/\S+", stamped.split("path: |", 1)[1].split("if-no-files-found", 1)[0])), 3)

    def test_the_result_fails_unless_the_two_sets_match(self) -> None:
        result = re.search(r"(?ms)- name: Result .*?(?=^\S|\Z)", self.text)
        assert result
        self.assertIn("FILES_MATCH: ${{ steps.files.outputs.files_match }}", result.group(0))
        self.assertIn('[ "${FILES_MATCH:-missing}" = 0 ]', result.group(0))

    def test_the_summary_step_succeeds_in_stack_check_mode_where_no_run_files_exist(self) -> None:
        # Found by the first stack-check run on main (23 Nov): the last line of the Summary group was `[ -f score/scorecard.json ] && {...}`, which exits 1 when the file is absent, and a stack-check
        # run has none, so the step failed a healthy stack. The step is run here, as written, in an empty output directory.
        step = re.search(r"(?ms)- name: Summary\n.*?        run: \|\n(.*?)(?=^      # |^      - name:)", self.text)
        assert step, "no Summary step"
        script = "\n".join(line[10:] for line in step.group(1).splitlines())
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, "benchmark", "out").mkdir(parents=True)
            env = {"PATH": os.environ.get("PATH", ""), "MODE": "stack-check", "CODE": "0", "RUN_ID": "g1-stack-ghtest", "READYZ": "503", "NETCHECK": "0"}
            r = subprocess.run(["bash", "-e", "-c", script], cwd=tmp, env=env, capture_output=True, text=True, check=False)
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertIn("STACK-CHECK", Path(tmp, "benchmark", "out", "summary.md").read_text(encoding="utf-8"))

    def test_the_stack_check_keeps_the_readyz_answer_that_names_every_missing_component(self) -> None:
        # /readyz is 503 until every component is built and names each one that is not (names only, never a value: services/core/cmd/core/compose.go). The status alone told nobody what was missing,
        # so the first line is the status and the rest is the body, capped, in the run output that is shown only after a clean leak scan.
        block = re.search(r'(?s)if \[ "\$MODE" = stack-check \]; then(.*?)\n          else\n', self.script)
        assert block
        self.assertIn("print(r.status)", block.group(1))
        self.assertRegex(block.group(1), r"r\.read\(\d+\)")
        self.assertIn('readyz="$(head -n 1 benchmark/out/joblog/readyz.log)"', block.group(1))

    def test_the_job_log_scan_shows_why_the_log_could_not_be_read(self) -> None:
        # The first two stack-check runs ended with "the job log could not be read" after 60 s and no reason: the read's stderr went to /dev/null. gh prints an HTTP status line and no log text
        # on a failure, so it is kept in a file and shown when the read fails.
        job = re.search(r"(?ms)^  joblog-scan:\n(.*)", self.text)
        assert job
        read = re.search(r"gh api [^\n]*actions/jobs/\$id/logs\"[^\n]*", job.group(1))
        assert read, "the log read was not found"
        self.assertNotIn("2>/dev/null", read.group(0))
        # The reason the first three runs could not read the log (run 45127413024): gh refuses to print a response with terminal escape sequences, and a job log has them. The scanner must see them.
        self.assertIn("--allow-escape-sequences", read.group(0))
        self.assertIn("joblog-read.err", job.group(1))
        self.assertRegex(job.group(1), r"head -c \d+ \"?\$RUNNER_TEMP/joblog-read\.err")

    def test_the_job_log_scan_uses_its_own_allow_file_and_not_the_artifact_one(self) -> None:
        # Run 45128374794: the first scan that could read the log failed on a Go pseudo-version in a `go: downloading` line (a false card number). Only that line shape is allowed, in a file of its own,
        # so the artifact scans keep the standing allow file and the job-log scan keeps every other rule.
        job = re.search(r"(?ms)^  joblog-scan:\n(.*)", self.text)
        assert job
        scan = re.search(r"python3 -I tools/ci/g1_leakscan\.py[^\n]*g1-stack\.log\"", job.group(1))
        assert scan
        self.assertIn("--allow tools/ci/g1_joblog.allow", scan.group(0))
        self.assertNotIn("g1_leakscan.allow", scan.group(0))


if __name__ == "__main__":
    unittest.main()
