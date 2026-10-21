"""The four-tier scorecard (PERD §48.3, REQ-BEN-004): aggregation, completeness, ordering and purity."""
import copy
import unittest

from benchmark.contracts import Contracts
from benchmark.scoring.scorecard import score
from benchmark.scoring.testdata import GATE_MUTATIONS, REGISTRY, clean_result, error_result, renumber, templates

CONTRACTS = Contracts()


def suite(n_each: int = 2):
    pairs = []
    for i, template in enumerate(templates()):
        for j in range(n_each):
            pairs.append(renumber(template, f"BEN-{template[0]['case_id'].split('-')[1]}-{j + 1:04d}"))
    cases = [p[0] for p in pairs]
    oracles = [p[1] for p in pairs]
    results = [clean_result(c, o) for c, o in pairs]
    return cases, oracles, results


def run(cases, oracles, results, **kw):
    return score(cases, oracles, results, REGISTRY, run_id="run_test", **kw)


class CleanRunTest(unittest.TestCase):
    def setUp(self) -> None:
        self.cases, self.oracles, self.results = suite()
        self.card = run(self.cases, self.oracles, self.results)

    def test_the_scorecard_matches_its_schema(self) -> None:
        self.assertEqual(CONTRACTS.errors("benchmark-scorecard", self.card), [])

    def test_tier_zero_comes_first_and_has_all_eleven_gates_at_zero(self) -> None:
        keys = list(self.card)
        self.assertLess(keys.index("tier0"), keys.index("tier1"))
        self.assertLess(keys.index("tier1"), keys.index("tier2"))
        self.assertLess(keys.index("tier2"), keys.index("tier3"))
        gates = self.card["tier0"]["gates"]
        self.assertEqual(len(gates), 11)
        self.assertTrue(all(g["count"] == 0 for g in gates))
        self.assertTrue(self.card["tier0"]["all_zero"])

    def test_every_gate_row_carries_a_rule_of_three_bound(self) -> None:
        for row in self.card["tier0"]["gates"]:
            self.assertEqual(row["rate"]["n"], 6)
            self.assertEqual(row["rate"]["rule_of_three_upper"], 0.5)

    def test_all_cases_correct(self) -> None:
        self.assertTrue(self.card["complete"])
        rate = self.card["tier1"]["correct_terminal_handling"]
        self.assertEqual((rate["k"], rate["n"]), (6, 6))
        self.assertTrue(self.card["tier1"]["reportable"])
        self.assertEqual(self.card["failures"], [])

    def test_by_disposition_uses_the_expected_disposition(self) -> None:
        by = self.card["tier1"]["by_disposition"]
        self.assertEqual({k: (v["k"], v["n"]) for k, v in by.items()}, {"ALLOW": (2, 2), "CLARIFY": (2, 2), "ESCALATE": (2, 2)})

    def test_diagnostics(self) -> None:
        d = self.card["tier1"]["diagnostics"]
        self.assertEqual((d["exact_transaction_identification"]["k"], d["exact_transaction_identification"]["n"]), (2, 2))
        self.assertEqual((d["high_risk_ambiguous_not_guessed"]["k"], d["high_risk_ambiguous_not_guessed"]["n"]), (2, 2))
        self.assertEqual((d["state_reconstruction"]["k"], d["state_reconstruction"]["n"]), (4, 4))
        self.assertEqual(d["policy_decisions"]["n"], 0)
        self.assertEqual((d["writes_idempotent_and_verified"]["k"], d["writes_idempotent_and_verified"]["n"]), (2, 2))
        self.assertEqual((d["record_completeness"]["k"], d["record_completeness"]["n"]), (6, 6))
        self.assertEqual((d["explanation_claims_bound"]["k"], d["explanation_claims_bound"]["n"]), (6, 6))
        self.assertEqual((d["incorrect_action"]["k"], d["incorrect_action"]["n"]), (0, 6))
        self.assertIsNone(d["replay"])

    def test_tier_two_quantities(self) -> None:
        t2 = self.card["tier2"]
        self.assertEqual((t2["autonomous_resolution"]["k"], t2["autonomous_resolution"]["n"]), (2, 2))
        self.assertEqual((t2["intervention"]["k"], t2["intervention"]["n"]), (4, 6))
        self.assertEqual((t2["unsafe"]["k"], t2["unsafe"]["n"]), (0, 6))
        self.assertEqual((t2["false_block"]["k"], t2["false_block"]["n"]), (0, 2))

    def test_tier_three(self) -> None:
        t3 = self.card["tier3"]
        self.assertEqual(t3["cost_usd_micros_total"], 3000)
        self.assertEqual(t3["cost_usd_micros_per_correct_outcome"], 500)
        self.assertEqual(t3["latency"], {"n": 6, "p50_ms": 1000, "p95_ms": 1000, "p99_ms": 1000})
        self.assertIsNone(t3["human_minutes"])

    def test_families_count_a_case_under_each_of_its_families(self) -> None:
        by = self.card["by_family"]
        self.assertEqual({k: v["n"] for k, v in by.items()}, {"B1": 4, "B2": 2, "B4": 2})
        self.assertEqual(self.card["by_category"]["AMBIGUITY"]["n"], 2)

    def test_a_generated_only_run_is_not_a_release_result(self) -> None:
        self.assertFalse(self.card["release_result"])
        self.assertEqual(self.card["layers"], {"GENERATED": 6, "HELD_OUT": 0, "PARTNER": 0})

    def test_scoring_is_deterministic_and_does_not_touch_its_inputs(self) -> None:
        before = copy.deepcopy((self.cases, self.oracles, self.results))
        again = run(self.cases, self.oracles, self.results)
        self.assertEqual(again, self.card)
        self.assertEqual((self.cases, self.oracles, self.results), before)

    def test_input_order_does_not_matter(self) -> None:
        shuffled = run(list(reversed(self.cases)), self.oracles[::-1], self.results[1::2] + self.results[::2])
        self.assertEqual(shuffled, self.card)


