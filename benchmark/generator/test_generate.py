"""noema-casegen: determinism, quotas, schema validity, oracle and fixture consistency (REQ-BEN-001; PERD §48.1, §48.4)."""
import json
import unittest
from collections import Counter
from functools import lru_cache

from benchmark.contracts import Contracts, loads_strict
from benchmark.generator import catalogue
from benchmark.generator.generate import GENERATOR_NAME, GENERATOR_VERSION, Suite, generate

SEED = 20261020


@lru_cache(maxsize=None)
def suite(seed: int = SEED) -> Suite:
    return generate(seed)


@lru_cache(maxsize=None)
def contracts() -> Contracts:
    return Contracts()


class Determinism(unittest.TestCase):
    def test_same_seed_is_byte_identical(self) -> None:
        """REQ-BEN-001: the same generator version and seed give byte-identical cases."""
        again = generate(SEED)
        self.assertEqual(suite().files, again.files)

    def test_other_seed_differs(self) -> None:
        self.assertNotEqual(suite().files["manifest.json"], suite(SEED + 1).files["manifest.json"])

    def test_every_case_records_generator_and_seed(self) -> None:
        """PERD §48.1: every result records the generator version and seed."""
        for case in suite().cases.values():
            self.assertEqual(case["generator"], {"name": GENERATOR_NAME, "version": GENERATOR_VERSION, "seed": SEED})


class Golden(unittest.TestCase):
    def test_manifest_digest_is_pinned(self) -> None:
        """A change to any case changes this digest. Change it only with a generator version bump and a note on why (REQ-BEN-001)."""
        from benchmark.contracts import sha256_of
        self.assertEqual(GENERATOR_VERSION, "1.3.0")
        self.assertEqual(sha256_of(suite().manifest), "sha256:9b59bfa13856fe73f95df8e1af14c43e7a0ef95ba08c28a4d0b5b084f8f61092")


class Quotas(unittest.TestCase):
    def test_category_counts_are_the_perd_table(self) -> None:
        """PERD §48.1 category table; the release suite has at least 320 cases."""
        counts = Counter(c["category"] for c in suite().cases.values())
        self.assertEqual(dict(counts), dict(catalogue.CATEGORY_QUOTAS))
        self.assertGreaterEqual(len(suite().cases), 320)

    def test_pidgin_or_code_switched_share(self) -> None:
        """PERD §48.1: at least 30% of cases are in Pidgin or code-switched."""
        shared = sum(1 for c in suite().cases.values() if "pcm" in c["locale_mix"] or len(c["locale_mix"]) > 1)
        self.assertGreaterEqual(shared * 100, catalogue.MIN_PIDGIN_OR_CODE_SWITCHED_PERCENT * len(suite().cases))

    def test_case_ids_are_unique_and_split_base_from_mutated(self) -> None:
        """Bases take 0001 to 0999, mutated cases 1001 and up (PERD §48.4)."""
        mutated = {e["case_id"] for e in suite().manifest["cases"] if e["mutation"]}
        self.assertTrue(mutated)
        for case_id in suite().cases:
            number = int(case_id.rsplit("-", 1)[1])
            self.assertEqual(number >= 1001, case_id in mutated, case_id)
        self.assertEqual(len(suite().cases), len({*suite().cases}))

    def test_every_mutation_kind_is_present(self) -> None:
        """PERD §48.4 four kinds, and the four attack families (DEC-2026-10-20 T4 item 3)."""
        kinds = {e["mutation"]["kind"] for e in suite().manifest["cases"] if e["mutation"]}
        self.assertEqual(kinds, {"AMBIGUITY", "STATE", "TEMPORAL", "RECOVERY", "FAMILY"})
        names = {e["mutation"]["name"] for e in suite().manifest["cases"] if e["mutation"]}
        self.assertTrue({"NAME_COLLISION", "INJECTION_IN_NAME", "CANDIDATE_FLOODING", "CROSS_PRINCIPAL_PROBE"} <= names)


