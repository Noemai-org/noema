"""The RECORD_ACCESSED check of the G1 stack run (SR's first-dispatch condition 1 on noema#436; REQ-AUD-003; G1 evidence item 3: one RECORD_ACCESSED line per read).
Runs the real script on real files. SYNTHETIC log lines."""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "g1_record_access.py"


def line(corr: str, outcome: str, **extra: str) -> str:
    d = {"time": "t", "level": "INFO", "msg": "RECORD_ACCESSED", "tenant_id": "ten_alpha", "principal": "prn_1", "correlation_id": corr, "outcome": outcome}
    d.update(extra)
    return json.dumps(d)


class RecordAccessed(unittest.TestCase):
    def check(self, lines: list[str], correlations: dict[str, str] | None = None) -> tuple[int, str]:
        with tempfile.TemporaryDirectory() as tmp:
            log, cor = Path(tmp) / "core.log", Path(tmp) / "correlations.json"
            log.write_text("\n".join(lines) + "\n", encoding="utf-8")
            cor.write_text(json.dumps(correlations if correlations is not None else {"c1": "corr_a", "c2": "corr_b"}), encoding="utf-8")
            r = subprocess.run([sys.executable, "-I", str(SCRIPT), "--core-log", str(log), "--correlations", str(cor)], capture_output=True, text=True, check=False)
            return r.returncode, r.stdout + r.stderr

    GOOD = [line("corr_a", "SERVED"), line("corr_a", "NOT_FOUND_OR_NOT_AUTHORIZED"), line("corr_b", "SERVED"), '{"msg":"other"}']

    def test_one_served_line_per_case_is_met(self) -> None:
        self.assertEqual(self.check(self.GOOD)[0], 0)

    def test_a_case_with_no_served_line_is_not_met(self) -> None:
        code, text = self.check([line("corr_a", "SERVED")])
        self.assertEqual(code, 1, text)
        self.assertIn("corr_b", text)

    def test_two_served_lines_for_one_case_is_not_met(self) -> None:
        self.assertEqual(self.check(self.GOOD + [line("corr_a", "SERVED")])[0], 1)

    def test_a_served_line_for_an_unknown_correlation_is_not_met(self) -> None:
        self.assertEqual(self.check(self.GOOD + [line("corr_zzz", "SERVED")])[0], 1)

    def test_no_line_at_all_is_not_run_never_zero(self) -> None:
        self.assertEqual(self.check(['{"msg":"other"}'])[0], 3)

    def test_an_unreadable_log_or_no_correlations_is_not_run(self) -> None:
        self.assertEqual(self.check(self.GOOD, {})[0], 3)
        r = subprocess.run([sys.executable, "-I", str(SCRIPT), "--core-log", "/nonexistent", "--correlations", "/nonexistent"], capture_output=True, text=True, check=False)
        self.assertEqual(r.returncode, 3)

    def test_a_line_that_carries_record_content_is_not_met(self) -> None:
        code, text = self.check(self.GOOD + [line("corr_a", "NOT_FOUND", payload="x")])
        self.assertEqual(code, 1, text)
        self.assertIn("payload", text)

    def test_a_malformed_record_accessed_line_is_not_met(self) -> None:
        self.assertEqual(self.check(self.GOOD + ['RECORD_ACCESSED not json'])[0], 1)


if __name__ == "__main__":
    unittest.main()
