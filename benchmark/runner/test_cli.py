"""The command: refuses the held-out set, reports 'not run: no access' rather than faking a run, and exits by what happened."""
import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

from benchmark.runner.__main__ import main
from benchmark.runner.test_suite import suite_dir


def run(*args: str) -> tuple[int, str]:
    out = io.StringIO()
    with redirect_stdout(out):
        code = main(list(args))
    return code, out.getvalue()


class Cli(unittest.TestCase):
    def test_any_argument_naming_the_heldout_set_is_refused(self) -> None:
        code, out = run("--suite", "/x/benchmark-heldout/suite", "--out", "/tmp/o")
        self.assertEqual(code, 2)
        self.assertIn("benchmark-heldout", out)

    def test_a_missing_suite_is_an_input_error(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            code, _ = run("--suite", str(Path(d) / "nope"), "--out", str(Path(d) / "o"))
            self.assertEqual(code, 2)

    def test_without_a_fixture_loader_the_run_is_not_run_and_says_why(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            cred = Path(d) / "bench.cred"
            cred.write_text("c" * 40)
            log = Path(d) / "m.log"
            log.write_text("")
            code, out = run("--suite", str(suite_dir()), "--out", str(Path(d) / "o"), "--api-url", "http://127.0.0.1:9", "--bench-url", "http://127.0.0.1:9",
                            "--bench-credential-file", str(cred), "--mock-case-log", str(log), "--model-writer", "MODEL", "--tokens-dir", d,
                            "--declare-no-monetary-route", "mock-case only: no monetary route exists")
            self.assertEqual(code, 3)
            self.assertIn("not run: no access", out)
            self.assertIn("fixture loader", out)
            manifest = json.loads((Path(d) / "o" / "run-manifest.json").read_text())
            self.assertEqual(manifest["status"], "NOT_RUN")
            self.assertEqual(manifest["counts"]["total"], 320)

    def test_help_names_every_option(self) -> None:
        with self.assertRaises(SystemExit):
            main(["--help"])


if __name__ == "__main__":
    unittest.main()
