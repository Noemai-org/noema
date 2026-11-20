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
FORBIDDEN = {"expected", "oracle_ref", "fault_plan", "families", "goal", "category", "safety_gate_relevance", "permitted_capabilities", "events_timeline", "known_facts_fixture",
             "ledger", "switch", "destination", "settlement", "owner", "enterprise_strings", "display_name"}
BENEFICIARY_KEYS = {"beneficiary_id", "name", "aliases", "account_last4"}
TRANSACTION_KEYS = {"ref", "direction", "amount_minor", "currency", "age_minutes", "beneficiary"}


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
            self.assertEqual({"case_id", "channel", "locale_mix", "canonical_locale", "principal", "candidates", "utterances", "case_sha256"}, set(c), c["case_id"])
            self.assertEqual({"principal_id", "roles"}, set(c["principal"]))
            self.assertEqual({"beneficiaries", "transactions"}, set(c["candidates"]))
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

    def test_the_candidate_world_rows_are_the_principals_own_with_only_request_visible_fields(self) -> None:
        """A439: candidates are part of the request, so the candidate world rows are exported, inputs only: an allowlist of fields, and none of the world's state (ledger, switch, destination,
        settlement), which is what a case is built to find out. Only rows the case's own principal owns."""
        for c in self.doc["cases"]:
            src = self.suite.cases[c["case_id"]]
            fx = self.suite.fixtures[src["known_facts_fixture"]]
            pid = src["principal"]["principal_id"]
            self.assertEqual({"principal_id": pid, "roles": src["principal"]["roles"]}, c["principal"])
            want_b = [{k: b[k] for k in BENEFICIARY_KEYS} for b in fx["beneficiaries"] if b["owner"] == pid]
            want_t = [{k: x[k] for k in TRANSACTION_KEYS} for x in fx["transactions"] if x["owner"] == pid]
            self.assertEqual(want_b, c["candidates"]["beneficiaries"], c["case_id"])
            self.assertEqual(want_t, c["candidates"]["transactions"], c["case_id"])
            self.assertTrue(c["candidates"]["beneficiaries"] and c["candidates"]["transactions"], c["case_id"])
            for b in c["candidates"]["beneficiaries"]:
                self.assertEqual(BENEFICIARY_KEYS, set(b))
            for x in c["candidates"]["transactions"]:
                self.assertEqual(TRANSACTION_KEYS, set(x))
                self.assertIn(x["beneficiary"], {b["beneficiary_id"] for b in c["candidates"]["beneficiaries"]})

    def test_rows_of_another_principal_are_never_exported(self) -> None:
        suite = generate(SEED)
        src = suite.cases["BEN-TMO-0001"]
        fx = suite.fixtures[src["known_facts_fixture"]]
        fx["beneficiaries"].append({"beneficiary_id": "ben_9999", "owner": "usr_other", "name": "Someone Else", "aliases": [], "account_last4": "0000"})
        fx["transactions"].append({"ref": "fin.transaction:TX9999", "owner": "usr_other", "beneficiary": "ben_9999", "direction": "OUTGOING", "amount_minor": 1, "currency": "NGN", "age_minutes": 1,
                                   "ledger": "POSTED", "switch": "OK", "destination": "OK", "settlement": "OK"})
        out = json.dumps(build(suite)["cases"])
        self.assertNotIn("ben_9999", out)
        self.assertNotIn("TX9999", out)

    def test_the_canonical_locale_is_en_ng_for_every_case_and_the_mix_stays_a_tag_only(self) -> None:
        """A460 (rulings of 20 Nov): the locale is en-NG for all five cases (what the UI sends). `canonical_locale` is "en-NG", `locale_mix` stays a tag only (not sent), and no locale hint is
        exported."""
        for c in self.doc["cases"]:
            self.assertEqual("en-NG", c["canonical_locale"], c["case_id"])
            src = self.suite.cases[c["case_id"]]
            self.assertEqual(src["locale_mix"], c["locale_mix"], c["case_id"])  # the tag is the generated case's own, unchanged
        self.assertIn("en-NG", self.doc["canonical_locale_rule"])
        self.assertIn("A460", self.doc["canonical_locale_rule"])
        self.assertNotIn("locale_hint", json.dumps(self.doc))
        self.assertTrue(any(len(c["locale_mix"]) > 1 or c["locale_mix"] != ["en-NG"] for c in self.doc["cases"]))  # the mixes are real: the constant is a ruling, not a derivation

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
