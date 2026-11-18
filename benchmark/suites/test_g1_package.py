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


def card(complete: bool = True, zero: bool = True, errored: list[str] | None = None, other: str | None = None) -> Json:
    incomplete: Json = {"missing": [], "errored": errored or [], "stale_version": [], "duplicate": [], "unknown": [], "invalid_case": []}
    if other:
        incomplete[other] = ["BEN-CLEAN-0001"]
    return {"complete": complete, "tier0": {"all_zero": zero}, "incomplete": incomplete}


SIX = ["BEN-A-0001", "BEN-A-0002", "BEN-CLEAN-0001", "BEN-CLEAN-1007", "BEN-TMO-0001", "BEN-TMO-1002"]


def report(verdict: str = "IDENTICAL", excluded: list[str] | None = None, sha: str = SHA, cases: list[Json] | None = None) -> Json:
    ex = excluded or []
    rows = cases if cases is not None else [{"case_id": c, "result": "IDENTICAL"} for c in SIX if c not in ex]
    return {"verdict": verdict, "full_coverage": not ex, "case_coverage": {"covered": 6 - len(ex), "of": 6}, "open_exclusions": [{"case_id": c} for c in ex],
            "first": {"run_id": "g1-a"}, "second": {"run_id": "g1-b"}, "commit_sha": sha, "seed": 20261020, "cases": rows}


def leg(scorecard: Json, run_id: str) -> Json:
    """A scorecard of one replayed leg: it carries the leg's run id (scorecard.py)."""
    return {**scorecard, "run_id": run_id}


def make(i: Json | None = None, a: Json | None = None, b: Json | None = None, r: Json | None = None) -> str:
    a, b = a or card(), b or card()
    return render(i or inputs(), a if "run_id" in a else leg(a, "g1-a"), b if "run_id" in b else leg(b, "g1-b"), r or report())


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


class AttestationAndExclusions(unittest.TestCase):
    """Codex P2s on noema#717 (ruling A322): row 5 needs the owner's attestation like every row, and row 10 accepts the registry-named exclusion exactly as `g1.accept_named_exclusions` does."""

    def test_row_5_needs_the_owners_attestation_and_a_link_even_when_every_computed_check_passes(self) -> None:
        missing = inputs()
        del missing["rows"]["5"]
        unread = inputs()
        unread["rows"]["5"]["read_by_bo"] = False
        nolink = inputs()
        del nolink["rows"]["5"]["link"]
        for i in (missing, unread, nolink):
            text = make(i)
            self.assertIn("**NOT MET**", row(text, 5))
            self.assertIn("G1: NOT MET", text)

    def test_row_5_still_fails_a_computed_check_when_the_owner_attests(self) -> None:
        i = inputs()
        i["check_runs"]["all_success"] = False
        self.assertIn("**NOT MET**", row(make(i), 5))

    def test_a_leg_whose_only_incompleteness_is_a_named_exclusion_is_met(self) -> None:
        a, b = card(False, errored=["BEN-CLEAN-1007"]), card(False, errored=["BEN-CLEAN-1007"])
        text = make(a=a, b=b, r=report(excluded=["BEN-CLEAN-1007"]))
        self.assertIn("**MET**", row(text, 10))
        self.assertIn("G1: MET", text)

    def test_an_error_that_is_not_a_named_exclusion_is_not_met(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-1007", "BEN-CLEAN-0001"]), r=report(excluded=["BEN-CLEAN-1007"])), 10))
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-0001"]), r=report(excluded=["BEN-CLEAN-1007"])), 10))
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-1007"]), r=report()), 10), "the replay names no exclusion")

    def test_any_other_incomplete_reason_or_a_tier_0_gate_still_fails_even_with_the_exclusion(self) -> None:
        r = report(excluded=["BEN-CLEAN-1007"])
        for reason in ("missing", "stale_version", "duplicate", "unknown", "invalid_case"):
            self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-1007"], other=reason), r=r), 10), reason)
        self.assertIn("**NOT MET**", row(make(a=card(False, zero=False, errored=["BEN-CLEAN-1007"]), r=r), 10))
        self.assertIn("**NOT MET**", row(make(b=card(False, errored=["BEN-CLEAN-1007"], zero=False), r=r), 10))

    def test_the_unknown_outcome_case_is_never_an_accepted_exclusion(self) -> None:
        r = report(excluded=["BEN-TMO-0001"])
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-TMO-0001"]), r=r), 10))

    def test_an_incomplete_leg_with_no_errored_case_is_not_rescued(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=card(False), r=report(excluded=["BEN-CLEAN-1007"])), 10))

    def test_a_malformed_scorecard_fails_closed(self) -> None:
        for bad in ({"complete": False}, {"complete": False, "tier0": {"all_zero": True}}, {"complete": False, "tier0": {"all_zero": True}, "incomplete": {"errored": "BEN-CLEAN-1007"}}):
            self.assertIn("**NOT MET**", row(make(a=bad, r=report(excluded=["BEN-CLEAN-1007"])), 10))


    def test_a_scorecard_of_the_wrong_shape_is_not_met_and_never_raises(self) -> None:
        wrong: list[Any] = [{"complete": True, "tier0": None}, {"complete": True, "tier0": "zero"}, {}, [], None]
        for bad in wrong:
            self.assertIn("**NOT MET**", row(render(inputs(), leg(bad, "g1-a") if isinstance(bad, dict) else bad, leg(card(), "g1-b"), report()), 10))


