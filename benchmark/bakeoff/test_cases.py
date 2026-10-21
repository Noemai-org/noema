"""The bake-off case set (REQ-BEN-005, T4 item 9a): deterministic, synthetic, split four ways, and meaning-preserving perturbations."""
import re
import unittest
from collections import Counter, defaultdict
from typing import Any

from hypothesis import given, settings, strategies as st

from benchmark.bakeoff.cases import BUCKETS, TASKS, cases_digest, generate, perturb
from benchmark.contracts import Contracts, canonical_bytes
from benchmark.testing import examples

CONTRACTS = Contracts()
ABSTENTIONS = {"none", "other"}


class GenerateTest(unittest.TestCase):
    cases: list[dict[str, Any]]

    @classmethod
    def setUpClass(cls) -> None:
        cls.cases = generate(seed=11, per_cell=12)

    def test_every_case_is_valid(self) -> None:
        for case in self.cases:
            self.assertEqual(CONTRACTS.errors("benchmark-decision-case", case), [], case["case_id"])

    def test_same_seed_same_bytes_and_a_different_seed_differs(self) -> None:
        self.assertEqual([canonical_bytes(c) for c in generate(11, 12)], [canonical_bytes(c) for c in self.cases])
        self.assertNotEqual(cases_digest(generate(12, 12)), cases_digest(self.cases))

    def test_digest_is_of_the_canonical_bytes_in_id_order(self) -> None:
        self.assertEqual(cases_digest(self.cases), cases_digest(list(reversed(self.cases))))

    def test_every_task_and_language_bucket_has_its_share(self) -> None:
        cells = Counter((c["task"], c["language_bucket"]) for c in self.cases)
        for task in TASKS:
            for bucket in BUCKETS:
                self.assertGreaterEqual(cells[(task, bucket)], 12, (task, bucket))

    def test_ids_are_unique_and_carry_the_task_code(self) -> None:
        ids = [c["case_id"] for c in self.cases]
        self.assertEqual(len(ids), len(set(ids)))
        codes = {"INTENT_NODE": "INT", "ENUM_SLOT": "SLT", "AMBIGUITY_FLAG": "AMB", "LOCALE": "LOC", "ANSWER_BINDING": "BND", "INSTRUCTION_SCREEN": "INJ"}
        for c in self.cases:
            self.assertTrue(c["case_id"].startswith(f"BDC-{codes[c['task']]}-"), c["case_id"])

    def test_every_choice_offers_an_abstention_and_the_truth_is_an_option(self) -> None:
        for c in self.cases:
            options = c["question"]["options"]
            self.assertIn(c["expected"], options, c["case_id"])
            if c["question"]["type"] == "choice":
                self.assertTrue(ABSTENTIONS & set(options), c["case_id"])
            else:
                self.assertEqual(options, ["yes", "no"], c["case_id"])

    def test_the_truth_is_not_one_class(self) -> None:
        by_question: dict[str, Counter[str]] = defaultdict(Counter)
        for c in self.cases:
            by_question[c["question"]["id"]][c["expected"]] += 1
        for question, counts in by_question.items():
            self.assertGreaterEqual(len(counts), 2, question)
            self.assertLess(max(counts.values()) / sum(counts.values()), 0.9, (question, counts))

    def test_abstention_is_sometimes_the_right_answer(self) -> None:
        for task in ("INTENT_NODE", "ANSWER_BINDING"):
            truths = [c["expected"] for c in self.cases if c["task"] == task]
            self.assertTrue(any(t in ABSTENTIONS for t in truths), task)

    def test_voice_transcripts_look_like_transcripts(self) -> None:
        voice = [c for c in self.cases if c["language_bucket"] == "voice-transcript"]
        self.assertTrue(voice)
        for c in voice:
            self.assertEqual(c["input"]["channel"], "voice_transcript")
            self.assertEqual(c["input"]["text"], c["input"]["text"].lower())
            self.assertNotRegex(c["input"]["text"], r"[.?!;:]|(?<!\d),|,(?!\d)")
        for c in self.cases:
            if c["language_bucket"] != "voice-transcript":
                self.assertEqual(c["input"]["channel"], "text")

    def test_no_long_digit_run_anywhere(self) -> None:
        # No account-number-like string in any input, option label or perturbation (REQ-AMB-004 shows at most four digits).
        for c in self.cases:
            texts = [c["input"]["text"], *[p["text"] for p in c["perturbations"]], *[o["label"] for o in c["input"].get("context", {}).get("options", [])]]
            for t in texts:
                self.assertNotRegex(t, r"\d{5,}", (c["case_id"], t))

    def test_binding_options_have_distinct_labels_and_ids(self) -> None:
        for c in self.cases:
            options = c["input"].get("context", {}).get("options")
            if c["task"] == "ANSWER_BINDING":
                self.assertTrue(options)
                self.assertEqual(len({o["label"] for o in options}), len(options))
                self.assertEqual([o["id"] for o in options] + ["none"], c["question"]["options"])

    def test_every_case_has_perturbations_and_they_differ_from_the_input(self) -> None:
        for c in self.cases:
            self.assertGreaterEqual(len(c["perturbations"]), 1, c["case_id"])
            for p in c["perturbations"]:
                self.assertNotEqual(p["text"], c["input"]["text"], (c["case_id"], p["kind"]))

    def test_the_prior_turn_is_present_exactly_for_contradiction_cases(self) -> None:
        for c in self.cases:
            has = "prior_turn" in c["input"].get("context", {})
            self.assertEqual(has, c["question"]["id"] == "contradicts_prior_turn", c["case_id"])

    def test_all_cases_are_labelled_generated_and_seeded(self) -> None:
        for c in self.cases:
            self.assertEqual((c["provenance"], c["generator"]["seed"]), ("generated", 11))


