"""BEN-04 (G0-N N10: "the fixture run log twice with identical output"), noema#590 reads by Codex, the Security Reviewer, C1 and C2. Written before `reproduce.py` and seen failing.

`python -m benchmark.generator reproduce` generates the suite twice, in two SEPARATE processes, into two empty directories, compares every file by sha256 and prints both generator logs, the
manifest digests and a tree digest. It fails on any difference or any generator failure. The summary it can write holds counts, digests and the logs only: never a generated file, because the
oracle documents are generated beside the cases (INV-22). Synthetic data only (AGENT-25)."""
import hashlib
import io
import re
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

from benchmark.generator.__main__ import main
from benchmark.generator.reproduce import Result, Runner, reproduce, render, run_generator, tree_digest, tree_files

ROOT = Path(__file__).resolve().parents[2]
GOLDEN = "sha256:c6bdd3a63e4da29487100251bef33ad1dba36561a17ecaa170dc211bc57f0191"  # pinned in test_generate.py


def fake(files: dict[str, bytes], code: int = 0, manifest: str = "sha256:" + "a" * 64) -> Runner:
    """A generator stand-in that writes `files` into the directory it is given."""
    def one(seed: int, out: Path) -> tuple[int, str]:
        for rel, data in files.items():
            target = out / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        return code, f"noema-casegen 0 seed {seed}: {len(files)} cases written to X (manifest {manifest})\n"
    return one


