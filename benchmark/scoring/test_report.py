"""The human report (REQ-BEN-004): Tier 0 first and beside every tier; n, method, conditions and bound on every number."""
import copy
import re
import unittest
from typing import Any

from benchmark.contracts import Contracts, sha256_of
from benchmark.scoring.report import ReportError, render_markdown
from benchmark.scoring.scorecard import score
from benchmark.scoring.testdata import GATE_MUTATIONS, REGISTRY, clean_result, renumber, templates

CONTRACTS = Contracts()


def fixture(held_out: int = 0) -> tuple[list[dict[str, Any]], list[dict[str, Any]], list[dict[str, Any]], dict[str, Any]]:
    pairs = [renumber(t, f"BEN-{t[0]['case_id'].split('-')[1]}-{i:04d}") for t in templates() for i in (1, 2)]
    cases = [p[0] for p in pairs]
    for c in cases[:held_out]:
        c["suite_layer"] = "HELD_OUT"
    oracles = [p[1] for p in pairs]
    results = [clean_result(c, o) for c, o in pairs]
    manifest: dict[str, Any] = {
        "suite_id": "generated-test", "suite_version": "0.1.0",
        "generator": {"name": "noema-casegen", "version": "0.1.0", "seed": 42},
        "layer_counts": {"GENERATED": len(cases) - held_out, "HELD_OUT": held_out, "PARTNER": 0},
        "category_counts": {k: 0 for k in ("CLEAN_UNIQUE", "AMBIGUITY", "STATE_CONFLICT", "TIMEOUT_UNKNOWN", "DUPLICATE_REPLAY",
                                           "AUTHORISATION", "POLICY_CLOCK", "PROMPT_INJECTION", "STALE_SESSION", "INTEGRATION_HOSTILITY")},
        "pidgin_or_code_switched": {"count": 2, "of": len(cases)},
        "cases": [{"case_id": c["case_id"], "version": 1, "suite_layer": c["suite_layer"], "category": c["category"],
                   "sha256": sha256_of(c), "covers": ["US-01", "FAULT:TIMEOUT_AFTER_COMMIT"], "base_case_id": None, "mutation": None}
                  for c in cases],
    }
    for c in cases:
        manifest["category_counts"][c["category"]] += 1
    return cases, oracles, results, manifest


def card_for(cases: list[dict[str, Any]], oracles: list[dict[str, Any]], results: list[dict[str, Any]], **kw: Any) -> dict[str, Any]:
    return score(cases, oracles, results, REGISTRY, run_id="run_report", **kw)


