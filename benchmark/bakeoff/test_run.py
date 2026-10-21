"""Running a provider path over the bake-off cases: one code path, typed rejections recorded, NOT_RUN when there is no access."""
import itertools
import unittest
from typing import Any

from benchmark.bakeoff.cases import cases_digest, generate
from benchmark.bakeoff.run import Answer, DeciderError, DeciderRejected, DeciderUnavailable, NotRunDecider, get_decider, run_provider
from benchmark.contracts import Contracts

CASES = generate(seed=3, per_cell=3)
CONTRACTS = Contracts()
MODEL = {"provider": "example", "name": "example-model", "version": "2026-01-01"}


class Scripted:
    """A decider with fixed behaviour, standing in for a provider path in tests of the runner (not a mock of a failure mode)."""

    provider = "LLM"

    def __init__(self, behaviour: Any = None) -> None:
        self.behaviour = behaviour or (lambda question, text: Answer(selected=question["options"][0], confidence=0.9, cost_usd_micros=7))
        self.calls = 0

    def ready(self) -> None:
        return None

    def model_info(self) -> dict[str, Any]:
        return dict(MODEL)

    def decide(self, question: dict[str, Any], text: str, context: dict[str, Any] | None) -> Answer:
        self.calls += 1
        return self.behaviour(question, text)


def run(decider: Any, cases: list[dict[str, Any]] | None = None) -> dict[str, Any]:
    ticks = itertools.count(0, 5_000_000)
    return run_provider(cases or CASES, decider, run_id="bakeoff_unit", runner_region="af-lagos", timer=lambda: next(ticks))


class RunTest(unittest.TestCase):
    def test_a_run_has_a_base_call_and_one_call_per_perturbation_and_is_valid(self) -> None:
        decider = Scripted()
        result = run(decider)
        self.assertEqual(CONTRACTS.errors("benchmark-decision-run", result), [])
        self.assertEqual(result["status"], "RAN")
        self.assertEqual(result["cases_version"], cases_digest(CASES))
        self.assertEqual(len(result["results"]), sum(1 + len(c["perturbations"]) for c in CASES))
        self.assertEqual(decider.calls, len(result["results"]))
        self.assertEqual(result["model"], MODEL)

    def test_latency_is_the_timer_difference_per_call(self) -> None:
        self.assertEqual({r["latency_ms"] for r in run(Scripted())["results"]}, {5})

    def test_cost_and_confidence_come_from_the_answer(self) -> None:
        r = run(Scripted())["results"][0]
        self.assertEqual((r["cost_usd_micros"], r["confidence"], r["outcome"]), (7, 0.9, "ANSWERED"))

    def test_an_answer_off_the_menu_is_rejected_never_repaired(self) -> None:
        result = run(Scripted(lambda q, t: Answer(selected="a made up option", confidence=0.99, cost_usd_micros=1)))
        self.assertTrue(all(r["outcome"] == "REJECTED" and r["selected"] is None and r["confidence"] is None for r in result["results"]))
        self.assertEqual(CONTRACTS.errors("benchmark-decision-run", result), [])

    def test_a_confidence_outside_zero_to_one_is_rejected(self) -> None:
        result = run(Scripted(lambda q, t: Answer(selected=q["options"][0], confidence=1.2, cost_usd_micros=1)))
        self.assertTrue(all(r["outcome"] == "REJECTED" for r in result["results"]))

    def test_a_typed_rejection_from_the_harness_is_recorded(self) -> None:
        def reject(q: Any, t: Any) -> Answer:
            raise DeciderRejected("schema mismatch")
        self.assertTrue(all(r["outcome"] == "REJECTED" for r in run(Scripted(reject))["results"]))

    def test_a_provider_error_is_recorded_as_an_error_and_the_run_goes_on(self) -> None:
        state = {"n": 0}

        def flaky(q: Any, t: Any) -> Answer:
            state["n"] += 1
            if state["n"] % 2:
                raise DeciderError("timeout")
            return Answer(selected=q["options"][0], confidence=0.8, cost_usd_micros=1)

        outcomes = {r["outcome"] for r in run(Scripted(flaky))["results"]}
        self.assertEqual(outcomes, {"ERROR", "ANSWERED"})

    def test_an_unexpected_exception_is_not_swallowed(self) -> None:
        def broken(q: Any, t: Any) -> Answer:
            raise KeyError("bug")
        with self.assertRaises(KeyError):
            run(Scripted(broken))

    def test_the_text_sent_is_the_input_or_its_perturbation(self) -> None:
        seen: list[str] = []

        def spy(q: Any, t: Any) -> Answer:
            seen.append(t)
            return Answer(selected=q["options"][-1], confidence=0.5, cost_usd_micros=0)

        run(Scripted(spy), CASES[:1])
        self.assertEqual(seen, [CASES[0]["input"]["text"], *[p["text"] for p in CASES[0]["perturbations"]]])

    def test_the_decider_never_sees_the_expected_answer(self) -> None:
        def check(q: Any, t: Any) -> Answer:
            self.assertNotIn("expected", q)
            return Answer(selected=q["options"][0], confidence=0.5, cost_usd_micros=0)

        run(Scripted(check), CASES[:3])


class NotRunTest(unittest.TestCase):
    def test_no_access_is_recorded_as_not_run_and_nothing_is_faked(self) -> None:
        result = run(NotRunDecider("DECIDE_OPENAI_DECISIONS", "no access: no API key"))
        self.assertEqual((result["status"], result["results"], result["not_run_reason"]), ("NOT_RUN", [], "no access: no API key"))
        self.assertNotIn("model", result)
        self.assertEqual(CONTRACTS.errors("benchmark-decision-run", result), [])

    def test_a_decider_that_is_not_ready_is_not_run_whatever_it_says(self) -> None:
        decider = Scripted()

        def not_ready() -> None:
            raise DeciderUnavailable("the harness is down")

        decider.ready = not_ready  # type: ignore[method-assign]
        result = run(decider)
        self.assertEqual((result["status"], decider.calls), ("NOT_RUN", 0))


class GetDeciderTest(unittest.TestCase):
    def test_without_a_key_it_says_no_access(self) -> None:
        for provider in ("LLM", "DECIDE_OPENAI_DECISIONS"):
            decider = get_decider(provider, {})
            with self.assertRaises(DeciderUnavailable) as caught:
                decider.ready()
            self.assertIn("no access", str(caught.exception))

    def test_with_a_key_it_is_still_not_run_until_the_harness_exists_and_the_key_is_never_echoed(self) -> None:
        decider = get_decider("DECIDE_OPENAI_DECISIONS", {"OPENAI_API_KEY": "sk-canary-value"})
        with self.assertRaises(DeciderUnavailable) as caught:
            decider.ready()
        self.assertIn("harness.decide()", str(caught.exception))
        self.assertNotIn("sk-canary-value", str(caught.exception))

    def test_unknown_provider(self) -> None:
        with self.assertRaises(ValueError):
            get_decider("DECIDE_JEV", {})


if __name__ == "__main__":
    unittest.main()
