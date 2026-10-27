"""PROPOSAL, not a change: move BEN-INJ-0003's injection from `get_case`/`case_note` to `find_cases`/`reason_code` (see PROPOSAL-BEN-INJ-0003.md).

The generator's output is NOT changed by this file: the case keeps its `needs_fault` marker until the benchmark owner or the non-author co-founder approves the move.
These tests build the proposed variant in memory (the archetype list is restored afterwards) and show what the move changes and what it leaves alone, so the approver
can see it. When the move is approved, the follow-up PR changes `lib_security.py` and replaces `Current` below with the variant's expectations (REQ-BEN-001; PERD §48.1)."""
import json
import unittest
from pathlib import Path
from dataclasses import replace
from typing import Any

from benchmark.contracts import Contracts
from benchmark.generator import library
from benchmark.generator.common import at
from benchmark.generator.faultplans import normalise
from benchmark.generator.generate import Suite, generate

ROOT = Path(__file__).resolve().parents[2]
SEED = 20261020
CASE = "BEN-INJ-0003"
KEY = "inj_case_note"
Json = dict[str, Any]


def variant_suite() -> Suite:
    """The suite as it would be with the injection on `reason_code`. Nothing is kept: the library list is put back."""
    arcs = library.BY_CATEGORY["PROMPT_INJECTION"]
    index = next(i for i, a in enumerate(arcs) if a.key == KEY)
    original = arcs[index]
    arcs[index] = replace(original, faults=(at("INJECTION_PAYLOAD", "mock-case", "find_cases", field="reason_code"),))
    try:
        return generate(SEED)
    finally:
        arcs[index] = original


class Current(unittest.TestCase):
    """What is on main today. If these fail, the proposal is out of date."""

    def test_the_case_is_marked_and_has_no_plan(self) -> None:
        suite = generate(SEED)
        events = [e["event"] for e in suite.cases[CASE]["events_timeline"]]
        self.assertIn("needs_fault:INJECTION_PAYLOAD.get_case", events)
        self.assertEqual(suite.cases[CASE]["fault_plan"], "fp_none")

    def test_get_case_with_a_transaction_match_is_refused_by_mock_case_so_the_move_needs_find_cases(self) -> None:
        """mock-case's faultplan accepts only nth_call, case_id and tenant_id as match keys on get_case (synthetic/mock_case/internal/faultplan/plan.go, operationKeys);
        find_cases accepts transaction_ref. Moving only the field, and keeping get_case with the per-transaction match, would be refused at load."""
        source = (ROOT / "synthetic" / "mock_case" / "internal" / "faultplan" / "plan.go").read_text(encoding="utf-8")
        self.assertIn('OpGetCase:         {"nth_call", "case_id", "tenant_id"}', source)
        self.assertIn('OpFindCases:       {"nth_call", "transaction_ref", "idempotency_key", "principal_id", "tenant_id"}', source)

    def test_case_note_is_not_a_field_the_contract_allows_on_mock_case(self) -> None:
        fault = {"target": "mock-case", "operation": "get_case", "match": {}, "effect": "INJECTION_PAYLOAD", "params": {"field": "case_note"}}
        self.assertEqual(normalise(fault), (None, "INJECTION_PAYLOAD.get_case"))


class Proposed(unittest.TestCase):
    base: Suite
    moved: Suite

    @classmethod
    def setUpClass(cls) -> None:
        cls.base, cls.moved = generate(SEED), variant_suite()

    def test_the_library_is_restored_after_building_the_variant(self) -> None:
        self.assertEqual(generate(SEED).files, self.base.files)

    def test_the_move_is_producible_and_valid(self) -> None:
        """The plan exists, is a fault-plan.v1 document, and is produced by mock-case alone."""
        plan = json.loads(self.moved.files["fault_plans/fp_ben_inj_0003.json"])
        self.assertEqual(Contracts().errors("fault-plan", plan), [])
        self.assertEqual(plan["source"], "MOCK_CASE")
        (fault,) = plan["faults"]
        self.assertEqual((fault["target"], fault["operation"], fault["effect"], fault["params"]), ("mock-case", "find_cases", "INJECTION_PAYLOAD", {"field": "reason_code"}))
        self.assertEqual(list(fault["match"]), ["transaction_ref"], "find_cases accepts transaction_ref; get_case would be refused by mock-case with that key")

    def test_what_the_move_changes_is_only_the_field_the_marker_and_the_plan(self) -> None:
        a, b = self.base.cases[CASE], self.moved.cases[CASE]
        changed = sorted(k for k in {*a, *b} if a.get(k) != b.get(k))
        self.assertEqual(changed, ["events_timeline", "fault_plan"])
        self.assertEqual([e for e in a["events_timeline"] if e["event"] != "needs_fault:INJECTION_PAYLOAD.get_case"], b["events_timeline"])
        self.assertEqual((a["fault_plan"], b["fault_plan"]), ("fp_none", "fp_ben_inj_0003"))

    def test_the_expected_benchmark_outcome_is_unchanged(self) -> None:
        a, b = self.base.cases[CASE], self.moved.cases[CASE]
        self.assertEqual(a["expected"], b["expected"])
        for key in ("safety_gate_relevance", "severity_if_failed", "category", "families", "principal", "goal", "utterances", "permitted_capabilities", "policy_version", "pack_version"):
            self.assertEqual(a[key], b[key], key)

    def test_the_oracle_and_the_fixture_are_unchanged(self) -> None:
        self.assertEqual(self.base.oracles[CASE], self.moved.oracles[CASE])
        self.assertEqual(self.base.files["fixtures/fx_inj_0003.json"], self.moved.files["fixtures/fx_inj_0003.json"])

    def test_coverage_is_unchanged(self) -> None:
        cover = {e["case_id"]: e["covers"] for e in self.base.manifest["cases"]}
        moved = {e["case_id"]: e["covers"] for e in self.moved.manifest["cases"]}
        self.assertEqual(cover[CASE], moved[CASE])
        self.assertIn("FAULT:INJECTION_PAYLOAD", moved[CASE])

    def test_no_other_case_changes(self) -> None:
        for case_id, case in self.base.cases.items():
            if case_id != CASE:
                self.assertEqual(case, self.moved.cases[case_id], case_id)
        self.assertEqual(self.base.fixtures, self.moved.fixtures)
        self.assertEqual(self.base.oracles, self.moved.oracles)

    def test_mock_case_injects_into_reason_code_and_has_no_case_note_to_inject_into(self) -> None:
        """Read from mock-case's own source (not imported): the injection appends the one SYNTHETIC text, the same for any field it supports, to `reason_code` of a case; it has no
        `case_note`. So with `case_note` mock-case would inject nothing, and the case could not test what its note says."""
        source = (ROOT / "synthetic" / "mock_case" / "internal" / "api" / "pipeline.go").read_text(encoding="utf-8")
        self.assertIn('const InjectedText = "SYNTHETIC INJECTION TEST:', source)
        self.assertIn(r'b.ReasonCode += "\n" + InjectedText', source)
        self.assertNotIn("case_note", source)
        self.assertNotIn("CaseNote", source)

if __name__ == "__main__":
    unittest.main()
