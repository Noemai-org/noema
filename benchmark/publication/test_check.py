"""BEN-31, REQ-BEN-010 (DEC-2026-10-31 D28): publication rules as a check. Every external number traces to a run ID in the evidence bundle and carries n, method, bound and
confidence interval; the full distribution is shown; outcomes are judged against the final state of the systems. Written before `check.py` and seen failing.
SYNTHETIC scorecards from the real scorer. Generated layer only (INV-22, REQ-AGT-004)."""
import copy
import io
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from typing import Any

from benchmark.contracts import pretty_bytes
from benchmark.publication.__main__ import main
from benchmark.publication.check import build_claim, check
from benchmark.scoring.test_report import card_for, fixture

Json = dict[str, Any]
RUN = "run_report"


def scorecard() -> Json:
    cases, oracles, results, _ = fixture()
    return card_for(cases, oracles, results)


def distributions(card: Json) -> list[Json]:
    return [{"run_id": card["run_id"], "slice": s, "rows": {k: {"n": v["n"], "k": v["correct"]["k"]} for k, v in card[s].items()}} for s in ("by_family", "by_category")]


def publication(card: Json | None = None, **claim_changes: Any) -> tuple[Json, Json]:
    card = card or scorecard()
    tier0 = build_claim(card, "C0", "/tier0/gates/0/rate", "Unsafe consequential actions, Tier 0")
    claim = build_claim(card, "C1", "/tier1/correct_terminal_handling", "Correct terminal handling on the generated suite")
    claim.update(claim_changes)
    return ({"publication_id": "pub-test", "claims": [tier0, claim], "distributions": distributions(card)}, {card["run_id"]: card})


def problems(pub: Json, cards: Json) -> list[str]:
    return check(pub, cards)


class Valid(unittest.TestCase):
    def test_a_publication_built_from_a_scorecard_passes(self) -> None:
        pub, cards = publication()
        self.assertEqual([], problems(pub, cards))

    def test_a_built_claim_carries_every_required_field(self) -> None:
        claim = publication()[0]["claims"][1]
        for field in ("id", "text", "run_id", "scorecard_ref", "k", "n", "rate", "ci95", "method", "population", "outcome_basis", "tier0_all_zero"):
            self.assertIn(field, claim)
        self.assertEqual("ORACLE_FINAL_STATE", claim["outcome_basis"])
        self.assertEqual("Wilson 95% interval", claim["method"])


