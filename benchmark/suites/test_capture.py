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
    """`g1` captures per leg and fails the leg as NOT RUN (exit 3) when it cannot."""

    def run_g1_with(self, capture: Any) -> tuple[int, Any]:
        import io
        from contextlib import redirect_stderr, redirect_stdout
        from unittest import mock

        from benchmark.runner.test_suite import suite_dir
        from benchmark.suites import __main__ as entry
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(entry, "mock_case_log", capture), mock.patch.object(entry, "_run", return_value=0) as run, \
                redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
            code = entry.main(["g1", "--suite", str(suite_dir()), "--out", tmp, "--run-id", "w", "--data-source", "stand-in"])
            return code, run

    def test_each_leg_passes_its_own_captured_log_to_the_runner(self) -> None:
        import contextlib

        @contextlib.contextmanager
        def ok(path: Path, **_: Any) -> Iterator[Path]:
            yield path
        _, run = self.run_g1_with(ok)
        passthroughs = [c.args[1] for c in run.call_args_list]
        logs = [p[p.index("--mock-case-log") + 1] for p in passthroughs]
        self.assertEqual(2, len(logs))
        self.assertNotEqual(logs[0], logs[1])
        self.assertTrue(all(l.endswith("mock-case.log") for l in logs))

    def test_a_capture_that_cannot_start_is_exit_3_and_the_runner_gets_no_log_so_it_records_not_run(self) -> None:
        import contextlib

        @contextlib.contextmanager
        def broken(path: Path, **_: Any) -> Iterator[Path]:
            raise CaptureFailed("no docker")
            yield path
        code, run = self.run_g1_with(broken)
        self.assertEqual(3, code)
        self.assertEqual(1, run.call_count)
        self.assertNotIn("--mock-case-log", run.call_args.args[1])
