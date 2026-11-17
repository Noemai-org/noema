"""UND-16 (noema#546, A198): the five golden examples G-A to G-E of the Intent Compiler as synthetic canonical inputs, and the check each proposal must meet (PERD, the table of golden examples).
Written before `benchmark.compile_acceptance.golden` exists. All data is SYNTHETIC (AGENT-25). Nothing here reads the oracle or the held-out set (INV-22)."""
import copy
import re
import unittest
from typing import Any

from benchmark.compile_acceptance.golden import GOLDEN, NODE, canonical_input, check

UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
TEXTS = {
    "G-A": "Wetin happen to the 20k wey I send Tunde this morning?",
    "G-B": "Check the transfer to Tunde",
    "G-C": "The money comot this morning but e never reach",
    "G-D": "Has the 50k I sent yesterday been reversed?",
    "G-E": "Should I send it again? Just resend am for me",
}


def good(case_id: str) -> dict[str, Any]:
    """A proposal that meets the check of one golden example (the shape the assembler gives: noema_model tests, test_compile_assemble)."""
    slots: dict[str, Any] = {}
    g = next(x for x in GOLDEN if x.case_id == case_id)
    slots["question_type"] = {"type": "Reference", "value": g.question_type}
    if g.remedy:
        slots["requested_remedy"] = {"type": "Reference", "value": g.remedy}
    if g.amount_minor is not None:
        slots["amount"] = {"type": "Quantity", "unit": "NGN", "value": g.amount_minor, "status": "RESOLVED", "confidence": 9000, "surface_form": "x"}
    if g.counterparty:
        slots["counterparty"] = {"type": "Party", "surface_form": g.counterparty, "confidence": 9000}
    if g.when:
        slots["when"] = {"type": "TimeRef", "surface_form": g.when, "status": "RESOLVED", "confidence": 8000, "window": ["2026-11-16T05:00:00Z", "2026-11-16T12:00:00Z"]}
    return {"intent_node": NODE, "slots": slots}


class Examples(unittest.TestCase):
    def test_the_five_examples_are_in_order_with_the_texts_of_the_perd(self) -> None:
        self.assertEqual([g.case_id for g in GOLDEN], ["G-A", "G-B", "G-C", "G-D", "G-E"])
        self.assertEqual({g.case_id: g.text for g in GOLDEN}, TEXTS)

    def test_the_canonical_input_is_a_synthetic_valid_one_and_the_same_every_time(self) -> None:
        for g in GOLDEN:
            c = canonical_input(g)
            self.assertEqual(c, canonical_input(g))
            self.assertEqual(c["text"], g.text)
            self.assertEqual((c["schema_version"], c["channel"], c["mode"]), ("2.0", "text", "MODE_1"))
            self.assertRegex(c["request_id"], UUID)
            self.assertTrue(c["tenant_id"].startswith("ten_") and c["principal_ref"].startswith("usr_") and c["session_id"].startswith("ses_"))
            self.assertTrue(c["received_at"].endswith("Z"))
        self.assertEqual(len({canonical_input(g)["request_id"] for g in GOLDEN}), 5)

    def test_no_digit_run_over_four_is_in_an_id_a_person_could_confuse_with_an_account(self) -> None:
        for g in GOLDEN:
            for key in ("tenant_id", "correlation_id", "session_id", "principal_ref"):
                self.assertNotRegex(canonical_input(g)[key], r"\d{5}")


class Check(unittest.TestCase):
    def test_a_proposal_that_meets_each_example_has_no_problem(self) -> None:
        for g in GOLDEN:
            self.assertEqual(check(g, good(g.case_id)), [], g.case_id)

    def test_the_expectations_are_the_perd_table(self) -> None:
        by = {g.case_id: g for g in GOLDEN}
        self.assertEqual((by["G-A"].question_type, by["G-A"].amount_minor, by["G-A"].counterparty, by["G-A"].when), ("STATUS", 2_000_000, "Tunde", "this morning"))
        self.assertEqual((by["G-B"].question_type, by["G-B"].counterparty), ("STATUS", "Tunde"))
        self.assertEqual((by["G-C"].question_type, by["G-C"].amount_minor, by["G-C"].when, "amount" in by["G-C"].absent), ("STATUS", None, "this morning", True))
        self.assertEqual((by["G-D"].question_type, by["G-D"].amount_minor, by["G-D"].when), ("REVERSAL_STATUS", 5_000_000, "yesterday"))
        self.assertEqual((by["G-E"].question_type, by["G-E"].remedy), ("RESEND_ADVICE", "fin.cap.transfer_funds"))
        self.assertEqual(NODE, "fin.payments.exception.investigate")

    def test_each_departure_is_a_problem_that_names_it(self) -> None:
        for g in GOLDEN:
            wrong_node = {**good(g.case_id), "intent_node": "fin.meta.out_of_scope"}
            self.assertTrue(any("node" in p for p in check(g, wrong_node)), g.case_id)
            wrong_q = copy.deepcopy(good(g.case_id))
            wrong_q["slots"]["question_type"]["value"] = "OTHER"
            self.assertTrue(any("question_type" in p for p in check(g, wrong_q)), g.case_id)
            missing_q = copy.deepcopy(good(g.case_id))
            del missing_q["slots"]["question_type"]
            self.assertTrue(any("question_type" in p for p in check(g, missing_q)), g.case_id)

    def test_an_amount_must_be_exact_and_an_invented_one_is_a_problem(self) -> None:
        a = next(g for g in GOLDEN if g.case_id == "G-A")
        wrong = copy.deepcopy(good("G-A"))
        wrong["slots"]["amount"]["value"] = 2_000_001
        self.assertTrue(any("amount" in p for p in check(a, wrong)))
        in_dollars = copy.deepcopy(good("G-A"))
        in_dollars["slots"]["amount"]["unit"] = "USD"
        self.assertTrue(any("amount" in p for p in check(a, in_dollars)))
        c = next(g for g in GOLDEN if g.case_id == "G-C")
        invented = copy.deepcopy(good("G-C"))
        invented["slots"]["amount"] = {"type": "Quantity", "unit": "NGN", "value": 100, "status": "RESOLVED", "confidence": 9000}
        self.assertTrue(any("amount" in p and "invented" in p for p in check(c, invented)))

    def test_the_prohibited_remedy_must_be_named_and_a_remedy_where_none_is_expected_is_a_problem(self) -> None:
        e = next(g for g in GOLDEN if g.case_id == "G-E")
        missing = copy.deepcopy(good("G-E"))
        del missing["slots"]["requested_remedy"]
        self.assertTrue(any("requested_remedy" in p for p in check(e, missing)))
        a = next(g for g in GOLDEN if g.case_id == "G-A")
        extra = copy.deepcopy(good("G-A"))
        extra["slots"]["requested_remedy"] = {"type": "Reference", "value": "fin.cap.transfer_funds"}
        self.assertTrue(any("requested_remedy" in p for p in check(a, extra)))

    def test_a_proposal_that_is_not_an_object_or_has_no_slots_is_a_problem_not_an_error(self) -> None:
        g = GOLDEN[0]
        for bad in ({}, {"intent_node": NODE}, {"intent_node": NODE, "slots": []}, {"intent_node": NODE, "slots": {"question_type": "STATUS"}}):
            self.assertTrue(check(g, bad), bad)


if __name__ == "__main__":
    unittest.main()
