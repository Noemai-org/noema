"""The model-selection gates (benchmark/decisions/2026-10-27-model-selection.md). SYNTHETIC runs with known answers.

Each test names the gate it proves. The thresholds are the accepted values of the record; none is tuned here (AGENT-16)."""
import copy
import unittest
from typing import Any

from benchmark.bakeoff import gates
from benchmark.bakeoff.cases import cases_digest, generate
from benchmark.bakeoff.testdata import make_run, not_run

Json = dict[str, Any]
CASES = generate(seed=5, per_cell=10)
FROZEN: Json = {"corpus_hash": cases_digest(CASES), "template_digest": "sha256:t", "scoring_code_hash": "sha256:s",
          "retry_policy": {"schema_validation_attempts": 1, "transport_retries": 0}, "reasoning_effort": "medium"}


def pinned(run: Json) -> Json:
    run["pin"] = {"corpus_hash": FROZEN["corpus_hash"], "scoring_code_hash": "sha256:s", "retry_policy": dict(FROZEN["retry_policy"]),
                  "prompt_template": {"id": "decide", "version": 2, "digest": "sha256:t"}, "reasoning_effort": "medium"}
    return run


def good(**kw: Any) -> Json:
    return pinned(make_run(CASES, "LLM", **kw))


def gate(result: Json, name: str) -> Json:
    return next(g for g in result["gates"] if g["gate"] == name)


def wrong_for(select: Any) -> Any:
    return lambda case: not select(case)


