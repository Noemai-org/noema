"""Run provenance (UND-13, REQ-BEN-009; Codex P1 on noema#199): scoring says which results it read, so a qualification can bind repeat scorecards to the runs behind pass^k."""
import json
import tempfile
import unittest
from pathlib import Path

from benchmark.contracts import Contracts, sha256_of
from benchmark.scoring.test_cli import run_cli, write_repeat, write_tree

CONTRACTS = Contracts()


def scored(root: Path, name: str, *extra: str) -> dict[str, object]:
    out = root / name
    run_cli(root, out, *extra)
    return {"card": json.loads((out / "scorecard.json").read_text()), "prov": json.loads((out / "provenance.json").read_text())}


class Provenance(unittest.TestCase):
    def test_a_single_run_writes_a_valid_provenance_bound_to_its_scorecard(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            got = scored(root, "out")
            prov, card = got["prov"], got["card"]
            self.assertEqual([], CONTRACTS.errors("benchmark-run-provenance", prov))
            self.assertEqual(sha256_of(card), prov["scorecard_sha256"])  # type: ignore[index]
            self.assertEqual("run_cli", prov["run_id"])  # type: ignore[index]
            self.assertIsNone(prov["pass_k_runs"])  # type: ignore[index]

    def test_five_runs_list_every_contributing_run_with_its_own_results_digest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            extra: list[str] = []
            for i in range(2, 6):
                extra += ["--repeat-results", str(write_repeat(root, f"r{i}", f"run_{i}"))]
            prov = scored(root, "out", *extra)["prov"]
            self.assertEqual([], CONTRACTS.errors("benchmark-run-provenance", prov))
            runs = prov["pass_k_runs"]  # type: ignore[index]
            self.assertEqual(["run_cli", "run_2", "run_3", "run_4", "run_5"], [r["run_id"] for r in runs])
            self.assertEqual(5, len({r["results_sha256"] for r in runs}), "each run's result set has its own digest")

    def test_the_digest_follows_the_results_not_the_run_id_alone(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp) / "a", Path(tmp) / "b"
            a.mkdir()
            b.mkdir()
            write_tree(a)
            write_tree(b, mutate=lambda results: results[0].update(latency_ms=(results[0]["latency_ms"] or 0) + 7))
            first, second = scored(a, "out")["prov"], scored(b, "out")["prov"]
            self.assertNotEqual(first["results_sha256"], second["results_sha256"])  # type: ignore[index]
