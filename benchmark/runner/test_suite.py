"""Loading a generated suite for a run (REQ-BEN-002): strict, verified against its manifest, and never touching the oracle documents."""
import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from benchmark.generator.generate import generate, write
from benchmark.runner.suite import SuiteError, load_suite

_cache: dict[str, Path] = {}


def suite_dir() -> Path:
    if "dir" not in _cache:
        d = Path(tempfile.mkdtemp(prefix="runner-suite-"))
        write(generate(20261020), d)
        _cache["dir"] = d
    return _cache["dir"]


def copy_suite(keep_oracle: bool = True) -> Path:
    import shutil
    d = Path(tempfile.mkdtemp(prefix="runner-suite-copy-"))
    shutil.copytree(suite_dir(), d, dirs_exist_ok=True)
    if not keep_oracle:
        shutil.rmtree(d / "oracle")
    return d


class Load(unittest.TestCase):
    def test_every_generated_case_loads_with_its_fixture(self) -> None:
        suite = load_suite(suite_dir())
        self.assertEqual(len(suite.cases), 320)
        self.assertEqual(suite.invalid, [])
        for lc in suite.cases:
            self.assertEqual(lc.fixture["fixture_id"], Path(lc.case["known_facts_fixture"]).stem)
            self.assertEqual(lc.plan is None, lc.case["fault_plan"] == "fp_none")

    def test_the_runner_never_reads_the_oracle_documents(self) -> None:
        """The oracle is for the scorer and the oracle client; the runner needs none of it to drive a case (INV-22)."""
        d = copy_suite(keep_oracle=False)
        self.assertEqual(len(load_suite(d).cases), 320)
        seen: list[str] = []
        original = Path.read_text

        def spy(self: Path, *a: Any, **k: Any) -> str:
            seen.append(str(self))
            return original(self, *a, **k)

        Path.read_text = spy  # type: ignore[method-assign]
        try:
            load_suite(suite_dir())
        finally:
            Path.read_text = original  # type: ignore[method-assign]
        self.assertFalse([p for p in seen if "/oracle/" in p], "the runner opened an oracle document")

    def test_a_case_file_that_changed_since_generation_is_refused(self) -> None:
        d = copy_suite()
        path = d / "cases" / "BEN-CLEAN-0001.json"
        doc = json.loads(path.read_text())
        doc["goal"] = "tampered"
        path.write_text(json.dumps(doc))
        with self.assertRaises(SuiteError) as raised:
            load_suite(d)
        self.assertIn("BEN-CLEAN-0001", str(raised.exception))

    def test_a_case_that_fails_its_schema_is_listed_not_run(self) -> None:
        """The scorer excludes such a case and marks the run INCOMPLETE (REQ-BEN-001); the runner must not run it."""
        d = copy_suite()
        path = d / "cases" / "BEN-CLEAN-0002.json"
        doc = json.loads(path.read_text())
        del doc["expected"]
        path.write_text(json.dumps(doc))
        manifest_path = d / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        from benchmark.contracts import sha256_of
        for entry in manifest["cases"]:
            if entry["case_id"] == "BEN-CLEAN-0002":
                entry["sha256"] = sha256_of(doc)
        manifest_path.write_text(json.dumps(manifest))
        suite = load_suite(d)
        self.assertEqual([c for c, _ in suite.invalid], ["BEN-CLEAN-0002"])
        self.assertNotIn("BEN-CLEAN-0002", [lc.case["case_id"] for lc in suite.cases])

    def test_a_missing_fault_plan_file_is_refused(self) -> None:
        d = copy_suite()
        plan = next((d / "fault_plans").glob("*.json"))
        plan.unlink()
        with self.assertRaises(SuiteError):
            load_suite(d)

    def test_a_root_that_names_the_heldout_set_is_refused(self) -> None:
        with self.assertRaises(SuiteError):
            load_suite(Path("/tmp/benchmark-heldout/suite"))

    def test_only_the_generated_layer_is_run(self) -> None:
        d = copy_suite()
        manifest_path = d / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        manifest["cases"][0]["suite_layer"] = "HELD_OUT"
        manifest_path.write_text(json.dumps(manifest))
        with self.assertRaises(SuiteError):
            load_suite(d)

    def test_a_directory_without_a_manifest_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as d, self.assertRaises(SuiteError):
            load_suite(Path(d))


if __name__ == "__main__":
    unittest.main()
