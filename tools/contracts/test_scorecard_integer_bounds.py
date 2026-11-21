"""A446, A298/A311, REQ-UI-004, AGENT-15: every integer of scorecard-view.v1 is a whole number a browser holds exactly.

The benchmark UI reads this contract with `JSON.parse`, which turns an integer above 2^53-1 into another number before any parser sees it (noema#808). So no integer of the contract
may be unbounded: each has a minimum of 0 and a maximum no larger than 9007199254740991 (the largest integer a JavaScript number holds exactly), the bound the contract's counts carry; a tighter one, as the
1000 runs of a pass-k, stays.
Data is SYNTHETIC."""
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SAFE = 9007199254740991


def integers(node, path=""):
    if isinstance(node, dict):
        kinds = node.get("type")
        kinds = kinds if isinstance(kinds, list) else [kinds]
        if "integer" in kinds:
            yield path, node
        for key, value in node.items():
            yield from integers(value, f"{path}/{key}")
    elif isinstance(node, list):
        for i, value in enumerate(node):
            yield from integers(value, f"{path}[{i}]")


class ScorecardIntegerBounds(unittest.TestCase):
    def test_every_integer_has_a_minimum_of_zero_and_a_maximum_a_browser_holds_exactly(self):
        schema = json.loads((ROOT / "contracts/schemas/scorecard-view.v1.json").read_text(encoding="utf-8"))
        found = list(integers(schema))
        self.assertGreater(len(found), 20, "the scan sees the integers of the contract")
        loose = [path for path, node in found if node.get("minimum") != 0 or not isinstance(node.get("maximum"), int) or node["maximum"] > SAFE]
        self.assertEqual([], loose, "integers without minimum 0 and a maximum of at most 2^53-1")


if __name__ == "__main__":
    unittest.main()
