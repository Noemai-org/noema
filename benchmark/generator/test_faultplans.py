"""The fault plans the generator writes are fault-plan.v1 documents (PERD §37.1, REQ-ENV-004), and a fault no producer can produce is named, never dropped."""
import json
import unittest
from functools import lru_cache
from typing import Any

from benchmark.contracts import Contracts
from benchmark.generator.faultplans import MOCK_CASE_EFFECTS, TUSHE_EFFECTS, normalise, plan_document, source_of
from benchmark.generator.generate import Suite, generate

SEED = 20261020
Json = dict[str, Any]


@lru_cache(maxsize=None)
def suite() -> Suite:
    return generate(SEED)


def plans() -> dict[str, Json]:
    return {p: json.loads(b) for p, b in suite().files.items() if p.startswith("fault_plans/")}


def fault(target: str, effect: str, operation: str = "op", params: Json | None = None, match: Json | None = None) -> Json:
    return {"target": target, "operation": operation, "match": match or {}, "effect": effect, "params": params or {}}


class Emitted(unittest.TestCase):
    def test_every_plan_validates_against_the_fault_plan_contract(self) -> None:
        contracts = Contracts()
        found = plans()
        self.assertGreater(len(found), 100)
        for path, doc in found.items():
            self.assertEqual(contracts.errors("fault-plan", doc), [], path)

    def test_a_plan_names_its_case_its_seed_and_is_deterministic(self) -> None:
        for path, doc in plans().items():
            case_id = doc["scenario_id"]
            self.assertEqual(suite().cases[case_id]["fault_plan"], doc["fault_plan_id"], path)
            self.assertEqual((doc["seed"], doc["deterministic"]), (SEED, True))
            self.assertEqual(path, f"fault_plans/{doc['fault_plan_id']}.json")

    def test_the_provisional_wrapper_is_gone(self) -> None:
        for doc in plans().values():
            self.assertFalse({"plan_id", "case_id", "synthetic", "provisional", "harness_effects"} & set(doc))

    def test_mock_case_faults_are_only_what_mock_case_produces(self) -> None:
        for path, doc in plans().items():
            for f in doc["faults"]:
                if f["target"] == "mock-case":
                    self.assertIn(f["effect"], MOCK_CASE_EFFECTS, path)

    def test_a_mock_case_plan_holds_mock_case_faults_only(self) -> None:
        for path, doc in plans().items():
            if doc["source"] == "MOCK_CASE":
                self.assertEqual({f["target"] for f in doc["faults"]}, {"mock-case"}, path)

    def test_a_case_without_a_plan_names_fp_none(self) -> None:
        for case in suite().cases.values():
            if case["fault_plan"] == "fp_none":
                self.assertNotIn("fault_plans/fp_none.json", suite().files)


class Unproducible(unittest.TestCase):
    def test_an_unproducible_fault_is_a_timeline_marker_not_a_silent_drop(self) -> None:
        markers = [(c["case_id"], e["event"]) for c in suite().cases.values() for e in c["events_timeline"] if e["event"].startswith("needs_fault:")]
        self.assertGreaterEqual(len(markers), 8)
        effects = {m.split(":", 1)[1].split(".")[0] for _, m in markers}
        self.assertEqual(effects, {"HTTP_200_SEMANTIC_PENDING", "IDEMPOTENCY_COLLISION", "INJECTION_PAYLOAD", "NEAR_TIMEOUT_BEFORE", "NEAR_TIMEOUT_AFTER"})

    def test_a_marked_case_names_the_operation(self) -> None:
        for c in suite().cases.values():
            for e in c["events_timeline"]:
                if e["event"].startswith("needs_fault:"):
                    effect, _, operation = e["event"].split(":", 1)[1].partition(".")
                    self.assertTrue(effect.isupper() and operation, e["event"])

    def test_a_marker_is_coverage_neutral(self) -> None:
        """The case still declares the effect it needs, so the coverage report is unchanged."""
        covers = {t for e in suite().manifest["cases"] for t in e["covers"]}
        for effect in ("FAULT:HTTP_200_SEMANTIC_PENDING", "FAULT:IDEMPOTENCY_COLLISION", "FAULT:NEAR_TIMEOUT_BEFORE", "FAULT:NEAR_TIMEOUT_AFTER"):
            self.assertIn(effect, covers)


class Normalise(unittest.TestCase):
    def test_a_mock_case_effect_passes_through(self) -> None:
        f = fault("mock-case", "TIMEOUT_AFTER_COMMIT", "create_case", {"delay_ms": 3500}, {"nth_call": 1})
        self.assertEqual(normalise(f), (f, None))

    def test_other_targets_pass_through(self) -> None:
        f = fault("mock-ledger", "STALE_READ", "get_transaction", {"seconds": 61})
        self.assertEqual(normalise(f), (f, None))

    def test_service_unavailable_at_the_case_system_is_a_503(self) -> None:
        out, reason = normalise(fault("mock-case", "SERVICE_UNAVAILABLE", "create_case"))
        self.assertIsNone(reason)
        assert out is not None
        self.assertEqual((out["effect"], out["params"]), ("HTTP_5XX", {"status": 503}))

    def test_webhook_effects_belong_to_the_enterprise_not_the_case_system(self) -> None:
        for effect in ("DUPLICATE_WEBHOOK", "DROP_WEBHOOK", "REORDER_EVENTS"):
            out, _ = normalise(fault("mock-case", effect, "events_feed"))
            assert out is not None
            self.assertEqual((out["target"], out["operation"]), ("mock-destination", "events_feed"))

    def test_effects_no_producer_has_are_named(self) -> None:
        for effect in ("HTTP_200_SEMANTIC_PENDING", "IDEMPOTENCY_COLLISION", "INJECTION_PAYLOAD", "NEAR_TIMEOUT_BEFORE", "NEAR_TIMEOUT_AFTER"):
            out, reason = normalise(fault("mock-case", effect, "create_case"))
            self.assertIsNone(out)
            self.assertEqual(reason, f"{effect}.create_case")


class Sources(unittest.TestCase):
    def test_state_change_effects_need_tushe(self) -> None:
        for effect in TUSHE_EFFECTS:
            self.assertEqual(source_of([fault("mock-ledger", effect)]), "TUSHE", effect)

    def test_only_mock_case_faults_is_a_mock_case_plan(self) -> None:
        self.assertEqual(source_of([fault("mock-case", "DROP_RESPONSE")]), "MOCK_CASE")

    def test_anything_else_is_a_proxy_plan(self) -> None:
        self.assertEqual(source_of([fault("mock-ledger", "STALE_READ"), fault("mock-case", "DROP_RESPONSE")]), "PROXY")

    def test_a_plan_with_nothing_producible_is_no_plan(self) -> None:
        doc, missing = plan_document("BEN-TMO-0001", 5, [fault("mock-case", "HTTP_200_SEMANTIC_PENDING", "create_case")])
        self.assertIsNone(doc)
        self.assertEqual(missing, ["HTTP_200_SEMANTIC_PENDING.create_case"])

    def test_a_plan_document_has_the_contract_fields(self) -> None:
        doc, missing = plan_document("BEN-TMO-0001", 5, [fault("mock-case", "DROP_RESPONSE", "create_case")])
        self.assertEqual(missing, [])
        assert doc is not None
        self.assertEqual(Contracts().errors("fault-plan", doc), [])
        self.assertEqual((doc["fault_plan_id"], doc["scenario_id"], doc["source"]), ("fp_ben_tmo_0001", "BEN-TMO-0001", "MOCK_CASE"))


if __name__ == "__main__":
    unittest.main()
