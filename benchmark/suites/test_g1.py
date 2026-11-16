"""BEN-05 pre-stage / BEN-29 (Codex review of noema#117): the G1 evidence run keeps every failure. A leg that reports an incomplete run or a Tier 0 gate not at zero
(exit 1) must still fail the whole run, even when the replay diff then says IDENTICAL. Written before `g1.py` and seen failing. SYNTHETIC."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.suites.g1 import accept_named_exclusions, combine, evidence_record, run_g1


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


EXCLUDED = {"BEN-CLEAN-1007": {"reason": "needs INJECTION_PAYLOAD, not wired", "held_by": "Tushe", "opened": "2026-11-01"}}


def leg_with(root: Path, *, errored: list[str] | None = None, missing: list[str] | None = None, tier0: bool = True, complete: bool | None = None) -> Path:
    incomplete = {"missing": missing or [], "errored": errored or [], "stale_version": [], "duplicate": [], "unknown": [], "invalid_case": []}
    card = {"complete": complete if complete is not None else not any(incomplete.values()), "incomplete": incomplete, "tier0": {"all_zero": tier0}}
    (root / "score").mkdir(parents=True)
    (root / "score" / "scorecard.json").write_text(json.dumps(card), encoding="utf-8")
    return root


class NamedExclusions(unittest.TestCase):
    """Codex P2 on noema#117: BEN-CLEAN-1007 errors by design (INJECTION_PAYLOAD is not wired), so a leg whose only error is a registry-named exclusion is not a failed leg.
    Any other error, and any Tier 0 gate not at zero, still fails it."""

    def accepted(self, **kwargs: Any) -> int:
        with tempfile.TemporaryDirectory() as tmp:
            return accept_named_exclusions(leg_with(Path(tmp) / "a", **kwargs), 1, EXCLUDED)

    def test_the_only_error_is_the_named_exclusion_the_leg_is_accepted(self) -> None:
        self.assertEqual(0, self.accepted(errored=["BEN-CLEAN-1007"]))

    def test_another_error_beside_the_exclusion_still_fails(self) -> None:
        self.assertEqual(1, self.accepted(errored=["BEN-CLEAN-1007", "BEN-CLEAN-0001"]))

    def test_an_error_on_a_case_that_is_not_named_fails(self) -> None:
        self.assertEqual(1, self.accepted(errored=["BEN-CLEAN-0001"]))

    def test_a_missing_result_for_the_excluded_case_is_not_the_exclusion(self) -> None:
        self.assertEqual(1, self.accepted(missing=["BEN-CLEAN-1007"]))

    def test_a_tier_zero_gate_not_at_zero_still_fails(self) -> None:
        self.assertEqual(1, self.accepted(errored=["BEN-CLEAN-1007"], tier0=False))

    def test_other_incomplete_reasons_beside_the_exclusion_still_fail(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            leg = leg_with(Path(tmp) / "a", errored=["BEN-CLEAN-1007"])
            card = json.loads((leg / "score" / "scorecard.json").read_text())
            card["incomplete"]["duplicate"] = ["BEN-CLEAN-0014"]
            (leg / "score" / "scorecard.json").write_text(json.dumps(card))
            self.assertEqual(1, accept_named_exclusions(leg, 1, EXCLUDED))

    def test_no_scorecard_or_an_unreadable_one_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(1, accept_named_exclusions(Path(tmp) / "none", 1, EXCLUDED))
            leg = Path(tmp) / "bad"
            (leg / "score").mkdir(parents=True)
            (leg / "score" / "scorecard.json").write_text("{not json")
            self.assertEqual(1, accept_named_exclusions(leg, 1, EXCLUDED))

    def test_only_exit_one_is_ever_changed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            leg = leg_with(Path(tmp) / "a", errored=["BEN-CLEAN-1007"])
            for code in (0, 2, 3):
                self.assertEqual(code, accept_named_exclusions(leg, code, EXCLUDED))

    def test_a_complete_scorecard_with_exit_one_is_not_rescued_by_the_registry(self) -> None:
        self.assertEqual(1, self.accepted(complete=True, tier0=False))


REPORT = {"verdict": "IDENTICAL", "full_coverage": False, "case_coverage": {"covered": 5, "of": 6}, "open_exclusions": [{"case_id": "BEN-CLEAN-1007"}],
          "first": {"run_id": "g1-x-a"}, "second": {"run_id": "g1-x-b"}, "commit_sha": "a" * 40, "seed": 20261020}


class Evidence(unittest.TestCase):
    """Option B for G1 (Founder, 4 Nov 2026): the evidence is labelled with its data source, so stand-in evidence is never read as Tushe evidence."""

    def test_a_stand_in_run_says_so_in_the_headline_and_says_it_is_not_about_tushe(self) -> None:
        record = evidence_record("stand-in", "g1-x", 0, REPORT)
        self.assertEqual("stand-in", record["data_source"])
        self.assertIn("STAND-IN", record["headline"])
        self.assertIn("not Tushe", record["headline"])
        self.assertIn("IDENTICAL on 5 of 6", record["headline"])
        self.assertIn("1 open exclusion", record["headline"])
        self.assertTrue(record["rerun_against_tushe_required"])

    def test_a_tushe_run_does_not_carry_the_stand_in_warning(self) -> None:
        record = evidence_record("tushe", "g1-x", 0, REPORT)
        self.assertNotIn("STAND-IN", record["headline"])
        self.assertFalse(record["rerun_against_tushe_required"])

    def test_a_run_that_did_not_run_is_not_evidence_whatever_the_label(self) -> None:
        record = evidence_record("stand-in", "g1-x", 3, None)
        self.assertIn("NOT RUN", record["headline"])
        self.assertFalse(record["is_evidence"])
        self.assertIsNone(record["replay_verdict"])

    def test_a_failed_leg_is_not_evidence_even_with_an_identical_replay(self) -> None:
        record = evidence_record("stand-in", "g1-x", 1, REPORT)
        self.assertFalse(record["is_evidence"])
        self.assertEqual("IDENTICAL", record["replay_verdict"])

    def test_only_a_clean_identical_run_is_evidence_and_it_names_both_run_ids_the_commit_and_the_seed(self) -> None:
        record = evidence_record("stand-in", "g1-x", 0, REPORT)
        self.assertTrue(record["is_evidence"])
        self.assertEqual(["g1-x-a", "g1-x-b"], record["run_ids"])
        self.assertEqual(("a" * 40, 20261020, False), (record["commit_sha"], record["seed"], record["full_coverage"]))

    def test_a_replay_that_is_not_identical_is_not_evidence(self) -> None:
        self.assertFalse(evidence_record("stand-in", "g1-x", 1, {**REPORT, "verdict": "DIFFERENT"})["is_evidence"])

    def test_an_unknown_label_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            evidence_record("mock", "g1-x", 0, REPORT)


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


class NamedExclusionEndToEnd(unittest.TestCase):
    """Codex P2 on noema#117: five clean cases plus the registry-named exclusion BEN-CLEAN-1007 must produce G1 evidence when replay is IDENTICAL, and nothing else may: any other error,
    a Tier 0 gate not at zero or a replay that is not identical is still NOT EVIDENCE. This runs the whole verdict (`accept_named_exclusions`, `run_g1`, `evidence_record`)."""

    def verdict(self, **leg_kwargs: object) -> dict:  # type: ignore[type-arg]
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)

            def run_leg(leg: str, leg_out: Path) -> int:
                return accept_named_exclusions(leg_with(root / leg, **leg_kwargs), 1, EXCLUDED)  # type: ignore[arg-type]

            code = run_g1(root, run_leg, lambda a, b, o: 0, None)
            return evidence_record("stand-in", "g1-x", code, REPORT)

    def test_five_clean_cases_and_the_named_exclusion_with_an_identical_replay_is_evidence(self) -> None:
        record = self.verdict(errored=["BEN-CLEAN-1007"])
        self.assertTrue(record["is_evidence"], record)
        self.assertEqual(0, record["exit_code"])

    def test_another_errored_case_beside_the_exclusion_is_not_evidence(self) -> None:
        self.assertFalse(self.verdict(errored=["BEN-CLEAN-1007", "BEN-CLEAN-0001"])["is_evidence"])

    def test_the_named_exclusion_with_a_tier_zero_gate_not_at_zero_is_not_evidence(self) -> None:
        self.assertFalse(self.verdict(errored=["BEN-CLEAN-1007"], tier0=False)["is_evidence"])

    def test_the_named_exclusion_missing_instead_of_errored_is_not_evidence(self) -> None:
        self.assertFalse(self.verdict(missing=["BEN-CLEAN-1007"])["is_evidence"])
