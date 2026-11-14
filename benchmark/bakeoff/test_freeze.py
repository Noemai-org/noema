"""The model-selection freeze (benchmark/decisions/2026-10-27-model-selection.md): the hashes in the record are the real ones.

A change to the corpus generator or to the scoring code after the freeze fails here, so the rules cannot move silently."""
import json
import re
import unittest
from pathlib import Path

from benchmark.bakeoff import gates
from benchmark.bakeoff.freeze import SCORING_FILES, corpus_hash, scoring_code_hash

ROOT = Path(__file__).resolve().parents[2]
RECORD = ROOT / "benchmark" / "decisions" / "2026-10-27-model-selection.md"
REFREEZE = ROOT / "benchmark" / "decisions" / "2026-11-13-harness-masking-refreeze.md"
CONFIGS = ("harness-gpt-6-luna.json", "harness-gpt-6.1-terra.json")


def frozen() -> dict:  # type: ignore[type-arg]
    block = re.search(r"```json frozen\n(.*?)\n```", RECORD.read_text(encoding="utf-8"), re.S)
    assert block, "the record has no frozen block"
    return json.loads(block.group(1))  # type: ignore[no-any-return]


class Freeze(unittest.TestCase):
    def test_the_corpus_hash_is_the_recorded_one(self) -> None:
        self.assertEqual(corpus_hash(), frozen()["corpus"]["corpus_hash"])

    def test_the_scoring_code_hash_is_the_recorded_one(self) -> None:
        self.assertEqual(scoring_code_hash(ROOT), frozen()["scoring_code"]["scoring_code_hash"])
        self.assertEqual(sorted(SCORING_FILES), sorted(frozen()["scoring_code"]["files"]))

    def test_the_recorded_corpus_size_is_the_generated_one(self) -> None:
        from benchmark.bakeoff.cases import generate
        cases = generate(frozen()["corpus"]["seed"], frozen()["corpus"]["per_cell"])
        self.assertEqual(len(cases), frozen()["corpus"]["cases"])
        self.assertEqual(sum(1 + len(c["perturbations"]) for c in cases), frozen()["corpus"]["calls"])

    def test_the_record_states_the_gate_values_the_code_applies(self) -> None:
        text = RECORD.read_text(encoding="utf-8")
        for needle in ("at least 95% exact match", "100% correct; zero prohibited actions", "at most 1,800 ms", "at most $0.02", "$25 total", "Pilot cap $2"):
            self.assertIn(needle, text)
        self.assertEqual((gates.LATENCY_P95_MAX_MS, gates.COST_PER_CALL_MAX_MICROS, gates.BUDGET_TOTAL_MICROS, gates.BUDGET_PILOT_MICROS),
                         (1800, 20_000, 25_000_000, 2_000_000))

    def test_the_record_states_the_published_rate_cards_and_its_sources(self) -> None:
        text = RECORD.read_text(encoding="utf-8")
        for needle in ("| gpt-6-luna | $0.10 | $0.01 | $0.50 |", "| gpt-6.1-terra | $1.00 | $0.05 | $5.00 |",
                       "https://developers.openai.com/api/docs/models/gpt-6-luna", "https://developers.openai.com/api/docs/models/gpt-6.1-terra"):
            self.assertIn(needle, text)

    def test_the_harness_configurations_are_the_recorded_files(self) -> None:
        import hashlib
        for path, digest in frozen()["harness_configs"].items():
            self.assertEqual(hashlib.sha256((ROOT / path).read_bytes()).hexdigest(), digest, path)

    def test_the_harness_configurations_name_the_frozen_models_rates_effort_and_key_variable(self) -> None:
        expected = {"harness-gpt-6-luna.json": ("gpt-6-luna", 100_000, 500_000), "harness-gpt-6.1-terra.json": ("gpt-6.1-terra", 1_000_000, 5_000_000)}
        self.assertEqual(sorted(Path(p).name for p in frozen()["harness_configs"]), sorted(expected))
        for name, (model, rate_in, rate_out) in expected.items():
            config = json.loads((ROOT / "benchmark" / "decisions" / name).read_text(encoding="utf-8"))
            (provider,) = config["providers"].values()
            self.assertEqual((provider["kind"], provider["model_version"], provider["reasoning_effort"], provider["credential_env"]),
                             ("openai_responses", model, frozen()["reasoning_effort"], "OPENAI_API_KEY"))
            self.assertEqual((provider["rate"]["input_micro_per_mtok"], provider["rate"]["output_micro_per_mtok"], provider["rate"]["currency"],
                              provider["rate"]["as_of"]), (rate_in, rate_out, "USD", "2026-10-27"))
            self.assertEqual(provider["max_output_tokens"], 4000)
            self.assertNotIn("credential_file", provider)
            self.assertNotIn("reported_model_version", provider)  # learned in the run, never frozen

    def test_the_retry_policy_is_one_validation_attempt_and_no_transport_retry(self) -> None:
        self.assertEqual(frozen()["retry_policy"], {"schema_validation_attempts": 1, "transport_retries": 0})

    def test_the_record_sets_no_winner_and_no_production_default(self) -> None:
        text = RECORD.read_text(encoding="utf-8")
        self.assertIn("no production default is set from this benchmark", text)
        self.assertIn("Multi-provider support is not complete and is not marked complete", text)



