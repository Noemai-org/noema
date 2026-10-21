"""`python -m benchmark.bakeoff cases | run | report`."""
import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

from benchmark.bakeoff.__main__ import main
from benchmark.bakeoff.cases import generate
from benchmark.bakeoff.testdata import make_run
from benchmark.contracts import Contracts, pretty_bytes

CONTRACTS = Contracts()


def cli(*argv: str, env: dict[str, str] | None = None) -> tuple[int, str]:
    buffer = io.StringIO()
    with redirect_stdout(buffer):
        code = main(list(argv), environ=env or {})
    return code, buffer.getvalue()


class CasesCommandTest(unittest.TestCase):
    def test_writes_valid_cases_and_reruns_byte_identically(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            a, b = Path(tmp) / "a.json", Path(tmp) / "b.json"
            self.assertEqual(cli("cases", "--seed", "4", "--per-cell", "3", "--out", str(a))[0], 0)
            self.assertEqual(cli("cases", "--seed", "4", "--per-cell", "3", "--out", str(b))[0], 0)
            self.assertEqual(a.read_bytes(), b.read_bytes())
            cases = json.loads(a.read_text())
            self.assertEqual(len(cases), 6 * 4 * 3)
            self.assertTrue(all(CONTRACTS.errors("benchmark-decision-case", c) == [] for c in cases))


class RunCommandTest(unittest.TestCase):
    def test_without_a_key_the_run_is_not_run_with_the_reason(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cases, out = Path(tmp) / "cases.json", Path(tmp) / "run.json"
            cli("cases", "--seed", "4", "--per-cell", "2", "--out", str(cases))
            code, text = cli("run", "--cases", str(cases), "--provider", "DECIDE_OPENAI_DECISIONS", "--run-id", "r1",
                             "--runner-region", "unknown", "--out", str(out))
            run = json.loads(out.read_text())
            self.assertEqual((code, run["status"], run["results"]), (0, "NOT_RUN", []))
            self.assertIn("no access", run["not_run_reason"])
            self.assertEqual(CONTRACTS.errors("benchmark-decision-run", run), [])
            self.assertIn("NOT RUN", text)

    def test_a_key_in_the_environment_is_never_printed_or_written(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cases, out = Path(tmp) / "cases.json", Path(tmp) / "run.json"
            cli("cases", "--seed", "4", "--per-cell", "2", "--out", str(cases))
            _, text = cli("run", "--cases", str(cases), "--provider", "LLM", "--run-id", "r1", "--runner-region", "unknown",
                          "--out", str(out), env={"OPENAI_API_KEY": "sk-canary-value"})
            self.assertNotIn("sk-canary-value", text)
            self.assertNotIn("sk-canary-value", out.read_text())
            self.assertIn("blocked on session D", out.read_text())


class ReportCommandTest(unittest.TestCase):
    def test_report_from_recorded_runs_writes_json_markdown_and_one_svg_per_task(self) -> None:
        cases = generate(4, 3)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "cases.json").write_bytes(pretty_bytes(cases))
            (root / "llm.json").write_bytes(pretty_bytes(make_run(cases, "LLM")))
            notrun = {"run_id": "x", "provider": "DECIDE_OPENAI_DECISIONS", "status": "NOT_RUN", "not_run_reason": "no access: no key",
                      "runner_region": "unknown", "cases_version": make_run(cases)["cases_version"], "results": []}
            (root / "decide.json").write_bytes(pretty_bytes(notrun))
            code, _ = cli("report", "--cases", str(root / "cases.json"), "--runs", str(root / "llm.json"), str(root / "decide.json"), "--out", str(root / "out"))
            self.assertEqual(code, 0)
            names = sorted(p.name for p in (root / "out").iterdir())
            tasks = ["INTENT_NODE", "ENUM_SLOT", "AMBIGUITY_FLAG", "LOCALE", "ANSWER_BINDING", "INSTRUCTION_SCREEN"]
            self.assertEqual(names, sorted(["comparison.json", "report.md", *[f"reliability_{t}.svg" for t in tasks]]))
            self.assertIn("not run: no access: no key", (root / "out" / "report.md").read_text())

    def test_an_invalid_run_file_exits_two_and_writes_nothing(self) -> None:
        cases = generate(4, 2)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "cases.json").write_bytes(pretty_bytes(cases))
            bad = make_run(cases, "LLM")
            bad["results"][0]["latency_ms"] = -1
            (root / "bad.json").write_bytes(pretty_bytes(bad))
            (root / "ok.json").write_bytes(pretty_bytes(make_run(cases, "DECIDE_OPENAI_DECISIONS")))
            code, text = cli("report", "--cases", str(root / "cases.json"), "--runs", str(root / "bad.json"), str(root / "ok.json"), "--out", str(root / "out"))
            self.assertEqual(code, 2)
            self.assertFalse((root / "out").exists())

    def test_a_path_naming_the_held_out_repository_is_refused(self) -> None:
        code, text = cli("cases", "--seed", "1", "--per-cell", "1", "--out", "/tmp/benchmark-heldout/cases.json")
        self.assertEqual(code, 2)
        self.assertIn("benchmark-heldout", text)


if __name__ == "__main__":
    unittest.main()