class Tree(unittest.TestCase):
    def test_the_tree_digest_is_the_documented_construction(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a").mkdir()
            (root / "a" / "x.json").write_bytes(b"1")
            (root / "b.json").write_bytes(b"2")
            files = tree_files(root)
            self.assertEqual(files, {"a/x.json": hashlib.sha256(b"1").hexdigest(), "b.json": hashlib.sha256(b"2").hexdigest()})
            lines = "\n".join(f"{k} {v}" for k, v in sorted(files.items()))
            self.assertEqual(tree_digest(files), hashlib.sha256(lines.encode()).hexdigest())

    def test_a_different_byte_or_name_or_extra_file_changes_the_digest(self) -> None:
        base = {"a": "1" * 64, "b": "2" * 64}
        self.assertNotEqual(tree_digest(base), tree_digest({**base, "a": "3" * 64}))
        self.assertNotEqual(tree_digest(base), tree_digest({"a": "1" * 64, "c": "2" * 64}))
        self.assertNotEqual(tree_digest(base), tree_digest({**base, "c": "4" * 64}))


class Reproduce(unittest.TestCase):
    def run_with(self, one: Runner, second: Runner | None = None) -> Result:
        calls: list[Path] = []

        def runner(seed: int, out: Path) -> tuple[int, str]:
            calls.append(out)
            chosen = second if second is not None and len(calls) == 2 else one
            return chosen(seed, out)
        with tempfile.TemporaryDirectory() as tmp:
            result = reproduce(7, Path(tmp) / "r", runner)
        self.assertEqual(2, len(calls))
        self.assertNotEqual(calls[0], calls[1])
        return result

    def test_two_identical_runs_pass(self) -> None:
        result = self.run_with(fake({"cases/c1.json": b"1", "manifest.json": b"m"}))
        self.assertTrue(result.ok, result.problems)
        self.assertEqual(2, result.n_files)
        self.assertEqual(result.tree_digests[0], result.tree_digests[1])
        self.assertEqual(2, len(result.logs))

    def test_one_differing_byte_fails_and_names_the_file(self) -> None:
        result = self.run_with(fake({"cases/c1.json": b"1"}), fake({"cases/c1.json": b"2"}))
        self.assertFalse(result.ok)
        self.assertTrue(any("cases/c1.json" in p for p in result.problems), result.problems)

    def test_a_file_in_only_one_run_fails(self) -> None:
        result = self.run_with(fake({"a": b"1"}), fake({"a": b"1", "b": b"1"}))
        self.assertFalse(result.ok)
        self.assertTrue(any("b" in p and "only" in p for p in result.problems), result.problems)

    def test_a_file_in_only_the_first_run_fails_too(self) -> None:
        result = self.run_with(fake({"a": b"1", "only-first": b"1"}), fake({"a": b"1"}))
        self.assertFalse(result.ok)
        self.assertTrue(any("only-first" in p and "only in run 1" in p for p in result.problems), result.problems)

    def test_different_manifest_digests_fail_even_if_the_files_match(self) -> None:
        result = self.run_with(fake({"a": b"1"}, manifest="sha256:" + "a" * 64), fake({"a": b"1"}, manifest="sha256:" + "b" * 64))
        self.assertFalse(result.ok)
        self.assertTrue(any("manifest" in p for p in result.problems), result.problems)

    def test_a_generator_failure_fails_and_is_never_compared_as_a_pass(self) -> None:
        result = self.run_with(fake({"a": b"1"}), fake({"a": b"1"}, code=1))
        self.assertFalse(result.ok)
        self.assertTrue(any("exit" in p for p in result.problems), result.problems)

    def test_an_empty_output_is_a_failure_not_an_identical_pair(self) -> None:
        result = self.run_with(fake({}))
        self.assertFalse(result.ok)
        self.assertTrue(any("no file" in p for p in result.problems), result.problems)

    def test_a_non_empty_output_directory_is_refused_before_any_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "r"
            (out / "run1").mkdir(parents=True)
            (out / "run1" / "stale.json").write_text("x")
            with self.assertRaises(ValueError):
                reproduce(7, out, fake({"a": b"1"}))

    def test_the_rendering_has_both_logs_the_digests_and_no_file_content(self) -> None:
        result = self.run_with(fake({"cases/c1.json": b"SECRET-LOOKING-CONTENT-1234567890"}))
        text = render(result)
        for needle in ("run 1", "run 2", result.tree_digests[0], "manifest", "files: 1", "SYNTHETIC", "PASS"):
            self.assertIn(needle, text)
        self.assertNotIn("SECRET-LOOKING-CONTENT", text)
        self.assertNotIn("cases/c1.json", text.split("PASS")[0] if "FAIL" in text else text)  # a passing summary lists no file names

    def test_a_failing_rendering_says_fail_and_lists_the_problems(self) -> None:
        text = render(self.run_with(fake({"a": b"1"}), fake({"a": b"2"})))
        self.assertIn("FAIL", text)
        self.assertNotIn("PASS", text)


class RealGenerator(unittest.TestCase):
    """The real thing: two separate processes of the real generator at the default seed. It is the N10 evidence in miniature, and it fails if the generator stops being deterministic."""

    def test_two_real_processes_agree_and_match_the_golden_manifest_digest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            result = reproduce(20261020, Path(tmp) / "r", run_generator)
        self.assertTrue(result.ok, result.problems)
        self.assertEqual([GOLDEN, GOLDEN], result.manifest_digests)
        self.assertGreater(result.n_files, 1000)

    def test_the_command_exits_zero_and_writes_a_summary_without_any_generated_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            summary = Path(tmp) / "summary.txt"
            buffer = io.StringIO()
            with redirect_stdout(buffer):
                code = main(["reproduce", "--seed", "20261020", "--out", str(Path(tmp) / "r"), "--summary", str(summary)])
            self.assertEqual(0, code)
            text = summary.read_text(encoding="utf-8")
            self.assertIn("PASS", text)
            self.assertIn(GOLDEN, text)
            self.assertEqual(text, buffer.getvalue().rstrip("\n") + "\n" if not text.endswith("\n") else buffer.getvalue())
            self.assertNotIn("oracle/", text)
            self.assertFalse(re.search(r"BEN-[A-Z]+-\d{4}\.json", text), "a summary names no generated file")


class Workflow(unittest.TestCase):
    """The CI step: it runs the command on every run of the benchmark job, and uploads the summary only, never a generated tree (the oracle documents are in it; INV-22)."""

    def setUp(self) -> None:
        self.text = (ROOT / ".github" / "workflows" / "benchmark.yml").read_text(encoding="utf-8")

    def test_the_job_runs_the_two_process_generation(self) -> None:
        self.assertIn("make bench-reproduce", self.text)
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("-m benchmark.generator reproduce", makefile)
        self.assertIn("bench-reproduce: bench-venv", makefile)

    def test_the_step_runs_only_when_the_benchmark_job_runs_its_tests(self) -> None:
        step = self.text[self.text.index("make bench-reproduce") - 200:self.text.index("make bench-reproduce") + 100]
        self.assertIn("steps.changed.outputs.run == 'true'", step)

    def test_only_the_summary_is_uploaded_never_a_generated_tree(self) -> None:
        upload = self.text[self.text.index("upload-artifact"):]
        self.assertIn("path: ${{ runner.temp }}/bench-out/reproduce-summary.txt", upload)
        for forbidden in ("bench-out/reproduce\n", "run1", "run2", "oracle", "bench-out/reproduce/"):
            self.assertNotIn(forbidden, upload)

    def test_the_action_is_pinned_to_a_commit(self) -> None:
        self.assertRegex(self.text, r"actions/upload-artifact@[0-9a-f]{40}")


if __name__ == "__main__":
    unittest.main()