class Broken(unittest.TestCase):
    def one(self, mutate: Any) -> list[str]:
        pub, cards = publication()
        mutate(pub, cards)
        return problems(pub, cards)

    def test_a_missing_field_is_named(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].pop("method"))
        self.assertTrue(any("C1" in x and "method" in x for x in got), got)

    def test_a_run_id_with_no_scorecard_in_the_bundle_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(run_id="run_nope"))
        self.assertTrue(any("run_nope" in x for x in got), got)

    def test_a_number_that_does_not_match_the_scorecard_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(k=p["claims"][1]["k"] - 1))
        self.assertTrue(any("C1" in x and "does not match" in x for x in got), got)

    def test_an_interval_that_does_not_match_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(ci95=[0.0, 1.0]))
        self.assertTrue(any("ci95" in x for x in got), got)

    def test_a_pointer_that_is_not_a_rate_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(scorecard_ref="/tier1/by_disposition"))
        self.assertTrue(any("not a rate" in x for x in got), got)

    def test_a_method_other_than_the_scorers_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(method="normal approximation"))
        self.assertTrue(any("method" in x for x in got), got)

    def test_a_number_in_the_text_that_no_field_backs_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(text="Noema handles 99.9% of cases correctly"))
        self.assertTrue(any("99.9" in x for x in got), got)

    def test_numbers_in_the_text_that_the_claim_backs_are_accepted(self) -> None:
        pub, cards = publication()
        claim = pub["claims"][1]
        claim["text"] = f"{claim['k']} of {claim['n']} cases ({claim['rate'] * 100:.2f}%) were handled correctly"
        self.assertEqual([], problems(pub, cards))

    def test_a_zero_failure_claim_needs_its_rule_of_three_bound(self) -> None:
        pub, cards = publication()
        ref = "/tier0/gates/0/rate"
        claim = build_claim(cards[RUN], "C2", ref, "Unsafe consequential actions")
        pub["claims"].append(claim)
        self.assertEqual([], problems(pub, cards))
        del claim["bound"]
        self.assertTrue(any("C2" in x and "bound" in x for x in problems(pub, cards)))

    def test_a_publication_with_no_tier_zero_claim_for_a_run_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"].pop(0))
        self.assertTrue(any("Tier 0" in x and RUN in x for x in got), got)

    def test_a_tier_one_claim_listed_before_the_tier_zero_claim_of_its_run_is_refused(self) -> None:
        # Codex review of noema#124 (R4, DEC-2026-10-31 D28, SoT 38.8): Tier 0 is published first, beside every other number.
        def mutate(p: Json, c: Json) -> None:
            p["claims"].reverse()
        got = self.one(mutate)
        self.assertTrue(any("C1" in x and "before" in x and "Tier 0" in x for x in got), got)

    def test_tier_zero_claims_may_come_first_in_any_number_and_other_runs_are_ordered_on_their_own(self) -> None:
        pub, cards = publication()
        second = copy.deepcopy(cards[RUN])
        second["run_id"] = "run_two"
        cards["run_two"] = second
        pub["claims"] += [build_claim(second, "D0", "/tier0/gates/0/rate", "Unsafe consequential actions, Tier 0"), build_claim(second, "D1", "/tier1/correct_terminal_handling", "Correct handling")]
        pub["distributions"] += distributions(second)
        self.assertEqual([], problems(pub, cards))
        pub["claims"] = [pub["claims"][0], pub["claims"][1], pub["claims"][3], pub["claims"][2]]  # the second run's Tier 1 claim D1 now precedes its Tier 0 claim D0
        self.assertTrue(any("run_two" in x and "before" in x for x in problems(pub, cards)))

    def test_an_incomplete_run_is_refused(self) -> None:
        def mutate(p: Json, c: Json) -> None:
            c[RUN]["complete"] = False
        self.assertTrue(any("complete" in x for x in self.one(mutate)))

    def test_a_non_release_run_must_say_so_in_its_population(self) -> None:
        pub, cards = publication()
        self.assertFalse(cards[RUN]["release_result"])
        pub["claims"][1]["population"] = "the generated suite"
        self.assertTrue(any("not a release result" in x for x in problems(pub, cards)))

    def test_a_missing_family_in_the_distribution_is_named_as_cherry_picking(self) -> None:
        def mutate(p: Json, c: Json) -> None:
            del p["distributions"][0]["rows"]["B4"]
        got = self.one(mutate)
        self.assertTrue(any("B4" in x and "full distribution" in x for x in got), got)

    def test_a_distribution_value_that_does_not_match_is_refused(self) -> None:
        def mutate(p: Json, c: Json) -> None:
            p["distributions"][1]["rows"]["AMBIGUITY"]["k"] += 1
        self.assertTrue(any("AMBIGUITY" in x for x in self.one(mutate)))

    def test_a_publication_without_distributions_for_a_cited_run_is_refused(self) -> None:
        got = self.one(lambda p, c: p.update(distributions=[]))
        self.assertTrue(any("by_family" in x for x in got), got)

    def test_pass_k_that_is_unavailable_cannot_be_published(self) -> None:
        pub, cards = publication()
        cards[RUN]["tier1"]["diagnostics"]["pass_k"] = {"k_required": 5, "runs_supplied": 1, "available": False, "reason": "1 run", "rate": None, "failing_cases": []}
        pub["claims"].append({**pub["claims"][1], "id": "C3", "scorecard_ref": "/tier1/diagnostics/pass_k/rate"})
        self.assertTrue(any("C3" in x and "pass^k" in x for x in problems(pub, cards)))

    def test_duplicate_claim_ids_are_refused(self) -> None:
        pub, cards = publication()
        pub["claims"].append(copy.deepcopy(pub["claims"][1]))
        self.assertTrue(any("duplicate" in x for x in problems(pub, cards)))

    def test_outcomes_must_be_judged_against_the_final_state(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(outcome_basis="MODEL_SAID"))
        self.assertTrue(any("outcome_basis" in x for x in got), got)

    def test_a_tier_zero_status_that_is_not_the_scorecards_is_refused(self) -> None:
        got = self.one(lambda p, c: p["claims"][1].update(tier0_all_zero=False))
        self.assertTrue(any("tier0_all_zero" in x for x in got), got)

    def test_words_the_release_rules_forbid_are_refused_in_a_claim_text(self) -> None:
        # docs/poc/execution/release/claims.md section 3: "Words we do not use".
        for word in ("tamper-proof", "immutable", "compliant", "certified", "guaranteed", "zero risk", "proven in production", "Tamper-Proof"):
            got = self.one(lambda p, c, w=word: p["claims"][1].update(text=f"The Record is {w}"))
            self.assertTrue(any(word.lower() in x.lower() and "claims.md" in x for x in got), (word, got))

    def test_tamper_evident_is_allowed(self) -> None:
        pub, cards = publication()
        pub["claims"][1]["text"] = "The Record is tamper-evident"
        self.assertEqual([], problems(pub, cards))

    def test_the_input_is_not_changed(self) -> None:
        pub, cards = publication()
        before = (copy.deepcopy(pub), copy.deepcopy(cards))
        problems(pub, cards)
        self.assertEqual(before, (pub, cards))


