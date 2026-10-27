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


if __name__ == "__main__":
    unittest.main()
