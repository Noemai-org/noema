"""A604 (UND-16, run 7): the offline labelled set for the `requested_remedy` decision (benchmark/compile_acceptance/remedy_labelled.v1.json).

The set is SYNTHETIC (AGENT-25). It is separate from the five goldens and from the held-out set (INV-22): no text in it equals a golden, a G1 input or any text another file of the benchmark generator
writes, and it names no oracle. The checks here fail a change that makes it too small, unbalanced, duplicated, or overlapping a golden, so the compiler's fix is measured on a set it was not written against."""
import json
import re
import unittest
from pathlib import Path

from benchmark.compile_acceptance.golden import GOLDEN
from benchmark.compile_acceptance.remedy_labelled import ASKS_FOR, LOCALES, Item, LabelledSetRefused, load, parse

ROOT = Path(__file__).resolve().parents[2]
PATH = ROOT / "benchmark" / "compile_acceptance" / "remedy_labelled.v1.json"


class Shape(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.items = load(PATH)

    def test_the_set_has_24_to_30_items_a604(self) -> None:
        self.assertGreaterEqual(len(self.items), 24)
        self.assertLessEqual(len(self.items), 30)

    def test_both_labels_and_both_locales_are_balanced_in_every_cell_a604(self) -> None:
        for locale in LOCALES:
            for ask in (True, False):
                cell = [i for i in self.items if i.locale == locale and i.explicit_ask is ask]
                self.assertGreaterEqual(len(cell), 6, (locale, ask))

    def test_ids_and_texts_are_unique_and_texts_are_plain(self) -> None:
        self.assertEqual(len({i.item_id for i in self.items}), len(self.items))
        self.assertEqual(len({i.text.casefold() for i in self.items}), len(self.items))
        for i in self.items:
            self.assertRegex(i.item_id, r"^RL-[0-9]{3}$")
            self.assertTrue(10 <= len(i.text) <= 160, i.item_id)
            self.assertNotRegex(i.text, r"[0-9]{5,}", "no digit run of five or more (AGENT-13)")

    def test_an_ask_names_what_it_asks_for_and_a_non_ask_does_not(self) -> None:
        for i in self.items:
            if i.explicit_ask:
                self.assertIn(i.asks_for, ASKS_FOR, i.item_id)
            else:
                self.assertIsNone(i.asks_for, i.item_id)

    def test_every_asks_for_kind_is_covered_at_least_once(self) -> None:
        self.assertEqual({i.asks_for for i in self.items if i.explicit_ask}, set(ASKS_FOR))

    def test_borderline_items_are_a_few_and_every_borderline_item_says_why(self) -> None:
        border = [i for i in self.items if i.borderline]
        self.assertTrue(1 <= len(border) <= 6)
        for i in border:
            self.assertTrue(i.why.strip(), i.item_id)

    def test_no_text_equals_or_contains_a_golden_or_a_g1_input(self) -> None:
        g1 = json.loads((ROOT / "benchmark" / "replay" / "g1_inputs.json").read_text(encoding="utf-8"))
        taken = [g.text.casefold() for g in GOLDEN] + [u["text"].casefold() for case in g1["cases"] for u in case["utterances"]]
        for i in self.items:
            low = i.text.casefold()
            for other in taken:
                self.assertNotEqual(low, other, i.item_id)
                self.assertNotIn(other, low, i.item_id)
                self.assertNotIn(low, other, i.item_id)

    def test_no_item_uses_a_golden_counterparty_or_a_g1_counterparty(self) -> None:
        g1 = json.loads((ROOT / "benchmark" / "replay" / "g1_inputs.json").read_text(encoding="utf-8"))
        names = {g.counterparty.casefold() for g in GOLDEN if g.counterparty}
        names |= {a.casefold() for case in g1["cases"] for b in case["candidates"]["beneficiaries"] for a in b["aliases"]}
        for i in self.items:
            words = set(re.findall(r"[A-Za-z]+", i.text.casefold()))
            self.assertFalse(words & names, (i.item_id, words & names))

    def test_the_file_declares_what_it_is_and_what_it_is_not(self) -> None:
        doc = json.loads(PATH.read_text(encoding="utf-8"))
        self.assertEqual(doc["ruling"], "A604")
        self.assertIn("SYNTHETIC", doc["label"])
        self.assertIs(doc["held_out"], False)
        self.assertIs(doc["goldens"], False)
        self.assertTrue(doc["rule"].strip())


class Refusals(unittest.TestCase):
    def doc(self) -> dict:
        return json.loads(PATH.read_text(encoding="utf-8"))

    def test_a_malformed_file_is_refused_not_repaired(self) -> None:
        good = self.doc()
        self.assertIsInstance(parse(good)[0], Item)
        cases = {
            "not a mapping": [],
            "no items": {**good, "items": []},
            "an unknown locale": {**good, "items": [{**good["items"][0], "locale": "fr-FR"}] + good["items"][1:]},
            "a string label": {**good, "items": [{**good["items"][0], "explicit_ask": "yes"}] + good["items"][1:]},
            "an ask with no kind": {**good, "items": [{**good["items"][0], "explicit_ask": True, "asks_for": None}] + good["items"][1:]},
            "a non-ask with a kind": {**good, "items": [{**good["items"][0], "explicit_ask": False, "asks_for": "resend"}] + good["items"][1:]},
            "an extra member": {**good, "items": [{**good["items"][0], "oracle": "x"}] + good["items"][1:]},
            "held-out claimed": {**good, "held_out": True},
        }
        for name, doc in cases.items():
            with self.assertRaises(LabelledSetRefused, msg=name):
                parse(doc)


if __name__ == "__main__":
    unittest.main()