class ReportTest(unittest.TestCase):
    def setUp(self) -> None:
        cases, oracles, results, self.manifest = fixture()
        self.card = card_for(cases, oracles, results)
        self.text = render_markdown(self.card, self.manifest)

    def test_the_fixture_manifest_is_valid(self) -> None:
        self.assertEqual(CONTRACTS.errors("benchmark-suite-manifest", self.manifest), [])

    def test_tier_zero_is_printed_before_every_other_tier(self) -> None:
        order = [self.text.index(h) for h in ("## Tier 0", "## Tier 1", "## Tier 2", "## Tier 3")]
        self.assertEqual(order, sorted(order))

    def test_all_eleven_gates_are_listed(self) -> None:
        section = self.text.split("## Tier 1")[0]
        for gate in GATE_MUTATIONS:
            self.assertIn(gate, section)

    def test_every_tier_section_carries_the_tier_zero_status_line(self) -> None:
        for header in ("## Tier 1", "## Tier 2", "## Tier 3"):
            body = self.text.split(header, 1)[1].split("\n## ", 1)[0]
            self.assertIn("Tier 0: all 11 gates at zero", body, header)

    def test_a_failing_gate_is_named_beside_every_tier(self) -> None:
        cases, oracles, results, manifest = fixture()
        GATE_MUTATIONS["FALSE_SUCCESS_VERDICT"](results[0])
        text = render_markdown(card_for(cases, oracles, results), manifest)
        for header in ("## Tier 1", "## Tier 2", "## Tier 3"):
            body = text.split(header, 1)[1].split("\n## ", 1)[0]
            self.assertIn("Tier 0: 1 gate NOT at zero (FALSE_SUCCESS_VERDICT)", body, header)

    def test_every_rate_shows_its_n(self) -> None:
        for header in ("## Tier 1", "## Tier 2"):
            body = self.text.split(header, 1)[1].split("\n## ", 1)[0]
            bullets = [line for line in body.splitlines() if line.startswith("- ")]
            self.assertTrue(bullets)
            for line in bullets:
                self.assertRegex(line, r"\d+ of \d+", line)

    def test_states_how_the_cases_were_generated_and_the_conditions(self) -> None:
        self.assertIn("noema-casegen 0.1.0", self.text)
        self.assertIn("seed 42", self.text)
        self.assertIn("TIMEOUT_AFTER_COMMIT 6", self.text)
        self.assertIn("2 of 6", self.text)  # the Pidgin or code-switched share, with its n

    def test_states_the_rule_of_three_bound(self) -> None:
        self.assertRegex(self.text, r"0 of 6 .*below about 50\.00% at 95% confidence")

    def test_a_generated_only_run_says_it_is_not_a_release_result(self) -> None:
        self.assertIn("NOT A RELEASE RESULT", self.text)

    def test_an_incomplete_run_says_so_at_the_top(self) -> None:
        cases, oracles, results, manifest = fixture()
        text = render_markdown(card_for(cases, oracles, results[:-1]), manifest)
        self.assertIn("RUN INCOMPLETE", text.split("## Tier 0")[0])

    def test_failures_are_listed_for_regression_cases(self) -> None:
        cases, oracles, results, manifest = fixture()
        GATE_MUTATIONS["ORACLE_ACCESS"](results[2])
        text = render_markdown(card_for(cases, oracles, results), manifest)
        self.assertIn(cases[2]["case_id"], text.split("## Failures")[1])

    def test_rendering_is_deterministic(self) -> None:
        self.assertEqual(self.text, render_markdown(copy.deepcopy(self.card), copy.deepcopy(self.manifest)))

    def test_cost_not_available_is_said_plainly(self) -> None:
        cases, oracles, results, manifest = fixture()
        for r in results:
            r["cost_usd_micros"] = None
        text = render_markdown(card_for(cases, oracles, results), manifest)
        line = next(line for line in text.splitlines() if line.startswith("- provider cost"))
        self.assertIn("not available", line)
        self.assertNotIn("None", text)

    def test_no_threshold_or_verdict_word_is_decided_by_the_report(self) -> None:
        for word in ("PASS", "FAIL", "target met", "meets the target"):
            self.assertNotIn(word, self.text)


class GuardTest(unittest.TestCase):
    def test_no_report_without_tier_zero(self) -> None:
        cases, oracles, results, manifest = fixture()
        card = card_for(cases, oracles, results)
        for broken in (
            {k: v for k, v in card.items() if k != "tier0"},
            {**card, "tier0": {**card["tier0"], "gates": card["tier0"]["gates"][:10]}},
            {**card, "tier0": None},
        ):
            with self.assertRaises(ReportError):
                render_markdown(broken, manifest)

    def test_manifest_and_scorecard_must_describe_the_same_suite(self) -> None:
        cases, oracles, results, manifest = fixture()
        card = card_for(cases, oracles, results)
        manifest["cases"].pop()
        with self.assertRaises(ReportError):
            render_markdown(card, manifest)

    def test_layer_counts_must_agree(self) -> None:
        cases, oracles, results, manifest = fixture()
        card = card_for(cases, oracles, results)
        manifest["layer_counts"]["GENERATED"] -= 1
        with self.assertRaises(ReportError):
            render_markdown(card, manifest)


if __name__ == "__main__":
    unittest.main()
