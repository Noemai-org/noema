"""BEN-29, REQ-TST-005 (Codex on fe55018): a retry with a new run ID must not destroy the evidence of the run before it, and a reset must be followed by bringing the environment back up.

Written before `archive.py` and seen failing. SYNTHETIC. Generated layer only (INV-22)."""
import json
import tempfile
import unittest
from pathlib import Path

from benchmark.suites.archive import archive_previous
from benchmark.suites.g1 import reset_and_up

ROOT = Path(__file__).resolve().parents[2]


def put(out: Path, run_id: str | None, **files: str) -> None:
    out.mkdir(parents=True, exist_ok=True)
    if run_id is not None:
        (out / "suite-run.json").write_text(json.dumps({"run_id": run_id, "status": "COMPLETE"}), encoding="utf-8")
    for name, text in files.items():
        (out / name).write_text(text, encoding="utf-8")


class Archive(unittest.TestCase):
    def test_an_absent_or_empty_output_directory_needs_no_archive(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "us"
            self.assertIsNone(archive_previous(out))
            out.mkdir()
            self.assertIsNone(archive_previous(out))
            self.assertTrue(out.exists())

    def test_a_previous_run_is_moved_beside_it_named_by_its_run_id_and_kept_whole(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "us"
            put(out, "us-1", **{"scorecard.json": "old score", "selection.txt": "old"})
            moved = archive_previous(out)
            self.assertEqual(Path(tmp) / "us.us-1", moved)
            assert moved is not None
            self.assertEqual("old score", (moved / "scorecard.json").read_text(encoding="utf-8"))
            self.assertEqual("us-1", json.loads((moved / "suite-run.json").read_text(encoding="utf-8"))["run_id"])
            self.assertFalse(out.exists(), "the output directory is left for the new run: no stale file can sit beside the new record")

    def test_a_second_archive_of_the_same_run_id_never_overwrites_the_first(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "us"
            put(out, "us-1", **{"a.txt": "first"})
            first = archive_previous(out)
            put(out, "us-1", **{"a.txt": "second"})
            second = archive_previous(out)
            assert first is not None and second is not None
            self.assertNotEqual(first, second)
            self.assertEqual("first", (first / "a.txt").read_text(encoding="utf-8"))
            self.assertEqual("second", (second / "a.txt").read_text(encoding="utf-8"))

    def test_a_directory_with_no_record_is_still_archived_never_deleted(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "g1"
            put(out, None, **{"left-behind.txt": "x"})
            moved = archive_previous(out)
            assert moved is not None
            self.assertEqual("x", (moved / "left-behind.txt").read_text(encoding="utf-8"))
            self.assertTrue(moved.name.startswith("g1.unrecorded"), moved.name)

    def test_a_record_that_is_not_json_does_not_stop_the_archive(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "us"
            out.mkdir()
            (out / "suite-run.json").write_text("{not json", encoding="utf-8")
            moved = archive_previous(out)
            assert moved is not None
            self.assertTrue((moved / "suite-run.json").exists())

    def test_a_run_id_that_is_not_a_safe_name_cannot_choose_where_the_archive_goes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "us"
            put(out, "../../escape")
            moved = archive_previous(out)
            assert moved is not None
            self.assertEqual(Path(tmp), moved.parent)


class ResetThenUp(unittest.TestCase):
    def test_a_reset_is_followed_by_bringing_the_bench_environment_up(self) -> None:
        calls: list[list[str]] = []

        def run(cmd: list[str]) -> int:
            calls.append(cmd)
            return 0

        self.assertEqual(0, reset_and_up(run))
        self.assertEqual([["make", "reset"], ["make", "model-bench-up"]], calls)

    def test_a_failed_reset_stops_before_anything_is_started(self) -> None:
        calls: list[list[str]] = []

        def run(cmd: list[str]) -> int:
            calls.append(cmd)
            return 7

        self.assertEqual(7, reset_and_up(run))
        self.assertEqual([["make", "reset"]], calls)

    def test_a_failed_start_is_reported_and_is_not_a_pass(self) -> None:
        codes = iter([0, 5])
        self.assertEqual(5, reset_and_up(lambda cmd: next(codes)))


class Makefile(unittest.TestCase):
    """The recipes themselves: a reset is followed by the environment, and no two runs share one output directory."""

    def recipe(self, target: str) -> str:
        text = (ROOT / "Makefile").read_text(encoding="utf-8")
        start = text.index(f"\n{target}:")
        end = text.find("\n\n", start + 1)
        return text[start:end if end != -1 else None]

    def test_every_suite_recipe_brings_the_environment_up_after_a_reset(self) -> None:
        recipe = self.recipe("bench-us bench-chaos bench-evil bench-full")
        self.assertIn("$(MAKE) reset", recipe)
        self.assertLess(recipe.index("$(MAKE) reset"), recipe.index("$(MAKE) $(BENCH_UP)"))

    def test_the_g1_suite_is_generated_outside_the_directory_the_run_archives(self) -> None:
        recipe = self.recipe("bench-g1")
        self.assertNotIn("$(BENCH_OUT)/g1/suite", recipe)
        self.assertIn("$(BENCH_OUT)/g1-suite", recipe)


if __name__ == "__main__":
    unittest.main()
