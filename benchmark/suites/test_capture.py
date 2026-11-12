"""Mock-case log capture for a G1 leg (REQ-ENV-002; the runner's RefusalLog needs a file that is streamed live and complete).

No Docker here: `popen` is injected. The real command line is checked as data, not run."""
import tempfile
import unittest
from collections.abc import Callable, Iterator
from pathlib import Path
from typing import Any

from benchmark.suites.capture import CaptureFailed, capture_command, mock_case_log, with_log_argument


class FakeProc:
    def __init__(self, dies_after_polls: int | None = None, returncode: int = 1) -> None:
        self.polls, self.dies, self.rc, self.terminated, self.out = 0, dies_after_polls, returncode, False, None

    def poll(self) -> int | None:
        self.polls += 1
        return self.rc if self.dies is not None and self.polls > self.dies else None

    def terminate(self) -> None:
        self.terminated = True

    def wait(self, timeout: float | None = None) -> int:
        return 0


class Capture(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.path = Path(self._tmp.name) / "leg" / "mock-case.log"
        self.seen: list[tuple[list[str], bool]] = []

    def popen(self, proc: FakeProc) -> Callable[..., FakeProc]:
        def make(cmd: list[str], stdout: Any = None, **_: Any) -> FakeProc:
            self.seen.append((cmd, self.path.exists()))
            proc.out = stdout
            return proc
        return make

    def test_the_log_file_exists_before_the_process_starts_so_the_runner_can_mark_it(self) -> None:
        proc = FakeProc()
        with mock_case_log(self.path, popen=self.popen(proc), sleep=lambda s: None):
            pass
        self.assertTrue(self.seen[0][1])
        self.assertTrue(proc.terminated)

    def test_the_command_follows_mock_case_without_a_prefix(self) -> None:
        cmd = capture_command()
        self.assertEqual(["docker", "compose"], cmd[:2])
        for part in ("--follow", "--no-log-prefix", "mock-case"):
            self.assertIn(part, cmd)

    def test_a_process_that_exits_at_start_is_a_failed_capture(self) -> None:
        with self.assertRaises(CaptureFailed):
            with mock_case_log(self.path, popen=self.popen(FakeProc(dies_after_polls=0)), sleep=lambda s: None):
                self.fail("the body must not run with no log")

    def test_a_process_that_dies_during_the_run_fails_the_leg_because_the_log_is_incomplete(self) -> None:
        with self.assertRaises(CaptureFailed):
            with mock_case_log(self.path, popen=self.popen(FakeProc(dies_after_polls=1)), sleep=lambda s: None):
                pass

    def test_a_missing_docker_is_a_failed_capture_not_a_crash(self) -> None:
        def missing(*a: Any, **k: Any) -> None:
            raise FileNotFoundError("docker")
        with self.assertRaises(CaptureFailed):
            with mock_case_log(self.path, popen=missing, sleep=lambda s: None):
                pass

    def test_the_log_argument_is_added_once_and_a_callers_own_is_kept(self) -> None:
        self.assertEqual(["--mock-case-log", "x"], with_log_argument([], Path("x")))
        self.assertEqual(["--mock-case-log", "mine"], with_log_argument(["--mock-case-log", "mine"], Path("x")))
        self.assertEqual(["--mock-case-log=mine"], with_log_argument(["--mock-case-log=mine"], Path("x")))


if __name__ == "__main__":
    unittest.main()



class Wiring(unittest.TestCase):
    """`g1` captures per leg and fails the leg as NOT RUN (exit 3) when it cannot. No mock library (AGENT-12): module attributes are swapped by hand and restored."""

    def swap(self, module: Any, name: str, value: Any) -> None:
        old = getattr(module, name)
        setattr(module, name, value)
        self.addCleanup(setattr, module, name, old)

    def run_g1_with(self, capture: Any, runner: Callable[[Any, list[str]], int] | None = None) -> tuple[int, list[list[str]], Path]:
        import io
        from contextlib import redirect_stderr, redirect_stdout

        from benchmark.runner.test_suite import suite_dir
        from benchmark.suites import __main__ as entry
        calls: list[list[str]] = []

        def fake_run(args: Any, passthrough: list[str]) -> int:
            calls.append(list(passthrough))
            return runner(args, passthrough) if runner else 0
        self.swap(entry, "mock_case_log", capture)
        self.swap(entry, "_run", fake_run)
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        out = Path(tmp.name) / "g1"
        with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
            code = entry.main(["g1", "--suite", str(suite_dir()), "--out", str(out), "--run-id", "w", "--data-source", "stand-in"])
        return code, calls, out

    @staticmethod
    def ok(path: Path, **_: Any) -> Iterator[Path]:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"")
        yield path

    def test_each_leg_passes_its_own_captured_log_to_the_runner(self) -> None:
        import contextlib
        _, calls, _ = self.run_g1_with(contextlib.contextmanager(self.ok))
        logs = [p[p.index("--mock-case-log") + 1] for p in calls]
        self.assertEqual(2, len(logs))
        self.assertNotEqual(logs[0], logs[1])
        self.assertTrue(all(l.endswith("mock-case.log") for l in logs))

    def test_the_active_log_is_outside_the_directory_the_leg_archives_and_survives_the_archive(self) -> None:
        """Codex P1 on noema#117: `_run` archives the leg directory, so a log created inside it was moved while capture kept writing to it and the runner got a path that no longer existed."""
        import contextlib

        from benchmark.suites.archive import archive_previous
        seen: list[tuple[Path, bool]] = []

        def archiving_run(args: Any, passthrough: list[str]) -> int:
            log = Path(passthrough[passthrough.index("--mock-case-log") + 1])
            (Path(args.out) / "leftover.txt").parent.mkdir(parents=True, exist_ok=True)
            (Path(args.out) / "leftover.txt").write_text("an earlier run")
            archive_previous(Path(args.out))
            seen.append((log, log.is_file()))
            return 0
        _, _, out = self.run_g1_with(contextlib.contextmanager(self.ok), archiving_run)
        self.assertEqual(2, len(seen))
        for log, survived in seen:
            self.assertTrue(survived, f"{log} was archived away")
            self.assertNotIn(out / "a", log.parents)
            self.assertNotIn(out / "b", log.parents)

    def test_a_capture_that_cannot_start_is_exit_3_and_the_runner_gets_no_log_so_it_records_not_run(self) -> None:
        import contextlib

        @contextlib.contextmanager
        def broken(path: Path, **_: Any) -> Iterator[Path]:
            raise CaptureFailed("no docker")
            yield path
        code, calls, _ = self.run_g1_with(broken)
        self.assertEqual(3, code)
        self.assertEqual(1, len(calls))
        self.assertNotIn("--mock-case-log", calls[0])

    def test_a_log_the_caller_supplies_skips_local_capture_entirely(self) -> None:
        """Codex P2 on noema#117: an externally captured log must work without the local Compose stack, in both spellings, and the runner gets it exactly once."""
        import contextlib
        from benchmark.runner.test_suite import suite_dir
        from benchmark.suites import __main__ as entry
        for spelling in (["--mock-case-log", "/outside/mock.log"], ["--mock-case-log=/outside/mock.log"]):
            started: list[Path] = []

            @contextlib.contextmanager
            def capture(path: Path, **_: Any) -> Iterator[Path]:
                started.append(path)
                raise CaptureFailed("no docker")
                yield path
            self.swap(entry, "mock_case_log", capture)
            calls: list[list[str]] = []
            self.swap(entry, "_run", lambda args, passthrough: calls.append(list(passthrough)) or 0)
            import io
            from contextlib import redirect_stderr, redirect_stdout
            with tempfile.TemporaryDirectory() as tmp, redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
                code = entry.main(["g1", "--suite", str(suite_dir()), "--out", str(Path(tmp) / "g1"), "--run-id", "w", "--data-source", "stand-in", "--", *spelling])
            self.assertEqual([], started, spelling)
            self.assertEqual(2, len(calls), spelling)
            for call in calls:
                self.assertEqual(spelling, call, "the caller's option is passed through once, unchanged")
            self.assertNotEqual(3, code)

    def test_a_capture_that_cannot_start_leaves_no_stray_log_path_in_the_fallback_call(self) -> None:
        import contextlib

        @contextlib.contextmanager
        def broken(path: Path, **_: Any) -> Iterator[Path]:
            raise CaptureFailed("no docker")
            yield path
        _, calls, _ = self.run_g1_with(broken)
        self.assertEqual([[]], [c for c in calls], "no log option and no leftover value")

    def test_a_leg_whose_only_error_is_the_named_exclusion_is_accepted_on_both_paths(self) -> None:
        """Codex P2 on noema#117, wired: `_g1` applies the registry to the runner's exit 1 whether the log is captured or supplied."""
        import contextlib
        import io
        import json
        from contextlib import redirect_stderr, redirect_stdout

        from benchmark.runner.test_suite import suite_dir
        from benchmark.suites import __main__ as entry

        def only_the_exclusion_errors(args: Any, passthrough: list[str]) -> int:
            (Path(args.out) / "score").mkdir(parents=True, exist_ok=True)
            card = {"complete": False, "tier0": {"all_zero": True},
                    "incomplete": {"missing": [], "errored": ["BEN-CLEAN-1007"], "stale_version": [], "duplicate": [], "unknown": [], "invalid_case": []}}
            (Path(args.out) / "score" / "scorecard.json").write_text(json.dumps(card), encoding="utf-8")
            return 1
        for extra in ([], ["--mock-case-log", "/outside/mock.log"]):
            self.swap(entry, "mock_case_log", contextlib.contextmanager(self.ok))
            self.swap(entry, "_run", only_the_exclusion_errors)
            out = io.StringIO()
            with tempfile.TemporaryDirectory() as tmp, redirect_stdout(out), redirect_stderr(io.StringIO()):
                entry.main(["g1", "--suite", str(suite_dir()), "--out", str(Path(tmp) / "g1"), "--run-id", "w", "--data-source", "stand-in", *(["--", *extra] if extra else [])])
            self.assertEqual(2, out.getvalue().count("exit 1 accepted"), extra)


class SuiteTargets(unittest.TestCase):
    """Codex P2 on noema#117: the suite targets must pass the cost directory the bench stack writes, or every case's cost is lost (REQ-NFR-007)."""

    def test_the_suite_targets_and_g1_pass_the_generated_cost_directory(self) -> None:
        makefile = (Path(__file__).resolve().parents[2] / "Makefile").read_text(encoding="utf-8")
        for target in ("bench-us bench-chaos bench-evil bench-full:", "bench-g1:"):
            start = makefile.index(target)
            recipe = makefile[start:makefile.index("\n\n", start)]
            self.assertIn("--cost-dir $(BENCH_OUT)/cost", recipe, target)