class Cli(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)

    def write(self, pub: Json, cards: Json) -> tuple[Path, Path]:
        bundle = self.root / "bundle"
        bundle.mkdir(exist_ok=True)
        for run_id, card in cards.items():
            (bundle / f"{run_id}.json").write_bytes(pretty_bytes(card))
        path = self.root / "pub.json"
        path.write_bytes(pretty_bytes(pub))
        return path, bundle

    def run_main(self, *argv: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = main(list(argv))
        return code, out.getvalue(), err.getvalue()

    def test_a_clean_publication_exits_0(self) -> None:
        path, bundle = self.write(*publication())
        code, out, _ = self.run_main("check", str(path), "--scorecards", str(bundle))
        self.assertEqual(0, code)
        self.assertIn("2 claims", out)

    def test_a_broken_publication_exits_1_and_lists_every_problem(self) -> None:
        pub, cards = publication()
        pub["claims"][1]["k"] += 1
        pub["claims"][1]["method"] = "guess"
        path, bundle = self.write(pub, cards)
        code, out, _ = self.run_main("check", str(path), "--scorecards", str(bundle))
        self.assertEqual(1, code)
        self.assertGreaterEqual(out.count("C1"), 2)

    def test_an_unreadable_input_exits_2(self) -> None:
        code, _, err = self.run_main("check", str(self.root / "missing.json"), "--scorecards", str(self.root))
        self.assertEqual(2, code)
        self.assertIn("missing.json", err)

    def test_an_argument_naming_the_held_out_set_is_refused(self) -> None:
        code, _, err = self.run_main("check", "x.json", "--scorecards", "/a/benchmark-heldout")
        self.assertEqual(2, code)
        self.assertIn("benchmark-heldout", err)

    def test_claim_prints_a_claim_that_passes_the_check(self) -> None:
        pub, cards = publication()
        _, bundle = self.write(pub, cards)
        code, out, _ = self.run_main("claim", "--scorecard", str(bundle / f"{RUN}.json"), "--id", "C9", "--ref", "/tier1/correct_terminal_handling", "--text", "Correct terminal handling")
        self.assertEqual(0, code)
        claim = json.loads(out)
        pub["claims"].append(claim)
        self.assertEqual([], problems(pub, cards))


if __name__ == "__main__":
    unittest.main()
