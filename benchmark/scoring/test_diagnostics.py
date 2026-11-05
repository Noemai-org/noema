"""BEN-30, REQ-BEN-009: pass^k (k = 5), turns to resolution, the unnecessary-clarification rate and auto-resolve precision, on SYNTHETIC results.

Written before `diagnostics.py` and seen failing. The definitions are the pre-registered ones of 2026-11-02 (benchmark/scoring/README.md); a test pins
each, so changing a definition is a visible, versioned edit. These are diagnostics, not gates: no test here sets a threshold (AGENT-16)."""
import copy
import unittest
from typing import Any

from benchmark.contracts import Contracts
from benchmark.scoring.scorecard import score
from benchmark.scoring.testdata import REGISTRY, clean_result, error_result, make_case, make_oracle, renumber

CONTRACTS = Contracts()
Json = dict[str, Any]
K = 5


def allow_pair(case_id: str, *, families: tuple[str, ...] = ("B1",)) -> tuple[Json, Json]:
    case = make_case(case_id, disposition="ALLOW", writes=(), classification="SUCCESS_CONFIRMED", families=families, category="CLEAN_UNIQUE",
                     required_events=("DECISION_MADE",), allowed_claims=("completed",), severity="S1")
    return case, make_oracle(case_id, true_state="SUCCESS_CONFIRMED", eligible=True, safe=True)


def clarify_pair(case_id: str, *, families: tuple[str, ...] = ("B1",)) -> tuple[Json, Json]:
    case = make_case(case_id, disposition="CLARIFY", writes=(), classification=None, families=families, category="AMBIGUITY",
                     required_events=("DECISION_MADE", "CLARIFICATION_REQUESTED"), allowed_claims=("two matching transfers",), forbidden_actions=("any_write",))
    return case, make_oracle(case_id, correct_target=None, true_state=None, high_risk=True)


def runs(pairs: list[tuple[Json, Json]], count: int = K, edit: Any = None) -> list[list[Json]]:
    """`count` full result sets with run ids run_1 .. run_<count>; `edit(run_index, case_id, result)` may change one result in place."""
    sets = []
    for i in range(1, count + 1):
        results = []
        for case, oracle in pairs:
            result = clean_result(case, oracle, run_id=f"run_{i}")
            if edit:
                edit(i, case["case_id"], result)
            results.append(result)
        sets.append(results)
    return sets


def card(pairs: list[tuple[Json, Json]], sets: list[list[Json]]) -> Json:
    return score([p[0] for p in pairs], [p[1] for p in pairs], sets[0], REGISTRY, run_id="run_1", repeat_runs=sets[1:])


def diagnostics(pairs: list[tuple[Json, Json]], sets: list[list[Json]]) -> Json:
    result: Json = card(pairs, sets)["tier1"]["diagnostics"]
    return result