class Schemas(unittest.TestCase):
    def test_every_output_validates(self) -> None:
        c = contracts()
        for case_id, case in suite().cases.items():
            self.assertEqual(c.errors("benchmark-case", case), [], case_id)
        for case_id, oracle in suite().oracles.items():
            self.assertEqual(c.errors("benchmark-oracle", oracle), [], case_id)
        for name, fixture in suite().fixtures.items():
            self.assertEqual(c.errors("benchmark-fixture", fixture), [], name)
        self.assertEqual(c.errors("benchmark-suite-manifest", suite().manifest), [])

    def test_files_are_strict_json_and_match_the_documents(self) -> None:
        for path, data in suite().files.items():
            if path.endswith(".json"):
                loads_strict(data.decode("utf-8"))

    def test_manifest_hashes_match_case_files(self) -> None:
        import hashlib
        for entry in suite().manifest["cases"]:
            raw = suite().files[f"cases/{entry['case_id']}.json"]
            canonical = json.dumps(json.loads(raw), sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
            self.assertEqual(entry["sha256"], "sha256:" + hashlib.sha256(canonical).hexdigest(), entry["case_id"])


class Consistency(unittest.TestCase):
    def test_oracle_and_fixture_are_the_cases_own(self) -> None:
        for case_id, case in suite().cases.items():
            oracle = suite().oracles[case_id]
            self.assertEqual(oracle["case_id"], case_id)
            self.assertEqual(oracle["case_version"], case["version"])
            self.assertEqual(case["oracle_ref"], f"oracle/{case_id.split('-')[1].lower()}/{case_id.lower().replace('-', '_')}_v1.json")
            self.assertIn(case["oracle_ref"], suite().files)
            fixture = suite().files[case["known_facts_fixture"]]
            refs = {t["ref"] for t in json.loads(fixture)["transactions"]} | {c["ref"] for c in json.loads(fixture)["cases"]}
            principal_ids = {p["principal_id"] for p in json.loads(fixture)["principals"]}
            self.assertIn(case["principal"]["principal_id"], principal_ids)
            if oracle["correct_target"]:
                self.assertIn(oracle["correct_target"], refs, case_id)
            for target in [w["target"] for w in case["expected"]["writes_expected"]]:
                self.assertIn(target, refs, case_id)
            for ref in oracle["unowned_refs"]:
                self.assertIn(ref, refs, case_id)

    def test_unowned_refs_really_belong_to_someone_else(self) -> None:
        for case_id, case in suite().cases.items():
            fixture = json.loads(suite().files[case["known_facts_fixture"]])
            owners = {t["ref"]: t["owner"] for t in fixture["transactions"]}
            for ref in suite().oracles[case_id]["unowned_refs"]:
                if ref in owners:
                    self.assertNotEqual(owners[ref], case["principal"]["principal_id"], case_id)

    def test_oracle_is_not_in_the_case_file(self) -> None:
        """INV-22: the case holds expectations, never ground truth."""
        for case in suite().cases.values():
            self.assertNotIn("correct_target", json.dumps(case))
            self.assertNotIn("unowned_refs", json.dumps(case))

    def test_fault_plan_ids_resolve(self) -> None:
        for case_id, case in suite().cases.items():
            plan = case["fault_plan"]
            if plan == "fp_none":
                self.assertNotIn(f"fault_plans/{plan}.json", suite().files)
            else:
                self.assertIn(f"fault_plans/{plan}.json", suite().files, case_id)

    def test_nothing_mentions_the_heldout_set(self) -> None:
        """INV-22, REQ-AGT-004."""
        for path, data in suite().files.items():
            self.assertNotIn(b"benchmark-heldout", data, path)
            self.assertFalse(path.startswith("heldout"), path)


if __name__ == "__main__":
    unittest.main()
