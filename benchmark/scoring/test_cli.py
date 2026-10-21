"""`python -m benchmark.scoring`: score recorded result files (REQ-BEN-002 acceptance: a manifest reruns identically)."""
import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

from benchmark.contracts import pretty_bytes
from benchmark.scoring.__main__ import main
from benchmark.scoring.test_report import fixture
from benchmark.scoring.testdata import ALLOWED, GATE_MUTATIONS, PROHIBITED

PACK = {"capabilities": [{"capability_id": c, "prohibited": False, "side_effect_class": "CREATES_RECORD"} for c in sorted(ALLOWED)]
        + [{"capability_id": c, "prohibited": True, "side_effect_class": "MUTATES_FINANCIAL_STATE"} for c in sorted(PROHIBITED)]}


def write_tree(root: Path, mutate=None, break_case: bool = False):
    cases, oracles, results, manifest = fixture()
    if mutate:
        mutate(results)
    for sub in ("cases", "oracles", "results"):
        (root / sub).mkdir()
    for c in cases:
        (root / "cases" / f"{c['case_id']}.json").write_bytes(pretty_bytes(c))
    for o in oracles:
        (root / "oracles" / f"{o['case_id']}.json").write_bytes(pretty_bytes(o))
    for r in results:
        (root / "results" / f"{r['case_id']}.json").write_bytes(pretty_bytes(r))
    (root / "pack.json").write_bytes(pretty_bytes(PACK))
    (root / "manifest.json").write_bytes(pretty_bytes(manifest))
    if break_case:
        bad = json.loads((root / "cases" / f"{cases[0]['case_id']}.json").read_text())
        bad["category"] = "MISC"
        (root / "cases" / f"{cases[0]['case_id']}.json").write_bytes(pretty_bytes(bad))
    return cases


def run_cli(root: Path, out: Path, *extra: str) -> tuple[int, str]:
    argv = ["--cases", str(root / "cases"), "--oracles", str(root / "oracles"), "--results", str(root / "results"),
            "--pack", str(root / "pack.json"), "--manifest", str(root / "manifest.json"), "--run-id", "run_cli", "--out", str(out), *extra]
    buffer = io.StringIO()
    with redirect_stdout(buffer):
        code = main(argv)
    return code, buffer.getvalue()


class CliTest(unittest.TestCase):
    def test_a_clean_recorded_run_scores_and_reruns_byte_identically(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            code, out1 = run_cli(root, root / "out1")
            code2, _ = run_cli(root, root / "out2")
            self.assertEqual((code, code2), (0, 0))
            for name in ("scorecard.json", "report.md"):
                self.assertEqual((root / "out1" / name).read_bytes(), (root / "out2" / name).read_bytes())
            card = json.loads((root / "out1" / "scorecard.json").read_text())
            self.assertTrue(card["tier0"]["all_zero"])
            self.assertIn("Tier 0", out1)

    def test_a_tier_zero_failure_exits_one_and_the_report_still_shows_everything(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root, lambda results: GATE_MUTATIONS["MONETARY_CODE_PATH"](results[0]))
            code, _ = run_cli(root, root / "out")
            self.assertEqual(code, 1)
            self.assertIn("MONETARY_CODE_PATH", (root / "out" / "report.md").read_text())

    def test_a_case_that_fails_its_schema_is_excluded_listed_and_the_run_is_incomplete(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root, break_case=True)
            code, out = run_cli(root, root / "out")
            card = json.loads((root / "out" / "scorecard.json").read_text())
            self.assertEqual(code, 1)
            self.assertFalse(card["complete"])
            self.assertEqual(len(card["incomplete"]["invalid_case"]), 1)
            self.assertIn("RUN INCOMPLETE", (root / "out" / "report.md").read_text())

    def test_a_result_that_fails_its_schema_is_a_runner_error_for_that_case(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cases = write_tree(root)
            path = root / "results" / f"{cases[0]['case_id']}.json"
            doc = json.loads(path.read_text())
            doc["latency_ms"] = -5
            path.write_bytes(pretty_bytes(doc))
            code, _ = run_cli(root, root / "out")
            card = json.loads((root / "out" / "scorecard.json").read_text())
            self.assertEqual(code, 1)
            self.assertEqual(card["incomplete"]["errored"], [cases[0]["case_id"]])

    def test_an_unreadable_input_exits_two_and_writes_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            (root / "pack.json").write_text("{not json")
            code, out = run_cli(root, root / "out")
            self.assertEqual(code, 2)
            self.assertFalse((root / "out").exists())

    def test_a_path_naming_the_held_out_repository_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            code, out = run_cli(root, root / "out", "--structural-evidence", str(root / "benchmark-heldout" / "x.json"))
            self.assertEqual(code, 2)
            self.assertIn("benchmark-heldout", out)

    def test_results_may_be_one_jsonl_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_tree(root)
            lines = [json.dumps(json.loads(p.read_text()), sort_keys=True) for p in sorted((root / "results").glob("*.json"))]
            for p in (root / "results").glob("*.json"):
                p.unlink()
            (root / "results").rmdir()
            (root / "results").write_text("\n".join(lines) + "\n")
            code, _ = run_cli(root, root / "out")
            self.assertEqual(code, 0)


if __name__ == "__main__":
    unittest.main()
