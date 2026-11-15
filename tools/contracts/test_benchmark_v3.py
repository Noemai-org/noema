"""benchmark-scorecard.v3 (ruling A190, interim, Founder-approved; AGENT-15): v2 plus exactly one required member on the rate block, `exact_upper95`, the exact one-sided 95% upper bound on a failure
rate. The v1 and v2 files are not edited (their bytes are pinned here and in test_benchmark_v2.py); a document is v2 or v3, never both. No threshold is added (AGENT-16)."""
import copy
import hashlib
import json
import unittest
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = {p.name: json.loads(p.read_text(encoding="utf-8")) for p in (ROOT / "contracts" / "schemas").glob("benchmark-scorecard.v*.json")}
V2_SHA256 = "d60eb012ae6477ee03656eeb3351ccd664d9ecbca03893c282402d6cfebd61c1"


def errors(name: str, doc: object) -> list[str]:
    from benchmark.contracts import Contracts
    return Contracts().errors(name, doc)  # type: ignore[no-any-return]


def fixture(kind: str, folder: str, name: str) -> Any:
    return json.loads((ROOT / "contracts" / "fixtures" / kind / folder / name).read_text(encoding="utf-8"))


def rate_blocks(node: object) -> list[dict[str, Any]]:
    found: list[dict[str, Any]] = []
    if isinstance(node, dict):
        if {"k", "n", "rate", "wilson95", "rule_of_three_upper"} <= set(node):
            found.append(node)
        for v in node.values():
            found += rate_blocks(v)
    elif isinstance(node, list):
        for v in node:
            found += rate_blocks(v)
    return found


class ScorecardV3(unittest.TestCase):
    def test_the_v2_file_is_not_edited(self) -> None:
        self.assertEqual(hashlib.sha256((ROOT / "contracts" / "schemas" / "benchmark-scorecard.v2.json").read_bytes()).hexdigest(), V2_SHA256)

    def test_v3_is_v2_plus_exactly_the_rate_member(self) -> None:
        v2, v3 = SCHEMAS["benchmark-scorecard.v2.json"], SCHEMAS["benchmark-scorecard.v3.json"]
        r2, r3 = v2["$defs"]["Rate"], v3["$defs"]["Rate"]
        self.assertEqual(set(r3["properties"]) - set(r2["properties"]), {"exact_upper95"})
        self.assertEqual(set(r3["required"]) - set(r2["required"]), {"exact_upper95"})
        self.assertEqual(r3["additionalProperties"], False)
        self.assertEqual(v3["properties"], v2["properties"])
        self.assertEqual({k: v for k, v in v3["$defs"].items() if k != "Rate"}, {k: v for k, v in v2["$defs"].items() if k != "Rate"})
        self.assertEqual(v3["required"], v2["required"])
        self.assertEqual(v3["$id"], "https://contracts.noema.invalid/benchmark-scorecard.v3.json")
        self.assertIn("exact", r3["properties"]["exact_upper95"]["description"])

    def test_no_threshold_is_added(self) -> None:
        text = json.dumps(SCHEMAS["benchmark-scorecard.v3.json"]["$defs"]["Rate"]["properties"]["exact_upper95"]).lower()
        self.assertNotIn("threshold", text)
        self.assertNotIn("must be", text)

    def test_every_v2_valid_fixture_has_a_v3_twin_that_differs_only_by_the_member(self) -> None:
        names = sorted(p.name for p in (ROOT / "contracts" / "fixtures" / "valid" / "benchmark-scorecard.v2").glob("*.json"))
        self.assertTrue(names)
        for name in names:
            v2, v3 = fixture("valid", "benchmark-scorecard.v2", name), fixture("valid", "benchmark-scorecard.v3", name)
            self.assertEqual(errors("benchmark-scorecard.v3", v3), [], name)
            self.assertEqual(errors("benchmark-scorecard.v2", v2), [], name)
            stripped = copy.deepcopy(v3)
            for block in rate_blocks(stripped):
                block.pop("exact_upper95")
            self.assertEqual(stripped, v2, name)
            self.assertTrue(rate_blocks(v3))

    def test_a_v2_document_is_not_v3_and_the_reverse(self) -> None:
        v2, v3 = fixture("valid", "benchmark-scorecard.v2", "two_cases_clean.json"), fixture("valid", "benchmark-scorecard.v3", "two_cases_clean.json")
        self.assertTrue(errors("benchmark-scorecard.v3", v2))
        self.assertTrue(errors("benchmark-scorecard.v2", v3))

    def test_a_missing_or_malformed_member_is_refused(self) -> None:
        good = fixture("valid", "benchmark-scorecard.v3", "two_cases_clean.json")
        for mutate in (lambda b: b.pop("exact_upper95"), lambda b: b.update(exact_upper95="0.01"), lambda b: b.update(exact_upper95=-0.1), lambda b: b.update(exact_upper95=1.5),
                       lambda b: b.update(exact_upper95=True)):
            card = copy.deepcopy(good)
            mutate(rate_blocks(card)[0])
            self.assertTrue(errors("benchmark-scorecard.v3", card))

    def test_the_invalid_fixtures_cover_every_v2_one_and_the_new_member(self) -> None:
        """`tools/contracts/validate.py` checks that each is rejected for its named reason; this checks the set is complete."""
        v2 = {p.name for p in (ROOT / "contracts" / "fixtures" / "invalid" / "benchmark-scorecard.v2").glob("*.json")}
        v3 = {p.name for p in (ROOT / "contracts" / "fixtures" / "invalid" / "benchmark-scorecard.v3").glob("*.json")}
        self.assertEqual(v3 - v2, {"missing_exact_upper95.json", "exact_upper95_above_one.json", "exact_upper95_not_a_number.json", "exact_upper95_negative.json"})
        self.assertEqual(v2 - v3, set())


if __name__ == "__main__":
    unittest.main()
