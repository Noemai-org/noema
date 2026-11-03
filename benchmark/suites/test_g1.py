"""BEN-05 pre-stage / BEN-29 (Codex review of noema#117): the G1 evidence run keeps every failure. A leg that reports an incomplete run or a Tier 0 gate not at zero
(exit 1) must still fail the whole run, even when the replay diff then says IDENTICAL. Written before `g1.py` and seen failing. SYNTHETIC."""
import unittest
from pathlib import Path

from benchmark.suites.g1 import combine, run_g1


class Combine(unittest.TestCase):
    def test_all_clean_is_zero(self) -> None:
        self.assertEqual(0, combine([0, 0], 0))

    def test_a_leg_that_reports_exit_one_fails_the_run_even_when_the_replay_is_identical(self) -> None:
        self.assertEqual(1, combine([1, 0], 0))
        self.assertEqual(1, combine([0, 1], 0))
        self.assertEqual(1, combine([1, 1], 0))

    def test_a_replay_that_is_not_identical_fails_the_run(self) -> None:
        self.assertEqual(1, combine([0, 0], 1))

    def test_not_run_and_bad_input_win_over_exit_one(self) -> None:
        self.assertEqual(3, combine([3, 0], 0))
        self.assertEqual(2, combine([0, 2], 0))
        self.assertEqual(3, combine([1, 3], 0))

    def test_a_replay_input_error_is_kept(self) -> None:
        self.assertEqual(2, combine([0, 0], 2))
        self.assertEqual(2, combine([1, 1], 2))


class Run(unittest.TestCase):
    def fake(self, leg_codes: list[int], replay_code: int, log: list[str]) -> tuple[object, object, object]:
        codes = iter(leg_codes)

        def run_leg(leg: str, out: Path) -> int:
            log.append(f"leg {leg}")
            return next(codes)

        def run_replay(first: Path, second: Path, out: Path) -> int:
            log.append("replay")
            return replay_code

        def reset() -> int:
            log.append("reset")
            return 0
        return run_leg, run_replay, reset

    def go(self, leg_codes: list[int], replay_code: int = 0, do_reset: bool = True) -> tuple[int, list[str]]:
        log: list[str] = []
        run_leg, run_replay, reset = self.fake(leg_codes, replay_code, log)
        code = run_g1(Path("OUT"), run_leg, run_replay, reset if do_reset else None)  # type: ignore[arg-type]
        return code, log

    def test_each_leg_starts_from_a_reset_and_the_replay_comes_last(self) -> None:
        code, log = self.go([0, 0])
        self.assertEqual((0, ["reset", "leg a", "reset", "leg b", "replay"]), (code, log))

    def test_without_reset_nothing_is_reset(self) -> None:
        self.assertEqual(["leg a", "leg b", "replay"], self.go([0, 0], do_reset=False)[1])

    def test_a_leg_with_exit_one_still_writes_the_replay_report_and_the_run_fails(self) -> None:
        code, log = self.go([1, 0])
        self.assertEqual((1, "replay"), (code, log[-1]))

    def test_a_leg_that_did_not_run_stops_the_run_before_the_second_leg_and_the_replay(self) -> None:
        code, log = self.go([3, 0])
        self.assertEqual((3, ["reset", "leg a"]), (code, log))

    def test_a_failed_reset_stops_the_run(self) -> None:
        def reset() -> int:
            return 7
        self.assertEqual(7, run_g1(Path("OUT"), lambda leg, out: 0, lambda a, b, o: 0, reset))


if __name__ == "__main__":
    unittest.main()
