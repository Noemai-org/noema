"""scorecard-view.v1 (ruling A250, interim: "the scorecard view for the UI is a new `scorecard-view.v1` that BO defines (aggregates only, no case or oracle refs)"). Written before `view.py` and the
schema, and seen failing. SYNTHETIC cases from the real scorer's fixtures; no oracle value is printed or asserted.

The view is a projection of scorecard v3: every number the UI may show, and no identifier of a case. A case id, a list of case ids, the failure list, the structural-evidence names and the
manual-baseline placeholder are not in it. A view of a run that has failures carries only their COUNTS."""
import copy
import json
import re
import unittest
from typing import Any, Callable, Iterator

from benchmark.contracts import Contracts
from benchmark.scoring.rates import make_rate
from benchmark.scoring.test_report import card_for, fixture
from benchmark.scoring.testdata import GATE_MUTATIONS
from benchmark.scoring.view import scorecard_view

CONTRACTS = Contracts()
CASE_ID = re.compile(r"BEN-[A-Z]{2,5}-[0-9]{4,6}")
FORBIDDEN_KEYS = {"case_id", "case_ids", "failures", "failing_cases", "oracle_ref", "correct_target", "expected", "structural_evidence", "human_minutes", "reason_detail"}


def keys(node: Any) -> set[str]:
    found: set[str] = set()
    if isinstance(node, dict):
        for k, v in node.items():
            found.add(k)
            found |= keys(v)
    elif isinstance(node, list):
        for v in node:
            found |= keys(v)
    return found


def cards() -> dict[str, dict[str, Any]]:
    cases, oracles, results, _ = fixture()
    clean = card_for(cases, oracles, results)
    cases2, oracles2, results2, _ = fixture()
    GATE_MUTATIONS["FALSE_SUCCESS_VERDICT"](results2[0])
    tripped = card_for(cases2, oracles2, results2)
    cases3, oracles3, results3, _ = fixture()
    incomplete = card_for(cases3, oracles3, results3[:-2])
    return {"clean": clean, "tripped": tripped, "incomplete": incomplete}


class View(unittest.TestCase):
    def setUp(self) -> None:
        self.cards = cards()

    def test_every_view_validates_against_the_schema(self) -> None:
        for name, card in self.cards.items():
            self.assertEqual([], CONTRACTS.errors("scorecard-view.v1", scorecard_view(card)), name)

    def test_the_inputs_really_contain_case_ids_so_the_absence_below_means_something(self) -> None:
        self.assertTrue(CASE_ID.search(json.dumps(self.cards["tripped"])))
        self.assertTrue(CASE_ID.search(json.dumps(self.cards["incomplete"])))

    def test_no_case_id_appears_anywhere_in_a_view(self) -> None:
        for name, card in self.cards.items():
            self.assertIsNone(CASE_ID.search(json.dumps(scorecard_view(card))), name)

    def test_no_forbidden_member_appears_in_a_view(self) -> None:
        for name, card in self.cards.items():
            self.assertEqual(set(), keys(scorecard_view(card)) & FORBIDDEN_KEYS, name)

    def test_the_counts_the_lists_carried_are_kept_as_counts(self) -> None:
        tripped, incomplete = self.cards["tripped"], self.cards["incomplete"]
        self.assertGreater(len(tripped["failures"]), 0)
        self.assertEqual(len(tripped["failures"]), scorecard_view(tripped)["failure_count"])
        view = scorecard_view(incomplete)
        for member, ids in incomplete["incomplete"].items():
            self.assertEqual(len(ids), view["incomplete_counts"][member], member)
        self.assertGreater(sum(view["incomplete_counts"].values()), 0)
        self.assertFalse(view["complete"])
        gates = {g["gate"]: g["count"] for g in scorecard_view(tripped)["tier0"]["gates"]}
        self.assertEqual({g["gate"]: g["count"] for g in tripped["tier0"]["gates"]}, gates)
        self.assertGreater(gates["FALSE_SUCCESS_VERDICT"], 0)
        self.assertFalse(scorecard_view(tripped)["tier0"]["all_zero"])

    def test_every_number_the_ui_may_show_is_the_scorecards_own(self) -> None:
        for card in self.cards.values():
            view = scorecard_view(card)
            self.assertEqual(card["tier1"]["correct_terminal_handling"], view["tier1"]["correct_terminal_handling"])
            self.assertEqual(card["tier2"], view["tier2"])
            self.assertEqual(card["tier0"]["severity_counts"], view["tier0"]["severity_counts"])
            self.assertEqual(card["by_family"], view["by_family"])
            self.assertEqual(card["by_category"], view["by_category"])
            self.assertEqual({k: card["tier3"][k] for k in ("cost_usd_micros_total", "cost_usd_micros_per_correct_outcome", "latency", "latency_by_path")},
                             {k: view["tier3"][k] for k in ("cost_usd_micros_total", "cost_usd_micros_per_correct_outcome", "latency", "latency_by_path")})
            for gate, row in zip(card["tier0"]["gates"], view["tier0"]["gates"]):
                self.assertEqual({"gate": gate["gate"], "count": gate["count"], "rate": gate["rate"]}, row)

    def test_the_exact_bound_of_a_failure_rate_is_carried(self) -> None:
        view = scorecard_view(self.cards["clean"])
        self.assertEqual(view["tier0"]["gates"][0]["rate"]["exact_upper95"], make_rate(0, self.cards["clean"]["n_cases"], failure=True)["exact_upper95"])

    def test_pass_k_keeps_its_rate_and_loses_its_case_list(self) -> None:
        card = copy.deepcopy(self.cards["clean"])
        card["tier1"]["diagnostics"]["pass_k"] = {"k_required": 5, "runs_supplied": 5, "available": True, "reason": None, "rate": make_rate(3, 4),
                                                   "failing_cases": ["BEN-AMB-0001"]}
        view = scorecard_view(card)
        pass_k = view["tier1"]["diagnostics"]["pass_k"]
        self.assertEqual(1, pass_k["failing_case_count"])
        self.assertNotIn("failing_cases", pass_k)
        self.assertEqual([], CONTRACTS.errors("scorecard-view.v1", view))
        self.assertIsNone(CASE_ID.search(json.dumps(view)))

    def test_it_is_deterministic_and_does_not_change_its_input(self) -> None:
        card = self.cards["tripped"]
        before = json.dumps(card, sort_keys=True)
        self.assertEqual(scorecard_view(card), scorecard_view(card))
        self.assertEqual(before, json.dumps(card, sort_keys=True))

    def test_a_scorecard_that_is_not_v3_is_refused_not_projected(self) -> None:
        card = copy.deepcopy(self.cards["clean"])
        del card["tier0"]["gates"][0]["rate"]["exact_upper95"]
        with self.assertRaises(ValueError):
            scorecard_view(card)
        with self.assertRaises(ValueError):
            scorecard_view({"run_id": "run_x"})


