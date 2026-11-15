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


class Context(unittest.TestCase):
    """Codex P1 on noema#199 (3246f93): the same results scored against another pack, case set or oracle set are not the same scoring."""

    def test_the_context_digest_is_the_same_for_the_same_cases_oracles_and_pack(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp) / "a", Path(tmp) / "b"
            a.mkdir()
            b.mkdir()
            write_tree(a)
            write_tree(b)
            self.assertEqual(scored(a, "out")["prov"]["context_sha256"], scored(b, "out")["prov"]["context_sha256"])  # type: ignore[index]

    def test_a_different_pack_registry_changes_the_context_digest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp) / "a", Path(tmp) / "b"
            a.mkdir()
            b.mkdir()
            write_tree(a)
            write_tree(b)
            pack = json.loads((b / "pack.json").read_text())
            pack["capabilities"][0]["prohibited"] = not pack["capabilities"][0]["prohibited"]
            (b / "pack.json").write_text(json.dumps(pack))
            self.assertNotEqual(scored(a, "out")["prov"]["context_sha256"], scored(b, "out")["prov"]["context_sha256"])  # type: ignore[index]

    def test_a_different_oracle_changes_the_context_digest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp) / "a", Path(tmp) / "b"
            a.mkdir()
            b.mkdir()
            write_tree(a)
            write_tree(b)
            first = sorted((b / "oracles").glob("*.json"))[0]
            oracle = json.loads(first.read_text())
            oracle["high_risk_ambiguous"] = not oracle["high_risk_ambiguous"]
            first.write_text(json.dumps(oracle))
            self.assertNotEqual(scored(a, "out")["prov"]["context_sha256"], scored(b, "out")["prov"]["context_sha256"])  # type: ignore[index]

    def test_a_different_case_changes_the_context_digest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp) / "a", Path(tmp) / "b"
            a.mkdir()
            b.mkdir()
            write_tree(a)
            write_tree(b)
            first = sorted((b / "cases").glob("*.json"))[0]
            case = json.loads(first.read_text())
            case["expected"]["escalation_expected"] = not case["expected"]["escalation_expected"]
            first.write_text(json.dumps(case))
            self.assertNotEqual(scored(a, "out")["prov"]["context_sha256"], scored(b, "out")["prov"]["context_sha256"])  # type: ignore[index]


PINNED_FLAGS = ["--pinned-kind", "llm", "--pinned-provider", "openai", "--pinned-model-id", "decision-model-a", "--pinned-version", "2026-11-01"]
PINNED = {"kind": "llm", "provider": "openai", "model_id": "decision-model-a", "version": "2026-11-01"}


class Identity(unittest.TestCase):
    """Codex P1 on noema#199: scorecards and provenance named no model, so model A's five-run evidence plus model B's passing canary could qualify B. Scoring now writes the identity of
    the model whose results it read (declared by the operator, so a contradiction is caught; it is not authentication, see DEBT-17)."""

    def test_the_declared_identity_is_written_into_the_provenance(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            prov = scored(root, "out", *PINNED_FLAGS)["prov"]
            self.assertEqual(PINNED, prov["pinned"])  # type: ignore[index]
            self.assertEqual([], CONTRACTS.errors("benchmark-run-provenance", prov))

    def test_no_declared_identity_is_written_as_null_not_guessed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            prov = scored(root, "out")["prov"]
            self.assertIsNone(prov["pinned"])  # type: ignore[index]
            self.assertEqual([], CONTRACTS.errors("benchmark-run-provenance", prov))

    def test_a_partly_declared_identity_is_an_input_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            code, text = run_cli(root, root / "out", "--pinned-provider", "openai")
            self.assertEqual(2, code)
            self.assertIn("pinned", text)


class SubsetDigest(unittest.TestCase):
    """Codex P2 on noema#199: the provenance carries the digest of the understanding-dependent cases the scorer read, computed by the same function as the qualification subset's
    `cases_sha256`, so the two can be compared."""

    def test_it_is_the_qualification_subsets_own_digest_for_the_same_cases(self) -> None:
        from benchmark.qualification.run import select_subset
        from benchmark.scoring.provenance import understanding_cases_digest
        from benchmark.scoring.test_report import fixture
        cases = fixture()[0]
        self.assertEqual(select_subset(cases, subset_id="s")["cases_sha256"], understanding_cases_digest(cases))

    def test_it_ignores_cases_that_do_not_depend_on_understanding_and_their_order(self) -> None:
        from benchmark.scoring.provenance import understanding_cases_digest
        from benchmark.scoring.test_report import fixture
        cases = fixture()[0]
        self.assertEqual(understanding_cases_digest(cases), understanding_cases_digest(list(reversed(cases))))

    def test_no_understanding_dependent_case_gives_no_digest(self) -> None:
        from benchmark.scoring.provenance import understanding_cases_digest
        self.assertIsNone(understanding_cases_digest([]))

    def test_the_command_writes_it_into_the_provenance(self) -> None:
        from benchmark.scoring.provenance import understanding_cases_digest
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            prov = scored(root, "out")["prov"]
            cases = [json.loads(f.read_text()) for f in sorted((root / "cases").glob("*.json"))]
            self.assertEqual(understanding_cases_digest(cases), prov["pass_k_cases_sha256"])  # type: ignore[index]
            self.assertEqual([], CONTRACTS.errors("benchmark-run-provenance", prov))
