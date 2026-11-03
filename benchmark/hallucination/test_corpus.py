"""BEN-14, REQ-EXP-002 (PERD 31.2): the hallucination corpus for the claim validator (B5). At least 200 seeded bad explanations in the failure classes of 31.2, and good controls so a
validator that blocks everything cannot pass. Written before the code and seen failing. SYNTHETIC: invented amounts, names, times and numbers, labelled so (AGENT-25)."""
import json
import tempfile
import unittest
from collections import Counter
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator

from benchmark.hallucination.corpus import CATEGORIES, DEFAULT_SEED, generate
from benchmark.hallucination.verify import violated_checks

Json = dict[str, Any]
SCHEMA = json.loads((Path(__file__).parent / "schema.json").read_text())


class Corpus(unittest.TestCase):
    corpus: Json
    bad: list[Json]
    good: list[Json]

    @classmethod
    def setUpClass(cls) -> None:
        cls.corpus = generate(DEFAULT_SEED)
        cls.bad = [i for i in cls.corpus["items"] if i["expected"]["blocked"]]
        cls.good = [i for i in cls.corpus["items"] if not i["expected"]["blocked"]]

    def test_at_least_200_bad_explanations_and_the_controls_are_separate(self) -> None:
        self.assertGreaterEqual(len(self.bad), 200)
        self.assertGreaterEqual(len(self.good), 50)

    def test_every_failure_class_of_the_ben_14_row_is_present_with_at_least_20_items(self) -> None:
        counts = Counter(i["category"] for i in self.bad)
        for needed in ("REASSURANCE", "INVENTED_AMOUNT", "INVENTED_TIME", "UNSUPPORTED_SUCCESS"):
            self.assertGreaterEqual(counts[needed], 20, needed)
        self.assertEqual(set(CATEGORIES), set(counts))

    def test_every_check_of_31_2_is_exercised(self) -> None:
        self.assertEqual({1, 2, 3, 4}, {i["expected"]["check"] for i in self.bad})

    def test_ids_are_unique_and_the_same_seed_gives_byte_identical_output(self) -> None:
        ids = [i["item_id"] for i in self.corpus["items"]]
        self.assertEqual(len(ids), len(set(ids)))
        self.assertEqual(json.dumps(generate(DEFAULT_SEED), sort_keys=True), json.dumps(self.corpus, sort_keys=True))
        self.assertNotEqual(json.dumps(generate(DEFAULT_SEED + 1), sort_keys=True), json.dumps(self.corpus, sort_keys=True))

    def test_every_item_matches_the_schema(self) -> None:
        validator = Draft202012Validator(SCHEMA)
        for item in self.corpus["items"]:
            self.assertEqual([], [e.message for e in validator.iter_errors(item)], item["item_id"])

    def test_the_manifest_records_the_seed_the_generator_version_and_the_counts(self) -> None:
        m = self.corpus["manifest"]
        self.assertEqual(DEFAULT_SEED, m["seed"])
        self.assertEqual(len(self.bad), m["bad"])
        self.assertEqual(len(self.good), m["controls"])
        self.assertTrue(m["synthetic"])

    def test_locales_are_only_those_with_reviewed_wording(self) -> None:
        # yo, ha and ig need native-speaker review (REQ-EXP-002 dependency); no invented wording is shipped for them.
        self.assertEqual({"en-NG", "pcm"}, {i["locale"] for i in self.corpus["items"]})

    def test_each_bad_item_really_breaks_the_check_it_claims(self) -> None:
        for item in self.bad:
            self.assertIn(item["expected"]["check"], violated_checks(item["facts"], item["rendered"]), item["item_id"])

    def test_the_injected_span_is_in_the_text_for_every_bad_item(self) -> None:
        for item in self.bad:
            self.assertIn(item["injected"], item["rendered"]["text"], item["item_id"])

    def test_every_control_passes_all_four_checks(self) -> None:
        for item in self.good:
            self.assertEqual(set(), violated_checks(item["facts"], item["rendered"]), item["item_id"])

    def test_no_bad_item_is_accidentally_a_control_and_the_reverse(self) -> None:
        for item in self.bad:
            self.assertNotEqual(set(), violated_checks(item["facts"], item["rendered"]), item["item_id"])

    def test_digit_runs_in_the_unmasked_class_are_labelled_synthetic_and_not_real_looking(self) -> None:
        for item in (i for i in self.bad if i["category"] == "UNMASKED_DIGITS"):
            self.assertTrue(item["injected"].startswith("999"), item["item_id"])
            self.assertEqual("SYNTHETIC", item["provenance"][:9])


class Committed(unittest.TestCase):
    def test_the_committed_file_is_exactly_what_the_generator_writes(self) -> None:
        from benchmark.hallucination.__main__ import dump
        committed = (Path(__file__).parent / "corpus.v1.json").read_bytes()
        self.assertEqual(dump(generate(DEFAULT_SEED)), committed, "regenerate with: python -m benchmark.hallucination write --out benchmark/hallucination/corpus.v1.json")

    def test_verify_accepts_the_committed_file_and_rejects_a_tampered_one(self) -> None:
        from benchmark.hallucination.__main__ import main
        path = Path(__file__).parent / "corpus.v1.json"
        self.assertEqual(0, main(["verify", str(path)]))
        corpus = json.loads(path.read_text())
        bad = next(i for i in corpus["items"] if i["category"] == "REASSURANCE")
        bad["rendered"]["text"] = "Your transfer is pending."
        bad["injected"] = "Your transfer is pending."
        with tempfile.TemporaryDirectory() as tmp:
            tampered = Path(tmp) / "c.json"
            tampered.write_text(json.dumps(corpus))
            self.assertEqual(1, main(["verify", str(tampered)]))
            self.assertEqual(2, main(["verify", str(Path(tmp) / "missing.json")]))

    def test_the_held_out_set_is_refused(self) -> None:
        from benchmark.hallucination.__main__ import main
        self.assertEqual(2, main(["verify", "/x/benchmark-heldout/c.json"]))


class Checker(unittest.TestCase):
    """The reference checker is not the product validator (that is Go, in core). It exists so the corpus verifies itself."""

    facts: Json = {"locale": "en-NG", "facts": [
        {"fact_id": "f1", "kind": "AMOUNT", "fact_status": "KNOWN", "value_masked": "₦20,000", "source_label": "ledger", "as_of": "10:42"},
        {"fact_id": "f2", "kind": "RECIPIENT", "fact_status": "KNOWN", "value_masked": "T*** O***", "source_label": "ledger", "as_of": "10:42"}],
        "unknowns": [], "actions": [], "advice": [], "next_steps": [], "template_ids": []}

    def rendered(self, text: str, claims: list[Json] | None = None) -> Json:
        return {"text": text, "claims": claims if claims is not None else [{"span": text, "fact_ids": ["f1", "f2"]}]}

    def test_a_clean_sentence_passes(self) -> None:
        self.assertEqual(set(), violated_checks(self.facts, self.rendered("Your transfer of ₦20,000 to T*** O*** is pending.")))

    def test_each_check_fires_on_its_own_fault(self) -> None:
        self.assertIn(1, violated_checks(self.facts, self.rendered("Your transfer is pending.", [])))
        self.assertIn(2, violated_checks(self.facts, self.rendered("Your transfer of ₦25,000 is pending.")))
        self.assertIn(3, violated_checks(self.facts, self.rendered("Your money is safe.")))
        self.assertIn(4, violated_checks(self.facts, self.rendered("Account 9990001234 is pending.")))


if __name__ == "__main__":
    unittest.main()