class Fails(unittest.TestCase):
    """Codex's two P2s on noema#642 at 4e5ff27b: (1) `by_disposition` and `latency_by_path` accepted any key, so a case id could ride in as a map key; (2) the projection copied nested members
    without checking them, so an injected nested member (a failure list under tier2, an extra member of a rate or of pass^k) survived in the view, and a tier-2 rate without its exact bound passed.
    The source is validated against scorecard v3 before it is projected, and the view is validated against scorecard-view.v1 before it is returned: either failure is a ValueError."""

    def setUp(self) -> None:
        self.card = cards()["clean"]

    def refused(self, mutate: Callable[[dict[str, Any]], Any]) -> None:
        card = copy.deepcopy(self.card)
        mutate(card)
        with self.assertRaises(ValueError):
            scorecard_view(card)

    def test_a_case_id_as_a_by_disposition_key_is_refused(self) -> None:
        self.refused(lambda c: c["tier1"]["by_disposition"].update({"BEN-AMB-0001": make_rate(1, 1)}))

    def test_a_case_id_as_a_latency_by_path_key_is_refused(self) -> None:
        self.refused(lambda c: c["tier3"]["latency_by_path"].update({"BEN-AMB-0001": {"n": 1, "p50_ms": 1, "p95_ms": 1, "p99_ms": 1}}))

    def test_an_injected_nested_member_is_refused_wherever_it_is(self) -> None:
        self.refused(lambda c: c["tier2"].update(failures=[{"case_id": "BEN-AMB-0001"}]))
        self.refused(lambda c: c["tier2"]["unsafe"].update(case_ids=["BEN-AMB-0001"]))
        self.refused(lambda c: c["tier1"]["correct_terminal_handling"].update(note="BEN-AMB-0001"))
        self.refused(lambda c: c["tier1"]["diagnostics"].update(sample_cases=["BEN-AMB-0001"]))
        self.refused(lambda c: c["tier3"]["latency"].update(slowest_case="BEN-AMB-0001"))
        self.refused(lambda c: c["by_family"]["B1"].update(case_ids=["BEN-AMB-0001"]))

    def test_a_tier_two_rate_without_its_exact_bound_is_refused(self) -> None:
        self.refused(lambda c: c["tier2"]["unsafe"].pop("exact_upper95"))

    def test_an_injected_member_of_pass_k_is_refused(self) -> None:
        def inject(c: dict[str, Any]) -> None:
            c["tier1"]["diagnostics"]["pass_k"] = {"k_required": 5, "runs_supplied": 5, "available": True, "reason": None, "rate": make_rate(3, 4), "failing_cases": ["BEN-AMB-0001"],
                                                   "slowest": "BEN-AMB-0002"}
        self.refused(inject)

    def test_a_view_that_would_not_validate_is_never_returned(self) -> None:
        """The output check is not redundant with the input check: it is what stops a future change of the projection from leaking."""
        import benchmark.scoring.view as module
        original = module._project

        def leaking(card: Any) -> dict[str, Any]:
            leaked = original(card)
            leaked["failures"] = [{"case_id": "BEN-AMB-0001"}]
            return leaked
        module._project = leaking
        try:
            with self.assertRaises(ValueError):
                scorecard_view(self.card)
        finally:
            module._project = original


