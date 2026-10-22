"""What the environment can apply of a case's fault-plan.v1 document (REQ-ENV-004): mock-case's faults go to mock-case, the rest is named, never dropped."""
import unittest
from typing import Any

from benchmark.contracts import Contracts
from benchmark.runner.faultplan import CLOCK_START, split_plan

Json = dict[str, Any]


def case(timeline: list[str] | None = None) -> Json:
    return {"case_id": "BEN-TMO-0001", "generator": {"name": "noema-casegen", "version": "1.2.0", "seed": 5}, "fault_plan": "fp_ben_tmo_0001",
            "events_timeline": [{"at": "+00:00", "event": "session_start:synthetic"}, *({"at": "+00:00:31", "event": e} for e in timeline or [])]}


def plan(*faults: Json, source: str = "PROXY") -> Json:
    return {"fault_plan_id": "fp_ben_tmo_0001", "scenario_id": "BEN-TMO-0001", "seed": 5, "deterministic": True, "source": source, "faults": list(faults)}


def fault(effect: str, target: str = "mock-case", operation: str = "create_case", match: Json | None = None, params: Json | None = None) -> Json:
    return {"target": target, "operation": operation, "match": match or {}, "effect": effect, "params": params or {}}


class Split(unittest.TestCase):
    def test_no_plan_and_no_clock_loads_nothing(self) -> None:
        s = split_plan(case(), None)
        self.assertEqual((s.mock_case, s.unsupported), (None, []))

    def test_a_mock_case_fault_becomes_a_valid_mock_case_plan(self) -> None:
        s = split_plan(case(), plan(fault("TIMEOUT_AFTER_COMMIT", match={"nth_call": 1}, params={"delay_ms": 3500})))
        self.assertEqual(s.unsupported, [])
        assert s.mock_case is not None
        self.assertEqual(Contracts().errors("fault-plan", s.mock_case), [], "what mock-case is given is itself a fault-plan.v1 document")
        self.assertEqual((s.mock_case["source"], s.mock_case["deterministic"]), ("MOCK_CASE", True))
        self.assertEqual((s.mock_case["fault_plan_id"], s.mock_case["scenario_id"], s.mock_case["seed"]), ("fp_ben_tmo_0001", "BEN-TMO-0001", 5))
        self.assertEqual(s.mock_case["faults"], [fault("TIMEOUT_AFTER_COMMIT", match={"nth_call": 1}, params={"delay_ms": 3500})])

    def test_a_fault_aimed_at_a_source_this_build_cannot_reach_is_unsupported_not_dropped(self) -> None:
        s = split_plan(case(), plan(fault("STALE_READ", target="mock-ledger", operation="get_transaction", params={"seconds": 61}), fault("DROP_RESPONSE")))
        assert s.mock_case is not None
        self.assertEqual(len(s.mock_case["faults"]), 1)
        self.assertEqual(len(s.unsupported), 1)
        self.assertIn("mock-ledger", s.unsupported[0])

    def test_every_non_mock_case_fault_is_listed(self) -> None:
        s = split_plan(case(), plan(fault("DELAYED_REVERSAL", "mock-ledger", "get_reversal"), fault("INJECTION_PAYLOAD", "mock-customer", "get_beneficiary", params={"field": "name"}),
                                    source="TUSHE"))
        self.assertEqual(s.mock_case, None)
        self.assertEqual(len(s.unsupported), 2)

    def test_a_clock_advance_starts_the_simulated_clock(self) -> None:
        s = split_plan(case(["clock_advance:PT31S"]), None)
        assert s.mock_case is not None
        self.assertEqual(s.mock_case["clock"], {"start": CLOCK_START})
        self.assertEqual(s.mock_case["faults"], [])
        self.assertEqual(Contracts().errors("fault-plan", s.mock_case), [])
        self.assertEqual(s.mock_case["fault_plan_id"], "fp_ben_tmo_0001")

    def test_the_clock_joins_a_plan_with_faults(self) -> None:
        s = split_plan(case(["clock_advance:PT31S"]), plan(fault("DROP_RESPONSE")))
        assert s.mock_case is not None
        self.assertEqual((len(s.mock_case["faults"]), s.mock_case["clock"]), (1, {"start": CLOCK_START}))


if __name__ == "__main__":
    unittest.main()
