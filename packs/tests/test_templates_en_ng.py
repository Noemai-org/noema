"""EXP-01 slice 2 (rulings A8, A18; REQ-EXP-002, INV-13, AGENT-17): the en-NG fallback templates of the fintech pack, as pack data.

Run: python -m unittest discover -s packs/tests

Every outcome x classification the pack can produce has an en-NG template; a template is static text that states no figure, time or name (those come only from the
facts, REQ-EXP-001); a template for an uncertain classification says so and claims nothing done; no forbidden claim appears (PERD 31.2). Other locales are EXP-04's.
"""
import json
import re
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(ROOT / "tools"))
from packcheck import checks  # noqa: E402

PACK = json.loads((ROOT / "packs" / "fintech_v1.json").read_text())
FILE = ROOT / "packs" / "fintech" / "templates" / "en-NG.json"
FORBIDDEN = [r"\bmoney is safe\b", r"\bwill be reversed\b", r"\bin \d+ hours?\b", r"\bbank failed\b", r"\bresolved\b", r"\bfixed\b", r"\bdone\b", r"\bguarantee"]
UNCERTAIN = {"CONTRADICTORY_STATE", "POSSIBLE_DUPLICATE", "EXECUTION_OUTCOME_UNKNOWN", "SETTLEMENT_PENDING", "INSUFFICIENT_EVIDENCE"}
UNCERTAINTY_MARKERS = ("not yet confirmed", "cannot confirm", "not been confirmed", "could not confirm")


def templates() -> dict[str, str]:
    return json.loads(FILE.read_text())


class EnNgTemplates(unittest.TestCase):
    def test_every_outcome_and_classification_has_a_template_and_no_other_key_exists(self) -> None:
        want = set(checks.required_template_keys(PACK, ROOT / "packs"))
        self.assertTrue(want)
        self.assertEqual(want, set(templates()))

    def test_packcheck_reports_no_en_ng_problem_in_the_real_pack(self) -> None:
        problems = [m for _, m in checks.check_templates(PACK, ROOT / "packs") if m.startswith("en-NG:")]
        self.assertEqual([], problems)

    def test_a_missing_locale_fails_closed_in_packcheck(self) -> None:
        problems = [m for _, m in checks.check_templates(PACK, ROOT / "packs") if m.startswith("xx-XX:")]
        self.assertEqual([], problems)  # xx-XX is not declared; only declared locales are required
        declared = checks.template_locales(PACK)
        self.assertIn("en-NG", declared)
        tampered = {**PACK, "understanding": {**PACK["understanding"], "locales": [*PACK["understanding"]["locales"], {"code": "xx-XX"}]}}
        missing = [m for _, m in checks.check_templates(tampered, ROOT / "packs") if m.startswith("xx-XX:")]
        self.assertTrue(missing and "missing" in missing[0])

    def test_a_template_states_no_figure_time_or_name_so_every_one_comes_from_the_facts(self) -> None:
        for key, text in templates().items():
            self.assertFalse(re.search(r"\d", text), f"{key}: a digit in a fixed template invents a figure or time")
            self.assertNotIn("{", text, f"{key}: placeholders are not part of this format")

    def test_no_forbidden_claim_appears_in_any_template(self) -> None:
        for key, text in templates().items():
            for pattern in FORBIDDEN:
                self.assertIsNone(re.search(pattern, text, re.IGNORECASE), f"{key}: matches the forbidden claim {pattern}")

    def test_an_uncertain_classification_says_it_is_uncertain_and_claims_no_success(self) -> None:
        for key, text in templates().items():
            cls = key.split("/")[1]
            lowered = text.lower()
            if cls in UNCERTAIN:
                self.assertTrue(any(m in lowered for m in UNCERTAINTY_MARKERS), f"{key}: an uncertain state must say it is not confirmed")
                self.assertNotRegex(lowered, r"\b(successful|succeeded|completed|reversed|refunded)\b", f"{key}: an uncertain state must not claim an outcome")

    def test_a_confirmed_classification_names_the_confirmed_state(self) -> None:
        got = templates()
        for key, text in got.items():
            cls = key.split("/")[1]
            if cls == "SUCCESS_CONFIRMED":
                self.assertIn("confirmed", text.lower())
            if cls == "REVERSED_CONFIRMED":
                self.assertIn("reversal", text.lower())
                self.assertIn("confirmed", text.lower())

    def test_text_is_plain_english_for_a_customer_and_carries_no_internal_code(self) -> None:
        for key, text in templates().items():
            self.assertNotRegex(text, r"[A-Z]{2,}_[A-Z_]+", f"{key}: an internal code in customer text")
            self.assertNotIn("fin.", text)
            self.assertEqual(text, text.strip())
            self.assertLessEqual(len(text), 400)


if __name__ == "__main__":
    unittest.main()