class Schema(unittest.TestCase):
    def setUp(self) -> None:
        self.schema = json.loads((__import__("pathlib").Path(__file__).resolve().parents[2] / "contracts" / "schemas" / "scorecard-view.v1.json").read_text(encoding="utf-8"))

    def walk(self, node: Any, path: str = "#") -> Iterator[tuple[str, dict[str, Any]]]:
        if isinstance(node, dict):
            yield path, node
            for k, v in node.items():
                yield from self.walk(v, f"{path}/{k}")
        elif isinstance(node, list):
            for i, v in enumerate(node):
                yield from self.walk(v, f"{path}/{i}")

    def test_every_object_is_closed(self) -> None:
        for path, node in self.walk(self.schema):
            if node.get("type") == "object" and "properties" in node:
                self.assertIs(node.get("additionalProperties"), False, path)

    def test_no_free_text_can_carry_an_identifier(self) -> None:
        """A string must be an enum, a constant or a pattern that cannot spell a case id; there is no free string member."""
        for path, node in self.walk(self.schema):
            if node.get("type") == "string" or (isinstance(node.get("type"), list) and "string" in node["type"]):
                self.assertTrue(any(k in node for k in ("enum", "const", "pattern")), path)
                # The one deliberate identifier is the run's own id; its pattern is the existing RunId contract. Everything else must not be able to spell a case id.
                if "pattern" in node and path != "#/$defs/RunId":
                    self.assertIsNone(re.fullmatch(node["pattern"], "BEN-AMB-0001"), path)

    def test_every_map_constrains_its_keys_so_a_case_id_cannot_be_one(self) -> None:
        """Codex P2 on noema#642: a map with free keys can carry an identifier as a key. Every object that has `additionalProperties` as a schema (a map) names its allowed keys."""
        maps = 0
        for path, node in self.walk(self.schema):
            if isinstance(node.get("additionalProperties"), dict):
                maps += 1
                names: Any = node.get("propertyNames")
                self.assertIsNotNone(names, path)
                allowed = names.get("enum")
                if allowed is not None:
                    self.assertNotIn("BEN-AMB-0001", allowed, path)
                else:
                    self.assertIsNone(re.fullmatch(names["pattern"], "BEN-AMB-0001"), path)
        self.assertGreaterEqual(maps, 5)  # by_disposition, latency_by_path, layers, by_category, by_family

    def test_the_forbidden_members_are_not_defined_anywhere(self) -> None:
        defined = {k for _, node in self.walk(self.schema) for k in node.get("properties", {})}
        self.assertEqual(set(), defined & FORBIDDEN_KEYS)

    def test_it_refers_to_no_excluded_benchmark_contract(self) -> None:
        text = json.dumps(self.schema)
        self.assertNotIn("benchmark-", text)

    def test_an_injected_identifier_member_is_refused(self) -> None:
        view = scorecard_view(cards()["tripped"])
        mutations: list[Callable[[dict[str, Any]], Any]] = [lambda v: v.update(failures=[{"case_id": "BEN-AMB-0001"}]), lambda v: v["tier0"]["gates"][0].update(case_ids=["BEN-AMB-0001"]),
                       lambda v: v["tier1"]["diagnostics"]["pass_k"].update(failing_cases=["BEN-AMB-0001"]) if v["tier1"]["diagnostics"]["pass_k"].get("available") is not None else None,
                       lambda v: v.update(extra=1), lambda v: v.pop("failure_count"), lambda v: v["incomplete_counts"].update(missing=["BEN-AMB-0001"])]
        for mutate in mutations:
            candidate = copy.deepcopy(view)
            mutate(candidate)
            self.assertTrue(CONTRACTS.errors("scorecard-view.v1", candidate))


if __name__ == "__main__":
    unittest.main()
