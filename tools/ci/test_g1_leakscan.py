"""The G1 leak scan with a planted canary (tools/ci/g1_leakscan.py; SR criterion 8 on noema#436): a clean set passes only after the scanner proved it can see; a leak, a digit run of
5 or more and the canary itself in an artifact each fail; a blind scanner, or a scan that looked at nothing, is NOT RUN and never a pass."""
import io
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import g1_leakscan  # noqa: E402


def run(args: list[str]) -> tuple[int, str]:
    out = io.StringIO()
    with redirect_stdout(out):
        code = g1_leakscan.main(args)
    return code, out.getvalue()


class G1Leakscan(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.art = self.root / "artifacts"
        self.art.mkdir()
        (self.art / "run.log").write_text("case completed in 1234 ms with status OK\n", encoding="utf-8")
        self.work = self.root / "work"

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def test_a_clean_set_passes_after_the_scanner_proved_it_can_see(self) -> None:
        code, text = run(["--workdir", str(self.work), str(self.art)])
        self.assertEqual(code, 0, text)
        self.assertIn("the planted canary was found", text)

    def test_a_digit_run_of_five_or_more_fails(self) -> None:
        (self.art / "b.log").write_text("amount 123456 here\n", encoding="utf-8")
        code, text = run(["--workdir", str(self.work), str(self.art)])
        self.assertEqual(code, 1, text)
        self.assertIn("digit_run", text)

    def test_the_allow_file_lets_only_the_named_integer_fields_through(self) -> None:
        allow = HERE / "g1_leakscan.allow"
        fields = ['{', '  "seed": 20261103,', '  "cost_usd_micros": 326693,', '  "latency_ms": 12345', '}', '']
        (self.art / "evidence.json").write_text("\n".join(fields), encoding="utf-8")
        self.assertEqual(run(["--allow", str(allow), "--workdir", str(self.work), str(self.art)])[0], 0)
        self.assertEqual(run(["--workdir", str(self.root / "w2"), str(self.art)])[0], 1, "without the allow file the same fields are findings")
        (self.art / "other.json").write_text("\n".join(['{', '  "account_number": 1234567890', '}', '']), encoding="utf-8")
        code, text = run(["--allow", str(allow), "--workdir", str(self.root / "w3"), str(self.art)])
        self.assertEqual(code, 1, text)
        self.assertIn("account_number", text)  # a run of exactly 10 digits is reported as an account number

    def test_the_canary_in_an_artifact_fails_and_is_never_printed(self) -> None:
        # The canary is made inside the run, so the artifact cannot carry it by accident; plant it the way a leak would: by reading it from the work directory after the probe.
        real_scan = g1_leakscan.scan

        def leaking(scanner: Path, canary_file: Path, paths: list[Path], allow: Path | None = None) -> tuple[int, str]:
            if paths == [self.art.resolve()]:
                (self.art / "leak.log").write_text(canary_file.read_text(encoding="utf-8"), encoding="utf-8")
            return real_scan(scanner, canary_file, paths, allow)

        g1_leakscan.scan = leaking
        try:
            code, text = run(["--workdir", str(self.work), str(self.art)])
        finally:
            g1_leakscan.scan = real_scan
        self.assertEqual(code, 1, text)
        self.assertNotRegex(text, r"CANARY-[0-9a-f]{24}")

    def test_a_blind_scanner_is_not_run_and_never_clean(self) -> None:
        blind = self.root / "blind.py"
        blind.write_text("import sys\nprint('leakscan OK: scanned 1 files, 1 lines')\nsys.exit(0)\n", encoding="utf-8")
        code, text = run(["--scanner", str(blind), "--workdir", str(self.work), str(self.art)])
        self.assertEqual(code, 3, text)
        self.assertIn("NOT RUN", text)

    def test_a_scanner_that_fails_without_naming_a_canary_is_not_run(self) -> None:
        noisy = self.root / "noisy.py"
        noisy.write_text("import sys\nprint('leakscan FAIL: something')\nsys.exit(1)\n", encoding="utf-8")
        self.assertEqual(run(["--scanner", str(noisy), "--workdir", str(self.work), str(self.art)])[0], 3)

    def test_a_scan_that_looked_at_nothing_is_not_run(self) -> None:
        empty = self.root / "empty"
        empty.mkdir()
        code, text = run(["--workdir", str(self.work), str(empty)])
        self.assertEqual(code, 3, text)

    def test_a_missing_path_and_a_work_directory_inside_a_scanned_path_are_refused(self) -> None:
        self.assertEqual(run([str(self.root / "nope")])[0], 2)
        self.assertEqual(run(["--workdir", str(self.art / "work"), str(self.art)])[0], 2)

    def test_the_command_line_runs(self) -> None:
        done = subprocess.run([sys.executable, "-I", str(HERE / "g1_leakscan.py"), str(self.art)], capture_output=True, text=True, check=False)
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)


if __name__ == "__main__":
    unittest.main()
