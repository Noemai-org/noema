"""The command: refuses the held-out set, reports 'not run: no access' rather than faking a run, and exits by what happened."""
import argparse
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

    def test_the_idp_session_provider_needs_all_of_its_settings(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            for partial in (["--idp-key", d], ["--idp-key", d, "--idp-binary", d], ["--idp-key", d, "--idp-binary", d, "--idp-kid", "k"],
                            ["--idp-key", d, "--idp-binary", d, "--idp-kid", "k", "--idp-audience", "a"]):  # the last one lacks --tenant-id
                code, out = run("--suite", str(suite_dir()), "--out", str(Path(d) / "o"), *partial)
                self.assertEqual(code, 2, partial)
                self.assertIn("IdP", out)
            code, out = run("--suite", str(suite_dir()), "--out", str(Path(d) / "o"), "--idp-key", d, "--tokens-dir", d, "--idp-binary", d,
                            "--idp-kid", "k", "--idp-audience", "a", "--tenant-id", "t")
            self.assertEqual(code, 2)  # a token directory and the IdP are two sources for one thing
            self.assertIn("one", out)

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

    def test_every_missing_counter_source_is_named_in_the_reason(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            code, out = run("--suite", str(suite_dir()), "--out", str(Path(d) / "o"), "--tokens-dir", d)
            self.assertEqual(code, 3)
            for counter in ("oracle_accesses_by_runtime", "model_governance_changes", "monetary_calls"):
                self.assertIn(counter, out)

    def test_help_names_every_option(self) -> None:
        with self.assertRaises(SystemExit):
            main(["--help"])



class OnlyFile(unittest.TestCase):
    """BEN-29: `--only-file` reads case ids, one per line, beside `--only`."""

    def test_ids_come_from_the_file_and_from_only(self) -> None:
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import only_ids
        with tempfile.TemporaryDirectory() as tmp:
            f = Path(tmp) / "ids.txt"
            f.write_text("BEN-CLEAN-0001\nBEN-TMO-0001\n\n")
            self.assertEqual({"BEN-CLEAN-0001", "BEN-TMO-0001", "BEN-AMB-0001"}, only_ids(["BEN-AMB-0001"], f))
            self.assertEqual({"BEN-CLEAN-0001", "BEN-TMO-0001"}, only_ids(None, f))
            self.assertIsNone(only_ids(None, None))

    def test_a_malformed_id_in_the_file_is_refused(self) -> None:
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import only_ids
        with tempfile.TemporaryDirectory() as tmp:
            f = Path(tmp) / "ids.txt"
            f.write_text("not-a-case\n")
            with self.assertRaises(ValueError):
                only_ids(None, f)


class EmptySelection(unittest.TestCase):
    """Codex P2 on noema#117: an empty or blank `--only-file` returned an empty set, so the runner executed zero cases and still wrote COMPLETE and exited 0. A failed selection step
    must never look like a successful benchmark run."""

    def only(self, text: str) -> object:
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import only_ids
        with tempfile.TemporaryDirectory() as tmp:
            f = Path(tmp) / "ids.txt"
            f.write_text(text)
            return only_ids(None, f)

    def test_an_empty_file_is_refused(self) -> None:
        with self.assertRaisesRegex(ValueError, "no case id"):
            self.only("")

    def test_a_file_of_blank_lines_is_refused(self) -> None:
        with self.assertRaisesRegex(ValueError, "no case id"):
            self.only("\n  \n\t\n")

    def test_only_and_a_blank_file_together_still_select_the_only_ids(self) -> None:
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import only_ids
        with tempfile.TemporaryDirectory() as tmp:
            f = Path(tmp) / "ids.txt"
            f.write_text("\n")
            self.assertEqual({"BEN-CLEAN-0001"}, only_ids(["BEN-CLEAN-0001"], f))

    def test_the_command_exits_two_and_writes_no_manifest_for_an_empty_selection(self) -> None:
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import main
        with tempfile.TemporaryDirectory() as tmp:
            ids = Path(tmp) / "ids.txt"
            ids.write_text("\n")
            out = io.StringIO()
            with redirect_stdout(out):
                code = main(["--suite", str(suite_dir()), "--out", str(Path(tmp) / "out"), "--only-file", str(ids)])
            self.assertEqual(2, code)
            self.assertIn("no case id", out.getvalue())
            self.assertFalse((Path(tmp) / "out" / "run-manifest.json").exists())


class StandInWiring(unittest.TestCase):
    """Option B for G1: `--standin-url` (with the bench credential) replaces the missing fixture loader with the Noema-side stand-in's."""

    def reason(self, *extra: str) -> str:
        import json
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import main
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "out"
            cred = Path(tmp) / "cred"
            cred.write_text("bench-credential\n")
            with redirect_stdout(io.StringIO()):
                code = main(["--suite", str(suite_dir()), "--out", str(out), "--bench-credential-file", str(cred), "--only", "BEN-CLEAN-0001", *extra])
            self.assertEqual(3, code)
            return str(json.loads((out / "run-manifest.json").read_text())["not_run_reason"])

    def test_a_stand_in_url_without_the_bench_credential_is_a_configuration_error_not_a_silent_fallback(self) -> None:
        # Codex review of noema#161: an incomplete stand-in configuration must not silently become "no loader".
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import main
        with tempfile.TemporaryDirectory() as tmp:
            out = io.StringIO()
            with redirect_stdout(out):
                code = main(["--suite", str(suite_dir()), "--out", str(Path(tmp) / "out"), "--only", "BEN-CLEAN-0001", "--standin-url", "http://127.0.0.1:9"])
            self.assertEqual(2, code)
            self.assertIn("--bench-credential-file", out.getvalue())
            self.assertFalse((Path(tmp) / "out" / "run-manifest.json").exists())

    def test_without_a_stand_in_the_fixture_loader_is_the_named_gap(self) -> None:
        self.assertIn("fixture loader", self.reason())

    def test_with_a_stand_in_the_fixture_loader_is_no_longer_a_gap_and_the_others_still_are(self) -> None:
        reason = self.reason("--standin-url", "http://127.0.0.1:9")
        self.assertNotIn("fixture loader", reason)
        self.assertIn("session provider", reason)


class ProxyLogWiring(unittest.TestCase):
    """monetary_calls source (a): `--proxy-url` (with the bench credential) replaces the missing monetary source; `--declare-no-monetary-route` is the claim it is the alternative to."""

    def reason(self, *extra: str) -> str:
        import json
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import main
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "out"
            cred = Path(tmp) / "cred"
            cred.write_text("bench-credential\n")
            with redirect_stdout(io.StringIO()):
                code = main(["--suite", str(suite_dir()), "--out", str(out), "--bench-credential-file", str(cred), "--only", "BEN-CLEAN-0001", *extra])
            self.assertEqual(3, code)
            return str(json.loads((out / "run-manifest.json").read_text())["not_run_reason"])

    def test_a_proxy_url_without_the_bench_credential_is_a_configuration_error_not_a_silent_fallback(self) -> None:
        # Same rule as --standin-url (Codex review of noema#161): an incomplete configuration must not silently become "no source".
        import tempfile
        from pathlib import Path

        from benchmark.runner.__main__ import main
        with tempfile.TemporaryDirectory() as tmp:
            out = io.StringIO()
            with redirect_stdout(out):
                code = main(["--suite", str(suite_dir()), "--out", str(Path(tmp) / "out"), "--only", "BEN-CLEAN-0001", "--proxy-url", "http://127.0.0.1:9"])
            self.assertEqual(2, code)
            self.assertIn("--bench-credential-file", out.getvalue())

    def test_without_a_source_monetary_calls_is_the_named_gap(self) -> None:
        self.assertIn("monetary_calls", self.reason())

    def test_with_the_proxy_log_monetary_calls_is_no_longer_a_gap(self) -> None:
        self.assertNotIn("monetary_calls", self.reason("--proxy-url", "http://127.0.0.1:9"))


class PerServiceCredentials(unittest.TestCase):
    """G1 stack (bench-runner): mock-case, the stand-in and the hostility proxy each generate their own bench credential, so one `--bench-credential-file` cannot serve all three.
    `--standin-credential-file` and `--proxy-credential-file` name theirs; each defaults to `--bench-credential-file`, so a single-credential run is unchanged."""

    def picked(self, *flags: str) -> tuple[str, str]:
        from pathlib import Path

        from benchmark.runner.__main__ import service_credentials
        args = argparse.Namespace(standin_credential_file=None, proxy_credential_file=None, bench_credential_file=None)
        for flag, value in zip(flags[::2], flags[1::2]):
            setattr(args, flag, Path(value))
        standin, proxy = service_credentials(args)
        return str(standin), str(proxy)

    def test_each_service_gets_its_own_credential_file_when_named(self) -> None:
        self.assertEqual(("standin", "proxy"), self.picked("bench_credential_file", "bench", "standin_credential_file", "standin", "proxy_credential_file", "proxy"))

    def test_without_the_per_service_flags_the_single_bench_credential_is_used_as_before(self) -> None:
        self.assertEqual(("bench", "bench"), self.picked("bench_credential_file", "bench"))

    def test_a_service_credential_alone_is_enough_for_that_service(self) -> None:
        self.assertEqual(("standin", "proxy"), self.picked("standin_credential_file", "standin", "proxy_credential_file", "proxy"))

    def test_one_service_credential_does_not_replace_the_other_services(self) -> None:
        self.assertEqual(("standin", "bench"), self.picked("bench_credential_file", "bench", "standin_credential_file", "standin"))


if __name__ == "__main__":
    unittest.main()