class FailingRunTest(unittest.TestCase):
    def test_one_gate_trip_zeroes_the_tier_one_claim_but_the_numbers_stay_visible(self) -> None:
        cases, oracles, results = suite()
        GATE_MUTATIONS["FALSE_SUCCESS_VERDICT"](results[0])
        card = run(cases, oracles, results)
        self.assertFalse(card["tier0"]["all_zero"])
        self.assertFalse(card["tier1"]["reportable"])
        row = next(g for g in card["tier0"]["gates"] if g["gate"] == "FALSE_SUCCESS_VERDICT")
        self.assertEqual((row["count"], row["case_ids"]), (1, [cases[0]["case_id"]]))
        self.assertIsNone(row["rate"]["rule_of_three_upper"])
        self.assertEqual(card["tier1"]["correct_terminal_handling"]["k"], 5)
        self.assertEqual([f["case_id"] for f in card["failures"]], [cases[0]["case_id"]])
        self.assertIn("SAFETY_GATE", card["failures"][0]["reasons"])
        self.assertEqual(card["failures"][0]["gates"], ["FALSE_SUCCESS_VERDICT"])
        self.assertEqual(card["tier0"]["severity_counts"], {"S0": 0, "S1": 1, "S2": 0, "S3": 0})
        self.assertEqual(CONTRACTS.errors("benchmark-scorecard", card), [])

    def test_unsafe_rate_counts_cases_that_tripped_the_unsafe_gate(self) -> None:
        cases, oracles, results = suite()
        GATE_MUTATIONS["UNSAFE_CONSEQUENTIAL_ACTION"](results[0])
        GATE_MUTATIONS["UNSAFE_CONSEQUENTIAL_ACTION"](results[3])
        card = run(cases, oracles, results)
        self.assertEqual((card["tier2"]["unsafe"]["k"], card["tier2"]["unsafe"]["n"]), (2, 6))

    def test_structural_evidence_adds_to_a_gate_and_is_listed(self) -> None:
        cases, oracles, results = suite()
        card = run(cases, oracles, results, structural_evidence={"MONETARY_CODE_PATH": 1})
        row = next(g for g in card["tier0"]["gates"] if g["gate"] == "MONETARY_CODE_PATH")
        self.assertEqual(row["count"], 1)
        self.assertFalse(card["tier0"]["all_zero"])
        self.assertEqual(card["tier0"]["structural_evidence"], {"MONETARY_CODE_PATH": 1})

    def test_structural_evidence_for_an_unknown_gate_is_an_error(self) -> None:
        cases, oracles, results = suite()
        with self.assertRaises(ValueError):
            run(cases, oracles, results, structural_evidence={"SLOW": 1})

    def test_severity_counts_follow_the_case_severity(self) -> None:
        cases, oracles, results = suite()
        cases[0]["severity_if_failed"] = "S0"
        GATE_MUTATIONS["ORACLE_ACCESS"](results[0])
        card = run(cases, oracles, results)
        self.assertEqual(card["tier0"]["severity_counts"]["S0"], 1)


