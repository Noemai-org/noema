"""BEN-05 (A-order of 19 Nov): `benchmark/replay/g1_inputs.json`, the utterances and locales of five G1 cases from the pinned generated suite, inputs only. Written before `g1_inputs.py` and seen failing.

The file is for C2's next compile run (recorded outputs for the replay provider). It carries no expectation and no oracle reference (INV-22): case id, channel, locale mix and the utterances. It is
digest-pinned (the generator, the seed, the suite manifest digest, a digest of each case file and of the exported inputs) and reproducible: the committed file must equal a fresh export. SYNTHETIC."""
import hashlib
import json
import unittest
from pathlib import Path
from typing import Any

from benchmark.generator.generate import generate
from benchmark.replay.g1_inputs import CASES, PATH, SEED, build, canonical, main

Json = dict[str, Any]
FORBIDDEN = {"expected", "oracle_ref", "fault_plan", "families", "goal", "category", "safety_gate_relevance", "permitted_capabilities", "events_timeline", "known_facts_fixture", "principal"}


class Export(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.suite = generate(SEED)
        cls.doc = build(cls.suite)

    suite: Any
    doc: Json

    def test_it_exports_exactly_the_five_named_cases_in_the_given_order(self) -> None:
        self.assertEqual(["BEN-CLEAN-0001", "BEN-CLEAN-0014", "BEN-CLEAN-0027", "BEN-TMO-0001", "BEN-TMO-0018"], list(CASES))
        self.assertEqual(list(CASES), [c["case_id"] for c in self.doc["cases"]])

    def test_each_case_carries_only_inputs_never_an_expectation_or_an_oracle_reference(self) -> None:
        for c in self.doc["cases"]:
            self.assertEqual({"case_id", "channel", "locale_mix", "utterances", "case_sha256"}, set(c), c["case_id"])
            for u in c["utterances"]:
                self.assertEqual({"turn", "text"}, set(u))
        text = json.dumps(self.doc["cases"])  # the label itself says "no oracle reference"; the cases must not
        for word in FORBIDDEN | {"oracle", "expectation", "SUCCESS_CONFIRMED", "forbidden_claims"}:
            self.assertNotIn(word, text, word)

    def test_the_utterances_and_locales_are_the_generated_cases_own(self) -> None:
        for c in self.doc["cases"]:
            src = self.suite.cases[c["case_id"]]
            self.assertEqual(src["utterances"], c["utterances"])
            self.assertEqual(src["locale_mix"], c["locale_mix"])
            self.assertEqual(src["channel"], c["channel"])

    def test_it_is_digest_pinned_to_the_suite_the_generator_and_the_inputs(self) -> None:
        self.assertEqual(20261020, self.doc["suite"]["seed"])
        self.assertEqual(self.suite.manifest["generator"]["version"], self.doc["suite"]["generator_version"])
        self.assertEqual("noema-casegen", self.doc["suite"]["generator"])
        manifest = {e["case_id"]: e for e in self.suite.manifest["cases"]}
        for c in self.doc["cases"]:
            self.assertEqual(manifest[c["case_id"]]["sha256"], c["case_sha256"])
        body = canonical({k: v for k, v in self.doc.items() if k != "inputs_sha256"})
        self.assertEqual("sha256:" + hashlib.sha256(body).hexdigest(), self.doc["inputs_sha256"])
        self.assertEqual("sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191", self.doc["suite"]["manifest_sha256"])  # the value pinned in test_generate.py
        self.assertTrue(self.doc["suite"]["tree_sha256"].startswith("sha256:"))

    def test_it_is_labelled_synthetic_and_inputs_only(self) -> None:
        self.assertIn("SYNTHETIC", self.doc["label"])
        self.assertIn("inputs only", self.doc["label"])

    def test_the_export_is_reproducible_and_does_not_depend_on_dictionary_order(self) -> None:
        again = build(generate(SEED))
        self.assertEqual(canonical(self.doc), canonical(again))

    def test_the_committed_file_equals_a_fresh_export(self) -> None:
        self.assertEqual(canonical(self.doc) + b"\n", Path(PATH).read_bytes())

    def test_check_passes_on_the_committed_file_and_fails_on_a_changed_one(self) -> None:
        self.assertEqual(0, main(["--check"]))
        original = Path(PATH).read_bytes()
        try:
            Path(PATH).write_bytes(original.replace(b"BEN-CLEAN-0001", b"BEN-CLEAN-9999", 1))
            self.assertEqual(1, main(["--check"]))
        finally:
            Path(PATH).write_bytes(original)

    def test_an_unknown_case_is_an_error_never_a_quiet_omission(self) -> None:
        suite = generate(SEED)
        del suite.cases["BEN-TMO-0018"]
        with self.assertRaises(KeyError):
            build(suite)


if __name__ == "__main__":
    unittest.main()
