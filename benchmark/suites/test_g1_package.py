"""BEN-05 (G1 evidence package, noema#566 section 4): the package skeleton, rendered in minutes after the run from the run's own files. Written before `g1_package.py` and seen failing.
SYNTHETIC inputs. The renderer never says MET for anything it cannot tie to a link the Benchmark Owner opened and says it read; rows 5, 10 and 11 are computed from the run, not asserted;
the headline never drops the stand-in label (Founder, 4 Nov 2026). It reads no oracle (INV-22)."""
import copy
import unittest
from collections.abc import Callable
from typing import Any

from benchmark.suites.g1_package import ROWS, PackageError, render

Json = dict[str, Any]
SHA = "a" * 40
URL = "https://github.com/Noemai-org/noema/actions/runs/1"


def inputs() -> Json:
    return {
        "dispatch_sha": SHA, "run_id": "g1-20261120", "fault_plan_id": "plan_1", "data_label": "stand-in", "us06_cases": ["BEN-TMO-0001"],
        "check_runs": {"head_sha": SHA, "all_success": True, "url": URL},
        "rows": {str(n): {"link": URL, "read_by_bo": True} for n in range(1, 13)},
        "not_tested": ["live Tushe (data is the stand-in)", "the model side is the replay provider"],
    }


def card(complete: bool = True, zero: bool = True) -> Json:
    return {"complete": complete, "tier0": {"all_zero": zero}}


def report(verdict: str = "IDENTICAL", excluded: list[str] | None = None, sha: str = SHA) -> Json:
    ex = excluded or []
    return {"verdict": verdict, "full_coverage": not ex, "case_coverage": {"covered": 6 - len(ex), "of": 6}, "open_exclusions": [{"case_id": c} for c in ex],
            "first": {"run_id": "g1-a"}, "second": {"run_id": "g1-b"}, "commit_sha": sha, "seed": 20261020}


def make(i: Json | None = None, a: Json | None = None, b: Json | None = None, r: Json | None = None) -> str:
    return render(i or inputs(), a or card(), b or card(), r or report())


def row(text: str, n: int) -> str:
    return next(line for line in text.splitlines() if line.startswith(f"| {n} |"))


class Rows(unittest.TestCase):
    def test_a_complete_clean_run_marks_all_twelve_met_and_says_g1_met(self) -> None:
        text = make()
        for n in range(1, 13):
            self.assertIn("**MET**", row(text, n), n)
        self.assertIn("G1: MET", text)
        self.assertEqual(12, len(ROWS))

    def test_the_stand_in_label_is_in_the_headline_and_demands_a_rerun_against_tushe(self) -> None:
        text = make()
        self.assertIn("STAND-IN data, not Tushe", text.splitlines()[0])
        self.assertIn("rerun against live Tushe", text)

    def test_a_row_without_a_link_or_without_the_owners_read_is_not_met(self) -> None:
        i = inputs()
        del i["rows"]["3"]["link"]
        i["rows"]["4"]["read_by_bo"] = False
        i["rows"]["7"]["link"] = "not a url"
        text = make(i)
        for n in (3, 4, 7):
            self.assertIn("**NOT MET**", row(text, n), n)
        self.assertIn("G1: NOT MET", text)

    def test_a_missing_row_is_not_met_never_skipped(self) -> None:
        i = inputs()
        del i["rows"]["9"]
        self.assertIn("**NOT MET**", row(make(i), 9))


class ComputedRows(unittest.TestCase):
    def test_row_5_needs_one_final_head_with_every_check_run_green_at_that_head(self) -> None:
        cases: list[Callable[[Json], Any]] = [lambda i: i["check_runs"].update(head_sha="b" * 40), lambda i: i["check_runs"].update(all_success=False), lambda i: i.update(dispatch_sha="short")]
        for mutate in cases:
            i = inputs()
            mutate(i)
            self.assertIn("**NOT MET**", row(make(i), 5))
        self.assertIn("**NOT MET**", row(make(r=report(sha="c" * 40)), 5))  # the run was made at another head
        short = inputs()
        short.update(dispatch_sha="abc123")
        short["check_runs"]["head_sha"] = "abc123"
        self.assertIn("**NOT MET**", row(make(short, r=report(sha="abc123")), 5))  # all three agree, but it is not a full SHA

    def test_row_10_needs_both_legs_complete_with_tier_0_at_zero(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=card(complete=False)), 10))
        self.assertIn("**NOT MET**", row(make(b=card(zero=False)), 10))
        self.assertIn("**MET**", row(make(), 10))

    def test_row_11_needs_an_identical_replay_and_the_unknown_outcome_case_inside_it(self) -> None:
        self.assertIn("**NOT MET**", row(make(r=report("DIFFERENT")), 11))
        self.assertIn("**NOT MET**", row(make(r=report("NOT_COMPARABLE")), 11))
        self.assertIn("**NOT MET**", row(make(r=report(excluded=["BEN-TMO-0001"])), 11))
        other = make(r=report(excluded=["BEN-CLEAN-1007"]))
        self.assertIn("**MET**", row(other, 11))
        self.assertIn("BEN-CLEAN-1007", other)  # an excluded case is named in the package

    def test_a_hand_written_met_cannot_override_a_computed_row(self) -> None:
        i = inputs()
        i["rows"]["10"]["verdict"] = "MET"
        self.assertIn("**NOT MET**", row(make(i, a=card(zero=False)), 10))


class Whole(unittest.TestCase):
    def test_what_was_not_tested_is_required_and_listed(self) -> None:
        self.assertIn("## What was not tested", make())
        self.assertIn("the model side is the replay provider", make())
        i = inputs()
        i["not_tested"] = []
        self.assertIn("G1: NOT MET", make(i))
        self.assertIn("section is empty", make(i))

    def test_a_bad_input_is_an_error_not_a_quiet_default(self) -> None:
        bad: list[Callable[[Json], Any]] = [lambda i: i.update(data_label="live"), lambda i: i.pop("dispatch_sha"), lambda i: i.update(us06_cases=[]), lambda i: i.update(rows="x")]
        for mutate in bad:
            i = inputs()
            mutate(i)
            with self.assertRaises(PackageError):
                make(i)

    def test_it_is_deterministic_and_carries_ids_and_links_only(self) -> None:
        a, b = make(), make(copy.deepcopy(inputs()))
        self.assertEqual(a, b)
        self.assertIn("g1-20261120", a)
        self.assertIn(URL, a)
        self.assertIn("plan_1", a)
        self.assertNotIn("oracle", a.lower().replace("no oracle", ""))


if __name__ == "__main__":
    unittest.main()
