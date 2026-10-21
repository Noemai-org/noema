"""Turning a case's provisional fault plan into what the environment can apply (REQ-ENV-004; the plan format is mock-case's, not the generator's)."""
import unittest
from typing import Any

from benchmark.runner.faultplan import CLOCK_START, MOCK_CASE_EFFECTS, split_plan

Json = dict[str, Any]


def case(timeline: list[str] | None = None, plan: str = "fp_ben_x_0001") -> Json:
    return {"case_id": "BEN-TMO-0001", "generator": {"name": "noema-casegen", "version": "1.1.0", "seed": 5}, "fault_plan": plan,
            "events_timeline": [{"at": "+00:00", "event": "session_start:synthetic"}, *({"at": "+00:00:31", "event": e} for e in timeline or [])]}


def plan(*faults: Json) -> Json:
    return {"plan_id": "fp_ben_x_0001", "synthetic": True, "case_id": "BEN-TMO-0001", "provisional": "x", "faults": list(faults), "harness_effects": []}


def fault(effect: str, target: str = "mock-case", operation: str = "create_case", match: Json | None = None, params: Json | None = None) -> Json:
    return {"target": target, "operation": operation, "match": match or {}, "effect": effect, "params": params or {}}


class Split(unittest.TestCase):
    def test_no_plan_and_no_clock_loads_nothing(self) -> None:
        s = split_plan(case(plan="fp_none"), None, grant_ttl_seconds=None)
        self.assertEqual((s.mock_case, s.unsupported), (None, []))

    def test_a_mock_case_fault_becomes_a_mock_case_plan(self) -> None:
        s = split_plan(case(), plan(fault("TIMEOUT_AFTER_COMMIT", match={"nth_call": 1}, params={"delay_ms": 3500})), grant_ttl_seconds=None)
        self.assertEqual(s.unsupported, [])
        assert s.mock_case is not None
        self.assertEqual(s.mock_case["source"], "MOCK_CASE")
        self.assertTrue(s.mock_case["deterministic"])
        self.assertEqual((s.mock_case["fault_plan_id"], s.mock_case["scenario_id"], s.mock_case["seed"]), ("fp_ben_x_0001", "BEN-TMO-0001", 5))
        self.assertEqual(s.mock_case["faults"][0]["effect"], "TIMEOUT_AFTER_COMMIT")
        self.assertEqual(s.mock_case["faults"][0]["params"], {"delay_ms": 3500})

    def test_names_are_translated_to_mock_cases(self) -> None:
        s = split_plan(case(), plan(fault("CONCURRENT_MODIFICATION", operation="route"), fault("HTTP_5XX", params={"committed": True})), grant_ttl_seconds=None)
        assert s.mock_case is not None
        self.assertEqual(s.mock_case["faults"][0]["operation"], "route_case")
        self.assertEqual(s.mock_case["faults"][1]["params"], {"after_commit": True})

    def test_a_fault_aimed_at_a_source_this_build_cannot_reach_is_unsupported_not_dropped(self) -> None:
        s = split_plan(case(), plan(fault("STALE_READ", target="mock-ledger", operation="get_transaction"), fault("DROP_RESPONSE")), grant_ttl_seconds=None)
        self.assertEqual(len(s.mock_case["faults"]), 1)  # type: ignore[index]
        self.assertEqual(len(s.unsupported), 1)
        self.assertIn("mock-ledger", s.unsupported[0])

    def test_an_effect_mock_case_does_not_produce_is_unsupported(self) -> None:
        for effect in ("HTTP_200_SEMANTIC_PENDING", "SERVICE_UNAVAILABLE", "DROP_WEBHOOK", "NEAR_TIMEOUT_BEFORE"):
            s = split_plan(case(), plan(fault(effect)), grant_ttl_seconds=None)
            self.assertEqual(len(s.unsupported), 1, effect)
            self.assertNotIn(effect, MOCK_CASE_EFFECTS)

    def test_a_grant_relative_value_needs_the_grant_ttl(self) -> None:
        f = fault("HTTP_429", params={"retry_after": "beyond_grant_expiry"})
        self.assertEqual(len(split_plan(case(), plan(f), grant_ttl_seconds=None).unsupported), 1)
        s = split_plan(case(), plan(f), grant_ttl_seconds=300)
        assert s.mock_case is not None
        self.assertEqual(s.mock_case["faults"][0]["params"], {"retry_after_seconds": 301})
        slow = fault("LATENCY", params={"delay": "past_grant_ttl"})
        self.assertEqual(split_plan(case(), plan(slow), grant_ttl_seconds=2).mock_case["faults"][0]["params"], {"delay_ms": 3000})  # type: ignore[index]

    def test_a_clock_advance_starts_the_simulated_clock(self) -> None:
        s = split_plan(case(["clock_advance:PT31S"]), plan(), grant_ttl_seconds=None)
        assert s.mock_case is not None
        self.assertEqual(s.mock_case["clock"], {"start": CLOCK_START})
        self.assertEqual(s.mock_case["faults"], [])

    def test_a_param_mock_case_does_not_know_is_refused_here_not_silently_loaded(self) -> None:
        s = split_plan(case(), plan(fault("LATENCY", params={"mystery": 1})), grant_ttl_seconds=None)
        self.assertEqual(len(s.unsupported), 1)


if __name__ == "__main__":
    unittest.main()