class ReplayEvidence(unittest.TestCase):
    """Codex P2s on noema#717 at 088635bc: the replay must contain the unknown-outcome cases, and each scorecard must be the one of its replay leg."""

    def test_row_11_needs_every_unknown_outcome_case_in_the_replay_with_an_identical_result(self) -> None:
        limited = report(cases=[{"case_id": "BEN-CLEAN-0001", "result": "IDENTICAL"}])  # a replay limited to US-01: IDENTICAL, but it never compared US-06
        self.assertIn("**NOT MET**", row(make(r=limited), 11))
        self.assertIn("not in the replay", row(make(r=limited), 11))
        for bad in ("DIFFERENT", "NOT_COMPARABLE", None):
            r = report(cases=[{"case_id": "BEN-TMO-0001", "result": bad}])
            self.assertIn("**NOT MET**", row(make(r=r), 11), str(bad))

    def test_row_11_refuses_a_replay_without_a_readable_case_list(self) -> None:
        for broken in (None, "x", [{"result": "IDENTICAL"}], [None], [{"case_id": 7, "result": "IDENTICAL"}]):
            r = report()
            r["cases"] = broken
            self.assertIn("**NOT MET**", row(make(r=r), 11), str(broken))
        r = report()
        del r["cases"]
        self.assertIn("**NOT MET**", row(make(r=r), 11))

    def test_row_11_passes_when_every_unknown_outcome_case_is_compared_identical(self) -> None:
        self.assertIn("**MET**", row(make(r=report(cases=[{"case_id": "BEN-TMO-0001", "result": "IDENTICAL"}])), 11))

    def test_row_10_binds_each_scorecard_to_its_replay_leg(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=leg(card(), "g1-other")), 10))
        self.assertIn("**NOT MET**", row(make(b=leg(card(), "g1-other")), 10))
        self.assertIn("**NOT MET**", row(make(a=leg(card(), "g1-b"), b=leg(card(), "g1-a")), 10), "the legs swapped")
        self.assertIn("**NOT MET**", row(make(a=card(), b=leg(card(), "g1-b")) if False else render(inputs(), card(), leg(card(), "g1-b"), report()), 10), "no run id")
        self.assertIn("**NOT MET**", row(render(inputs(), leg(card(), "g1-a"), {**card(), "run_id": 7}, report()), 10), "a run id that is not a string")
        self.assertIn("**MET**", row(make(), 10))

    def test_an_empty_leg_id_matches_nothing_even_when_the_scorecard_carries_the_same_empty_id(self) -> None:
        r = report()
        r["first"] = {"run_id": ""}
        self.assertIn("**NOT MET**", row(render(inputs(), leg(card(), ""), leg(card(), "g1-b"), r), 10))

    def test_every_unknown_outcome_case_must_be_in_the_replay_not_only_the_first(self) -> None:
        i = inputs()
        i["us06_cases"] = ["BEN-TMO-0001", "BEN-TMO-1002"]
        both = report()
        self.assertIn("**MET**", row(make(i, r=both), 11))
        second_missing = report(cases=[{"case_id": "BEN-TMO-0001", "result": "IDENTICAL"}])
        self.assertIn("**NOT MET**", row(make(i, r=second_missing), 11))
        self.assertIn("BEN-TMO-1002", row(make(i, r=second_missing), 11))

    def test_the_binding_also_holds_for_a_leg_accepted_through_a_named_exclusion(self) -> None:
        r = report(excluded=["BEN-CLEAN-1007"])
        ok = make(a=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-a"), b=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-b"), r=r)
        self.assertIn("**MET**", row(ok, 10))
        wrong = make(a=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-zzz"), b=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-b"), r=r)
        self.assertIn("**NOT MET**", row(wrong, 10))


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