class PerturbationTest(unittest.TestCase):
    TEXT = st.text(alphabet="abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .,?!'ẹọṣẸỌṢ", min_size=1, max_size=80)

    @settings(max_examples=examples(80), deadline=None)
    @given(TEXT)
    def test_case_keeps_every_letter(self, text: str) -> None:
        self.assertEqual(perturb(text, "CASE").lower(), text.lower())

    @settings(max_examples=examples(80), deadline=None)
    @given(TEXT)
    def test_whitespace_changes_only_whitespace(self, text: str) -> None:
        self.assertEqual("".join(perturb(text, "WHITESPACE").split()), "".join(text.split()))

    @settings(max_examples=examples(80), deadline=None)
    @given(TEXT)
    def test_punctuation_changes_only_punctuation(self, text: str) -> None:
        def keep(s: str) -> str:
            return re.sub(r"[^\w\s]", "", s)

        self.assertEqual(keep(perturb(text, "PUNCTUATION")), keep(text))

    @settings(max_examples=examples(80), deadline=None)
    @given(TEXT)
    def test_filler_adds_a_polite_prefix_and_keeps_the_text(self, text: str) -> None:
        out = perturb(text, "FILLER")
        self.assertTrue(out.endswith(text))
        self.assertGreater(len(out), len(text))

    @settings(max_examples=examples(80), deadline=None)
    @given(TEXT)
    def test_asr_style_is_lowercase_without_punctuation(self, text: str) -> None:
        out = perturb(text, "ASR_STYLE")
        self.assertEqual(out, out.lower())
        self.assertNotRegex(out, r"[.?!;:]")
        self.assertNotRegex(out, r"(?<!\d),|,(?!\d)")
        self.assertEqual(re.sub(r"[^\w]", "", out), re.sub(r"[^\w]", "", text.lower()))

    def test_unknown_kind(self) -> None:
        with self.assertRaises(ValueError):
            perturb("x", "TRANSLATE")


if __name__ == "__main__":
    unittest.main()
