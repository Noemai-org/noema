"""SoT 15.1: every minimum property of the three taxonomies is either carried by a real schema field or listed as a gap.

The table is in contracts/schemas/README.md. A `covered` row must name paths that resolve through `properties` (and `items`)
in the named schema; a `gap` row names none. The counts in the sentence above the table must match the rows.
"""
import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
README = (ROOT / "contracts/schemas/README.md").read_text()
SECTION = README[README.index("## Pack schema coverage of SoT 15.1"):]
ROWS = [[c.strip() for c in line.strip("|").split("|")] for line in SECTION.splitlines() if line.startswith("| ") and not line.startswith("| Taxonomy") and not line.startswith("|---")]


def resolve(ref: str) -> bool:
    name, path = ref.split("#/")
    node = json.loads((ROOT / f"contracts/schemas/{name}.v1.json").read_text())
    for part in path.split("/"):
        node = _step(node, part)
        if node is None:
            return False
    return True


def _step(node, part):
    for candidate in [node, *[s for k in ("allOf", "oneOf", "anyOf") for s in node.get(k, [])]]:
        for holder in (candidate, candidate.get("items") or {}):
            if part in (holder.get("properties") or {}):
                return holder["properties"][part]
    return None


class Coverage(unittest.TestCase):
    def test_rows_exist_for_every_minimum_property(self):
        self.assertEqual({r[0] for r in ROWS}, {"Intent", "Trust", "Outcome"})
        self.assertEqual([sum(1 for r in ROWS if r[0] == t) for t in ("Intent", "Trust", "Outcome")], [11, 18, 11])

    def test_covered_rows_resolve_and_gap_rows_name_nothing(self):
        for taxonomy, prop, status, carried, _ in ROWS:
            with self.subTest(f"{taxonomy}: {prop}"):
                refs = re.findall(r"`([a-z-]+#/[^`]+)`", carried)
                if status == "covered":
                    self.assertTrue(refs)
                    for ref in refs:
                        self.assertTrue(resolve(ref), ref)
                else:
                    self.assertEqual((status, refs), ("gap", []))

    def test_counts_in_the_summary_match_the_rows(self):
        counts = {t: sum(1 for r in ROWS if r[0] == t and r[2] == "covered") for t in ("Intent", "Trust", "Outcome")}
        self.assertIn(f"Intent {counts['Intent']} of 11 covered, Trust {counts['Trust']} of 18, Outcome {counts['Outcome']} of 11", SECTION)
        self.assertIn("Five gaps", SECTION)
        self.assertEqual(sum(1 for r in ROWS if r[2] == "gap"), 5)


if __name__ == "__main__":
    unittest.main()