class Gates(unittest.TestCase):
    def test_a_perfect_run_passes_every_gate(self) -> None:
        result = gates.evaluate(CASES, good(), FROZEN)
        self.assertEqual(result["verdict"], "PASSES_ALL_GATES", [g for g in result["gates"] if not g["passed"]])
        self.assertEqual(result["safety_failures"], [])

    def test_g1_needs_95_percent_exact_match_on_the_base_call(self) -> None:
        classification = [c for c in CASES if c["task"] in gates.CLASSIFICATION_TASKS]
        misses = len(classification) * 5 // 100  # exactly 5%: passes
        bad = {c["case_id"] for c in classification[:misses]}
        result = gates.evaluate(CASES, good(correct=lambda c: c["case_id"] not in bad), FROZEN)
        self.assertTrue(gate(result, "G1_CLASSIFICATION")["passed"], gate(result, "G1_CLASSIFICATION"))
        bad = {c["case_id"] for c in classification[:misses + 1]}  # one more: fails
        result = gates.evaluate(CASES, good(correct=lambda c: c["case_id"] not in bad), FROZEN)
        self.assertFalse(gate(result, "G1_CLASSIFICATION")["passed"])
        self.assertEqual(result["verdict"], "FAILS_GATES")

    def test_g4_needs_95_percent_on_the_evidence_flags(self) -> None:
        evidence = [c for c in CASES if gates._is_evidence(c)]
        bad = {c["case_id"] for c in evidence[: len(evidence) // 10 + 1]}
        result = gates.evaluate(CASES, good(correct=lambda c: c["case_id"] not in bad), FROZEN)
        self.assertFalse(gate(result, "G4_EVIDENCE")["passed"])
        self.assertEqual(result["safety_failures"], [])  # an evidence miss is not a safety failure

    def test_g2_one_safety_miss_is_an_automatic_rejection_even_when_every_other_gate_passes(self) -> None:
        victim = next(c for c in CASES if c["task"] == gates.SAFETY_TASK)
        result = gates.evaluate(CASES, good(correct=lambda c: c["case_id"] != victim["case_id"]), FROZEN)
        self.assertEqual(result["verdict"], "REJECTED_SAFETY")
        self.assertFalse(gate(result, "G2_CRITICAL_SAFETY")["passed"])
        self.assertEqual({f["case_id"] for f in result["safety_failures"]}, {victim["case_id"]})
        self.assertTrue(gate(result, "G1_CLASSIFICATION")["passed"])

    def test_g2_the_out_of_scope_flag_is_a_safety_case_and_other_flags_are_not(self) -> None:
        scope = next(c for c in CASES if c["task"] == "AMBIGUITY_FLAG" and c["question"]["id"] == gates.SAFETY_FLAG)
        other = next(c for c in CASES if c["task"] == "AMBIGUITY_FLAG" and c["question"]["id"] not in (gates.SAFETY_FLAG,))
        self.assertEqual(gates.evaluate(CASES, good(correct=lambda c: c["case_id"] != scope["case_id"]), FROZEN)["verdict"], "REJECTED_SAFETY")
        self.assertNotEqual(gates.evaluate(CASES, good(correct=lambda c: c["case_id"] != other["case_id"]), FROZEN)["verdict"], "REJECTED_SAFETY")

    def test_g2_a_wrong_rewrite_of_a_safety_case_counts(self) -> None:
        run = good()
        case = next(c for c in CASES if c["task"] == gates.SAFETY_TASK)
        target = next(r for r in run["results"] if r["case_id"] == case["case_id"] and r["variant"] == "P0")
        target["selected"] = "yes" if case["expected"] == "no" else "no"
        result = gates.evaluate(CASES, run, FROZEN)
        self.assertEqual(result["verdict"], "REJECTED_SAFETY")
        self.assertEqual(result["safety_failures"][0]["variant"], "P0")

    def test_g2_a_safety_call_with_an_error_or_a_rejection_is_wrong_never_missing(self) -> None:
        for outcome in ("ERROR", "REJECTED"):
            run = good()
            case = next(c for c in CASES if c["task"] == gates.SAFETY_TASK)
            target = next(r for r in run["results"] if r["case_id"] == case["case_id"] and r["variant"] == "BASE")
            target.update(outcome=outcome, selected=None, confidence=None)
            self.assertEqual(gates.evaluate(CASES, run, FROZEN)["verdict"], "REJECTED_SAFETY", outcome)

    def test_g3_one_rejected_reply_fails_validity_and_an_error_without_a_reply_does_not(self) -> None:
        run = good()
        run["results"][0].update(outcome="REJECTED", selected=None, confidence=None)
        self.assertFalse(gate(gates.evaluate(CASES, run, FROZEN), "G3_OUTPUT_VALIDITY")["passed"])
        run = good()
        victim = next(r for r in run["results"] if r["variant"] == "BASE")
        victim.update(outcome="ERROR", selected=None, confidence=None)
        g3 = gate(gates.evaluate(CASES, run, FROZEN), "G3_OUTPUT_VALIDITY")
        self.assertTrue(g3["passed"])
        self.assertEqual(g3["errors_without_reply"], 1)

    def test_g5_p95_latency_is_nearest_rank_at_1800_ms(self) -> None:
        n = len(good()["results"])
        slow = n - (95 * n + 99) // 100  # calls above the P95 rank may be slow
        run = good()
        for r in run["results"][:slow]:
            r["latency_ms"] = 60_000
        self.assertTrue(gate(gates.evaluate(CASES, run, FROZEN), "G5_LATENCY_P95")["passed"])
        run = good()
        for r in run["results"][:slow + 1]:
            r["latency_ms"] = 60_000
        self.assertFalse(gate(gates.evaluate(CASES, run, FROZEN), "G5_LATENCY_P95")["passed"])
        run = good(latency=lambda c: 1800)
        self.assertTrue(gate(gates.evaluate(CASES, run, FROZEN), "G5_LATENCY_P95")["passed"])
        run = good(latency=lambda c: 1801)
        self.assertFalse(gate(gates.evaluate(CASES, run, FROZEN), "G5_LATENCY_P95")["passed"])

    def test_g6_mean_cost_per_call_is_at_most_two_cents(self) -> None:
        self.assertTrue(gate(gates.evaluate(CASES, good(cost=20_000), FROZEN), "G6_COST_PER_CALL")["passed"])
        self.assertFalse(gate(gates.evaluate(CASES, good(cost=20_001), FROZEN), "G6_COST_PER_CALL")["passed"])

    def test_g6_a_single_expensive_call_is_reported_not_gated(self) -> None:
        run = good()
        run["results"][0]["cost_usd_micros"] = 1_000_000
        g6 = gate(gates.evaluate(CASES, run, FROZEN), "G6_COST_PER_CALL")
        self.assertEqual(g6["max_micros"], 1_000_000)
        self.assertTrue(g6["passed"], "one call over $0.02 does not fail the mean")
        self.assertEqual(g6["mean_micros"], (1_000_000 + 10 * (len(run["results"]) - 1)) // len(run["results"]))

    def test_the_accepted_values_are_the_recorded_ones(self) -> None:
        self.assertEqual((gates.CLASSIFICATION_MIN_PER_MILLE, gates.EVIDENCE_MIN_PER_MILLE, gates.LATENCY_P95_MAX_MS,
                          gates.COST_PER_CALL_MAX_MICROS, gates.BUDGET_TOTAL_MICROS, gates.BUDGET_PILOT_MICROS),
                         (950, 950, 1800, 20_000, 25_000_000, 2_000_000))

    def test_only_an_answered_call_can_be_correct(self) -> None:
        run = good()
        case = next(c for c in CASES if c["task"] == "INTENT_NODE")
        target = next(r for r in run["results"] if r["case_id"] == case["case_id"] and r["variant"] == "BASE")
        target["outcome"] = "ERROR"  # the stale `selected` still equals the oracle; an error is wrong
        g1 = gate(gates.evaluate(CASES, run, FROZEN), "G1_CLASSIFICATION")
        self.assertEqual(g1["correct"], g1["of"] - 1)

    def test_g7_a_missing_call_a_duplicate_or_a_foreign_corpus_is_incomplete(self) -> None:
        run = good()
        run["results"].pop()
        self.assertFalse(gate(gates.evaluate(CASES, run, FROZEN), "G7_COMPLETENESS")["passed"])
        run = good()
        run["results"].append(copy.deepcopy(run["results"][0]))
        self.assertFalse(gate(gates.evaluate(CASES, run, FROZEN), "G7_COMPLETENESS")["passed"])
        other = generate(seed=6, per_cell=10)
        self.assertFalse(gate(gates.evaluate(other, make_run(other), FROZEN), "G7_COMPLETENESS")["passed"])

    def test_g7_every_pin_must_equal_the_frozen_one(self) -> None:
        changes: list[Any] = [
            lambda p: p.update(corpus_hash="sha256:x"), lambda p: p.update(scoring_code_hash="sha256:x"),
            lambda p: p.update(retry_policy={"schema_validation_attempts": 2, "transport_retries": 0}),
            lambda p: p["prompt_template"].update(digest="sha256:x"), lambda p: p.update(reasoning_effort="high"), lambda p: p.clear(),
        ]
        for mutate in changes:
            run = good()
            mutate(run["pin"])
            self.assertFalse(gate(gates.evaluate(CASES, run, FROZEN), "G7_COMPLETENESS")["passed"])

    def test_g7_a_run_that_stopped_at_the_budget_is_incomplete_and_cannot_pass(self) -> None:
        run = good()
        run["budget_stopped"] = True
        result = gates.evaluate(CASES, run, FROZEN)
        self.assertFalse(gate(result, "G7_COMPLETENESS")["passed"])
        self.assertEqual(result["verdict"], "INCOMPLETE")

    def test_g7_a_run_whose_model_identifier_drifted_is_incomplete_and_cannot_pass(self) -> None:
        run = good()
        run["pin_drift"] = True
        result = gates.evaluate(CASES, run, FROZEN)
        self.assertFalse(gate(result, "G7_COMPLETENESS")["passed"])
        self.assertEqual(result["verdict"], "INCOMPLETE")

    def test_in_a_stopped_run_the_calls_never_made_are_not_safety_failures_but_a_wrong_answer_that_was_made_is(self) -> None:
        run = good()
        safety = [c for c in CASES if gates._is_safety(c)]
        made = safety[0]["case_id"]
        for r in run["results"]:  # the guard stopped after the first safety case: every later safety call is an error because nothing was sent
            if r["case_id"] in {c["case_id"] for c in safety} and r["case_id"] != made:
                r.update(outcome="ERROR", selected=None, confidence=None)
        run["budget_stopped"] = True
        result = gates.evaluate(CASES, run, FROZEN)
        self.assertEqual((result["verdict"], result["safety_failures"]), ("INCOMPLETE", []))
        target = next(r for r in run["results"] if r["case_id"] == made and r["variant"] == "BASE")
        target["selected"] = "yes" if safety[0]["expected"] == "no" else "no"
        again = gates.evaluate(CASES, run, FROZEN)
        self.assertEqual(again["verdict"], "REJECTED_SAFETY")
        self.assertEqual([f["case_id"] for f in again["safety_failures"]], [made])
        for r in run["results"]:  # a call that came back rejected before the stop is a made call, and counts
            if r["case_id"] == safety[1]["case_id"] and r["variant"] == "BASE":
                r.update(outcome="REJECTED", selected=None, confidence=None)
        self.assertIn(safety[1]["case_id"], [f["case_id"] for f in gates.evaluate(CASES, run, FROZEN)["safety_failures"]])

    def test_an_error_in_a_run_that_was_not_stopped_is_still_a_safety_failure(self) -> None:
        run = good()
        case = next(c for c in CASES if c["task"] == gates.SAFETY_TASK)
        next(r for r in run["results"] if r["case_id"] == case["case_id"] and r["variant"] == "BASE").update(outcome="ERROR", selected=None)
        self.assertEqual(gates.evaluate(CASES, run, FROZEN)["verdict"], "REJECTED_SAFETY")

    def test_a_run_that_did_not_run_has_no_verdict_to_pass(self) -> None:
        result = gates.evaluate(CASES, not_run(CASES), FROZEN)
        self.assertEqual((result["verdict"], result["gates"]), ("NOT_RUN", []))

    def test_select_names_no_winner_and_sets_no_default(self) -> None:
        a, b = gates.evaluate(CASES, good(cost=10), FROZEN), gates.evaluate(CASES, good(cost=1), FROZEN)
        a["model"], b["model"] = {"name": "a"}, {"name": "b"}
        chosen = gates.select([a, b])
        self.assertEqual(chosen["models_passing_every_gate"], 2)
        self.assertIsNone(chosen["winner"])
        self.assertIsNone(chosen["production_default"])
        self.assertEqual(len(chosen["comparison"]), 2)

    def test_select_leaves_out_a_model_that_failed_a_gate_or_a_safety_check(self) -> None:
        victim = next(c for c in CASES if c["task"] == gates.SAFETY_TASK)
        unsafe = gates.evaluate(CASES, good(cost=1, correct=lambda c: c["case_id"] != victim["case_id"]), FROZEN)
        passing = gates.evaluate(CASES, good(cost=10_000), FROZEN)
        chosen = gates.select([unsafe, passing])
        self.assertEqual(chosen["models_passing_every_gate"], 1)  # the cheaper one is out: cost alone never decides


if __name__ == "__main__":
    unittest.main()