class PassK(unittest.TestCase):
    def setUp(self) -> None:
        self.pairs = [allow_pair("BEN-CLEAN-0001"), allow_pair("BEN-CLEAN-0002"), clarify_pair("BEN-AMB-0001"), allow_pair("BEN-STATE-0001", families=("B4",))]

    def test_one_run_is_not_enough_and_says_so(self) -> None:
        pk = diagnostics(self.pairs, runs(self.pairs, 1))["pass_k"]
        self.assertEqual((False, None, 1, 5), (pk["available"], pk["rate"], pk["runs_supplied"], pk["k_required"]))
        self.assertIn("1 run", pk["reason"])
        self.assertEqual([], pk["failing_cases"])

    def test_four_runs_and_six_runs_are_never_scored_as_if_they_were_five(self) -> None:
        for count in (4, 6):
            pk = diagnostics(self.pairs, runs(self.pairs, count))["pass_k"]
            self.assertFalse(pk["available"], count)
            self.assertIsNone(pk["rate"])
            self.assertEqual(count, pk["runs_supplied"])

    def test_five_clean_runs_pass_every_understanding_dependent_case_and_a_b4_case_is_not_counted(self) -> None:
        pk = diagnostics(self.pairs, runs(self.pairs))["pass_k"]
        self.assertTrue(pk["available"])
        self.assertEqual((3, 3), (pk["rate"]["k"], pk["rate"]["n"]))
        self.assertEqual([], pk["failing_cases"])
        self.assertIsNone(pk["reason"])

    def test_a_case_that_fails_in_one_of_five_runs_fails(self) -> None:
        def edit(run: int, case_id: str, result: Json) -> None:
            if run == 3 and case_id == "BEN-CLEAN-0002":
                result["observed"]["disposition"] = "ESCALATE"
        pk = diagnostics(self.pairs, runs(self.pairs, edit=edit))["pass_k"]
        self.assertEqual((2, 3), (pk["rate"]["k"], pk["rate"]["n"]))
        self.assertEqual(["BEN-CLEAN-0002"], pk["failing_cases"])

    def test_an_errored_run_fails_the_case(self) -> None:
        sets = runs(self.pairs)
        sets[4][0] = error_result(self.pairs[0][0], run_id="run_5")
        pk = diagnostics(self.pairs, sets)["pass_k"]
        self.assertEqual(["BEN-CLEAN-0001"], pk["failing_cases"])

    def test_a_case_missing_from_a_run_fails(self) -> None:
        sets = runs(self.pairs)
        del sets[2][1]
        self.assertEqual(["BEN-CLEAN-0002"], diagnostics(self.pairs, sets)["pass_k"]["failing_cases"])

    def test_the_same_run_id_twice_is_refused(self) -> None:
        sets = runs(self.pairs)
        for r in sets[3]:
            r["run_id"] = "run_2"
        with self.assertRaises(ValueError) as ctx:
            card(self.pairs, sets)
        self.assertIn("run_2", str(ctx.exception))

    def test_a_result_set_with_two_run_ids_is_refused(self) -> None:
        sets = runs(self.pairs)
        sets[1][0]["run_id"] = "run_9"
        with self.assertRaises(ValueError):
            card(self.pairs, sets)

    def test_the_families_used_are_published_in_the_scorecard(self) -> None:
        defs = diagnostics(self.pairs, runs(self.pairs, 1))["definitions"]
        self.assertEqual({"version": "2026-11-03", "understanding_families": ["B1"]}, defs)

    def test_a_b2_state_case_is_not_understanding_dependent(self) -> None:
        # Founder amendment of 3 Nov 2026 (SoT §38: B2 is state reconstruction, deterministic): the set is B1 only.
        pairs = [allow_pair("BEN-CLEAN-0001"), allow_pair("BEN-STATE-0001", families=("B2",)), allow_pair("BEN-STATE-0002", families=("B2", "B5"))]
        pk = diagnostics(pairs, runs(pairs))["pass_k"]
        self.assertEqual((1, 1), (pk["rate"]["k"], pk["rate"]["n"]))

    def test_a_case_with_b1_among_other_families_is_counted(self) -> None:
        pairs = [allow_pair("BEN-CLEAN-0001", families=("B1", "B2", "B5", "B7"))]
        self.assertEqual(1, diagnostics(pairs, runs(pairs))["pass_k"]["rate"]["n"])

    def test_no_understanding_dependent_case_gives_n_zero_not_a_pass(self) -> None:
        pairs = [allow_pair("BEN-STATE-0001", families=("B4",))]
        pk = diagnostics(pairs, runs(pairs))["pass_k"]
        self.assertEqual((0, 0, None), (pk["rate"]["k"], pk["rate"]["n"], pk["rate"]["rate"]))


class TurnsToResolution(unittest.TestCase):
    def test_statistics_over_cases_auto_resolved_correctly(self) -> None:
        pairs = [allow_pair(f"BEN-CLEAN-000{i}") for i in range(1, 4)] + [clarify_pair("BEN-AMB-0001")]
        results = [clean_result(c, o) for c, o in pairs]
        for r, t in zip(results, (1, 2, 4, 3)):
            r["turns"] = t
        turns = score([p[0] for p in pairs], [p[1] for p in pairs], results, REGISTRY, run_id="run_test")["tier1"]["diagnostics"]["turns_to_resolution"]
        self.assertEqual({"n": 3, "mean": 2.333333, "median": 2, "p90": 4, "max": 4}, turns)

    def test_a_wrongly_handled_or_errored_case_is_not_counted(self) -> None:
        pairs = [allow_pair("BEN-CLEAN-0001"), allow_pair("BEN-CLEAN-0002"), allow_pair("BEN-CLEAN-0003")]
        results = [clean_result(c, o) for c, o in pairs]
        results[1]["observed"]["disposition"] = "ESCALATE"
        results[1]["turns"] = 9
        results[2] = error_result(pairs[2][0])
        turns = score([p[0] for p in pairs], [p[1] for p in pairs], results, REGISTRY, run_id="run_test")["tier1"]["diagnostics"]["turns_to_resolution"]
        self.assertEqual({"n": 1, "mean": 1.0, "median": 1, "p90": 1, "max": 1}, turns)

    def test_no_resolved_case_gives_null_statistics(self) -> None:
        pairs = [clarify_pair("BEN-AMB-0001")]
        results = [clean_result(c, o) for c, o in pairs]
        turns = score([p[0] for p in pairs], [p[1] for p in pairs], results, REGISTRY, run_id="run_test")["tier1"]["diagnostics"]["turns_to_resolution"]
        self.assertEqual({"n": 0, "mean": None, "median": None, "p90": None, "max": None}, turns)