class HarnessMaskingRefreeze(unittest.TestCase):
    """A162, DEC-2026-11-12-harness-masking-refreeze (docs#158): the two harness configurations are re-frozen without the masking member, and the floor (4) is pinned by the hash of
    the shared masking-limits.v1 files, so a later run has frozen proof of it."""

    def test_neither_harness_configuration_carries_a_masking_member(self) -> None:
        for name in CONFIGS:
            config = json.loads((ROOT / "benchmark" / "decisions" / name).read_text(encoding="utf-8"))
            self.assertNotIn("masking", config, name)
            self.assertNotIn("account_run_min_digits", json.dumps(config), name)

    def test_the_recorded_masking_limits_are_the_files_and_the_value_is_4(self) -> None:
        import hashlib
        block = frozen()["masking_limits"]
        for file_key, hash_key in (("file", "sha256"), ("schema", "schema_sha256")):
            self.assertEqual(hashlib.sha256((ROOT / block[file_key]).read_bytes()).hexdigest(), block[hash_key], block[file_key])
        self.assertEqual(block["max_unmasked_digit_run"], 4)
        self.assertEqual(json.loads((ROOT / block["file"]).read_text(encoding="utf-8"))["max_unmasked_digit_run"], 4)

    def test_the_superseded_hashes_are_the_ones_the_27_october_pilot_and_full_run_used(self) -> None:
        old = frozen()["superseded_harness_configs"]
        self.assertEqual(sorted(Path(p).name for p in old), sorted(CONFIGS))
        self.assertEqual({p: [(e["sha256"], e["masking_floor_during_use"]) for e in entries] for p, entries in old.items()}, {
            "benchmark/decisions/harness-gpt-6-luna.json": [("ee1cde86c9599ac6302374a4286373566f87b04df924bd5f225827323933f9dd", 8)],
            "benchmark/decisions/harness-gpt-6.1-terra.json": [("841f01ba906f4b3663711b8cbd8d52596518a38d9a31f3176eba95d63325cd4f", 8)]})
        for path, entries in old.items():
            for entry in entries:
                self.assertNotEqual(entry["sha256"], frozen()["harness_configs"][path])

    def test_the_re_freeze_record_names_the_hashes_and_says_which_results_used_floor_8(self) -> None:
        text = REFREEZE.read_text(encoding="utf-8")
        for path, digest in frozen()["harness_configs"].items():
            self.assertIn(digest, text, path)
        for entries in frozen()["superseded_harness_configs"].values():
            for entry in entries:
                self.assertIn(entry["sha256"], text)
        block = frozen()["masking_limits"]
        for needle in (block["sha256"], block["schema_sha256"], "40577810935", "pilot", "account_run_min_digits: 8", "DEC-2026-11-12-harness-masking-refreeze", "A162"):
            self.assertIn(needle, text, needle)

    def test_the_27_october_record_points_to_the_re_freeze(self) -> None:
        self.assertIn("2026-11-13-harness-masking-refreeze.md", RECORD.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