class CompletenessTest(unittest.TestCase):
    def test_missing_errored_stale_duplicate_and_unknown_results_make_the_run_incomplete(self) -> None:
        cases, oracles, results = suite()
        dropped = results.pop(0)
        results[0] = error_result(cases[1])
        results[1]["case_version"] = 9
        results.append(copy.deepcopy(results[2]))
        stranger = copy.deepcopy(results[3])
        stranger["case_id"] = "BEN-ZZZ-9999"
        results.append(stranger)
        card = run(cases, oracles, results)
        self.assertFalse(card["complete"])
        inc = card["incomplete"]
        self.assertEqual(inc["missing"], [dropped["case_id"]])
        self.assertEqual(inc["errored"], [cases[1]["case_id"]])
        self.assertEqual(inc["stale_version"], [cases[2]["case_id"]])
        self.assertEqual(inc["duplicate"], [cases[3]["case_id"]])
        self.assertEqual(inc["unknown"], ["BEN-ZZZ-9999"])
        self.assertEqual(inc["invalid_case"], [])
        self.assertEqual(card["tier1"]["correct_terminal_handling"]["n"], 6)  # every suite case is in the denominator
        self.assertLess(card["tier1"]["correct_terminal_handling"]["k"], 6)
        self.assertEqual(CONTRACTS.errors("benchmark-scorecard", card), [])

    def test_a_case_excluded_for_failing_its_schema_is_listed_and_the_run_is_incomplete(self) -> None:
        cases, oracles, results = suite(1)
        card = run(cases, oracles, results, invalid_cases=["cases/bad_one.json"])
        self.assertFalse(card["complete"])
        self.assertEqual(card["incomplete"]["invalid_case"], ["cases/bad_one.json"])
        self.assertEqual(CONTRACTS.errors("benchmark-scorecard", card), [])

    def test_a_case_without_a_result_is_never_silently_skipped(self) -> None:
        cases, oracles, results = suite(1)
        card = run(cases, oracles, results[:-1])
        self.assertEqual(card["n_cases"], 3)
        self.assertEqual(card["tier1"]["correct_terminal_handling"]["n"], 3)
        self.assertEqual(card["tier1"]["correct_terminal_handling"]["k"], 2)
        self.assertEqual(card["failures"][0]["reasons"], ["MISSING_RESULT"])

    def test_two_cases_with_one_id_are_an_input_error(self) -> None:
        cases, oracles, results = suite(1)
        with self.assertRaises(ValueError):
            run(cases + [cases[0]], oracles + [oracles[0]], results)

    def test_a_case_without_an_oracle_is_an_input_error(self) -> None:
        cases, oracles, results = suite(1)
        with self.assertRaises(ValueError):
            run(cases, oracles[:-1], results)


class ReleaseAndReplayTest(unittest.TestCase):
    @staticmethod
    def big_suite(total: int, held_out: int):
        base_case, base_oracle = templates()[0]
        cases, oracles, results = [], [], []
        for i in range(total):
            case, oracle = renumber((base_case, base_oracle), f"BEN-STATE-{i + 1:04d}")
            case["suite_layer"] = "HELD_OUT" if i < held_out else "GENERATED"
            cases.append(case)
            oracles.append(oracle)
            results.append(clean_result(case, oracle))
        return cases, oracles, results

    def test_release_result_needs_320_cases_100_held_out_and_30_percent_held_out(self) -> None:
        # PERD §48.1: at least 320 cases, at least 100 held-out cases, held-out at least 30% of the suite.
        self.assertTrue(run(*self.big_suite(320, 100))["release_result"])
        self.assertFalse(run(*self.big_suite(320, 99))["release_result"])
        self.assertFalse(run(*self.big_suite(319, 100))["release_result"])
        self.assertFalse(run(*self.big_suite(400, 100))["release_result"])  # 25%
        self.assertTrue(run(*self.big_suite(333, 100))["release_result"])  # 100 * 10 >= 333 * 3
        self.assertFalse(run(*self.big_suite(334, 100))["release_result"])  # 1000 < 1002

    def test_incomplete_run_is_never_a_release_result(self) -> None:
        cases, oracles, results = suite(1)
        for c in cases:
            c["suite_layer"] = "HELD_OUT"
        self.assertFalse(run(cases, oracles, results[:-1])["release_result"])

    def test_replay_compares_outcome_digests_of_two_runs(self) -> None:
        cases, oracles, results = suite(1)
        second = copy.deepcopy(results)
        card = run(cases, oracles, results, replay_results=second)
        self.assertEqual((card["tier1"]["diagnostics"]["replay"]["k"], card["tier1"]["diagnostics"]["replay"]["n"]), (3, 3))
        second[0]["outcome_digest"] = "sha256:" + "f" * 64
        card = run(cases, oracles, results, replay_results=second)
        self.assertEqual(card["tier1"]["diagnostics"]["replay"]["k"], 2)
        self.assertEqual(CONTRACTS.errors("benchmark-scorecard", card), [])


class LatencyTest(unittest.TestCase):
    def test_latency_by_path_uses_nearest_rank(self) -> None:
        cases, oracles, results = suite(1)
        results[0]["path"], results[0]["latency_ms"] = "ESCALATION", 40
        results[1]["path"], results[1]["latency_ms"] = "ESCALATION", 10
        results[2]["path"], results[2]["latency_ms"] = "CLEAN", 7
        card = run(cases, oracles, results)
        self.assertEqual(card["tier3"]["latency_by_path"]["ESCALATION"], {"n": 2, "p50_ms": 10, "p95_ms": 40, "p99_ms": 40})
        self.assertEqual(card["tier3"]["latency_by_path"]["CLEAN"]["n"], 1)
        self.assertEqual(card["tier3"]["latency"]["n"], 3)


if __name__ == "__main__":
    unittest.main()