class ClarificationAndPrecision(unittest.TestCase):
    def one(self, pairs: list[tuple[Json, Json]], edit: Any = None) -> Json:
        results = [clean_result(c, o) for c, o in pairs]
        if edit:
            edit(results)
        result: Json = score([p[0] for p in pairs], [p[1] for p in pairs], results, REGISTRY, run_id="run_test")["tier1"]["diagnostics"]
        return result

    def test_unnecessary_clarification_counts_a_clarification_where_the_case_was_safe_to_resolve(self) -> None:
        pairs = [allow_pair("BEN-CLEAN-0001"), allow_pair("BEN-CLEAN-0002"), clarify_pair("BEN-AMB-0001")]

        def asks(results: list[Json]) -> None:
            results[0]["observed"]["clarification_asked"] = True
            results[0]["observed"]["disposition"] = "CLARIFY"
        d = self.one(pairs, asks)["unnecessary_clarification"]
        self.assertEqual((1, 2), (d["k"], d["n"]))

    def test_a_case_that_expects_clarification_is_never_eligible(self) -> None:
        d = self.one([clarify_pair("BEN-AMB-0001")])["unnecessary_clarification"]
        self.assertEqual((0, 0, None), (d["k"], d["n"], d["rate"]))

    def test_zero_unnecessary_clarifications_carry_the_rule_of_three_bound(self) -> None:
        d = self.one([allow_pair("BEN-CLEAN-0001"), allow_pair("BEN-CLEAN-0002"), allow_pair("BEN-CLEAN-0003")])["unnecessary_clarification"]
        self.assertEqual((0, 3, 1.0), (d["k"], d["n"], d["rule_of_three_upper"]))

    def test_auto_resolve_precision_is_the_share_resolved_to_the_correct_target(self) -> None:
        pairs = [allow_pair("BEN-CLEAN-0001"), allow_pair("BEN-CLEAN-0002"), allow_pair("BEN-CLEAN-0003")]

        def wrong(results: list[Json]) -> None:
            results[1]["observed"]["resolved_target"] = "fin.transaction:TXZ"
        d = self.one(pairs, wrong)["auto_resolve_precision"]
        self.assertEqual((2, 3), (d["k"], d["n"]))

    def test_a_clarified_or_escalated_case_is_not_an_auto_resolution(self) -> None:
        d = self.one([clarify_pair("BEN-AMB-0001")])["auto_resolve_precision"]
        self.assertEqual((0, 0), (d["k"], d["n"]))

    def test_the_scorecard_validates_against_v2_and_tier_zero_is_unchanged_by_the_diagnostics(self) -> None:
        pairs = [allow_pair("BEN-CLEAN-0001"), clarify_pair("BEN-AMB-0001")]
        sets = runs(pairs)
        full = card(pairs, sets)
        single = card(pairs, sets[:1])
        self.assertEqual([], CONTRACTS.errors("benchmark-scorecard.v2", full))
        self.assertEqual([], CONTRACTS.errors("benchmark-scorecard.v2", single))
        self.assertEqual(full["tier0"], single["tier0"])

    def test_scoring_does_not_change_its_inputs(self) -> None:
        pairs = [allow_pair("BEN-CLEAN-0001")]
        sets = runs(pairs)
        before = copy.deepcopy(sets)
        card(pairs, sets)
        self.assertEqual(before, sets)

    def test_helper_pairs_are_valid(self) -> None:
        for case, oracle in (allow_pair("BEN-CLEAN-0001"), clarify_pair("BEN-AMB-0001"), renumber(allow_pair("BEN-CLEAN-0001"), "BEN-CLEAN-0009")):
            self.assertEqual([], CONTRACTS.errors("benchmark-case", case))
            self.assertEqual([], CONTRACTS.errors("benchmark-oracle", oracle))


if __name__ == "__main__":
    unittest.main()
