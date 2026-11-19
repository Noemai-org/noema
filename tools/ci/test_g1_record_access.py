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


def request(corr: str, kind: str = "reconstruction", status: int = 200, **extra: object) -> dict[str, object]:
    d: dict[str, object] = {"case_id": "c", "correlation_id": corr, "method": "GET", "path": "/v1/reconstruction/x", "status": status, "kind": kind}
    d.update(extra)
    return d


ONE_EACH = [request("corr_a"), request("corr_b")]


class RecordAccessed(unittest.TestCase):
    def check(self, lines: list[str], correlations: dict[str, str] | None = None, requests: object = "default") -> tuple[int, str]:
        """`requests` is the runner's own per-request record (A354): a list of objects, raw text, or None for no file. The expected count comes from it, never from a constant."""
        with tempfile.TemporaryDirectory() as tmp:
            log, cor, req = Path(tmp) / "core.log", Path(tmp) / "correlations.json", Path(tmp) / "requests.jsonl"
            log.write_text("\n".join(lines) + "\n", encoding="utf-8")
            cor.write_text(json.dumps(correlations if correlations is not None else {"c1": "corr_a", "c2": "corr_b"}), encoding="utf-8")
            if requests == "default":
                requests = ONE_EACH
            if isinstance(requests, str):
                req.write_text(requests, encoding="utf-8")
            elif requests is not None:
                req.write_text("".join(json.dumps(x) + "\n" for x in requests), encoding="utf-8")  # type: ignore[attr-defined]
            r = subprocess.run([sys.executable, "-I", str(SCRIPT), "--core-log", str(log), "--correlations", str(cor), "--requests", str(req)], capture_output=True, text=True, check=False)
            return r.returncode, r.stdout + r.stderr

    GOOD = [line("corr_a", "SERVED"), line("corr_a", "NOT_FOUND_OR_NOT_AUTHORIZED"), line("corr_b", "SERVED"), '{"msg":"other"}']

    def test_one_served_line_per_case_is_met(self) -> None:
        self.assertEqual(self.check(self.GOOD)[0], 0)

    # A354: the expected count is derived from the run's own request record, not a constant.
    def test_the_expected_count_follows_the_requests_the_runner_made(self) -> None:
        two = [request("corr_a"), request("corr_a"), request("corr_b")]  # US-06 style: an initial read and a read after recovery
        self.assertEqual(self.check(self.GOOD + [line("corr_a", "SERVED")], requests=two)[0], 0)
        code, text = self.check(self.GOOD, requests=two)  # one SERVED line where the record says two requests were made
        self.assertEqual(code, 1, text)
        self.assertIn("corr_a", text)

    def test_a_probe_or_a_refused_read_does_not_add_to_the_expected_count(self) -> None:
        extra = ONE_EACH + [request("corr_a", kind="probe", status=404), request("corr_a", kind="probe", status=401), request("corr_b", status=404), request("corr_a", kind="case")]
        self.assertEqual(self.check(self.GOOD, requests=extra)[0], 0)

    def test_a_missing_empty_or_unreadable_request_record_is_not_run(self) -> None:
        for what, requests in (("missing", None), ("empty", ""), ("no reconstruction request", [request("corr_a", kind="case")])):
            with self.subTest(what):
                code, text = self.check(self.GOOD, requests=requests)
                self.assertEqual(code, 3, text)

    def test_a_request_line_with_anything_beyond_the_six_fields_is_not_met(self) -> None:
        for extra in ({"authorization": "x"}, {"body": "{}"}, {"token": "t"}):
            with self.subTest(list(extra)):
                code, text = self.check(self.GOOD, requests=ONE_EACH + [request("corr_a", **extra)])
                self.assertEqual(code, 1, text)
                self.assertNotIn("Bearer", text)

    def test_a_malformed_request_line_or_a_request_for_another_run_is_not_met(self) -> None:
        for what, requests in (
            ("not json", json.dumps(ONE_EACH[0]) + "\nnot json\n"),
            ("a string status", ONE_EACH + [request("corr_a", status="200")]),
            ("an unknown kind", ONE_EACH + [request("corr_a", kind="other")]),
            ("a correlation that is not a case of this run", ONE_EACH + [request("corr_zzz")]),
        ):
            with self.subTest(what):
                code, text = self.check(self.GOOD, requests=requests)
                self.assertEqual(code, 1, text)

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
        r = subprocess.run([sys.executable, "-I", str(SCRIPT), "--core-log", "/nonexistent", "--correlations", "/nonexistent", "--requests", "/nonexistent"], capture_output=True, text=True, check=False)
        self.assertEqual(r.returncode, 3)

    def test_a_line_that_carries_record_content_is_not_met(self) -> None:
        code, text = self.check(self.GOOD + [line("corr_a", "NOT_FOUND", payload="x")])
        self.assertEqual(code, 1, text)
        self.assertIn("payload", text)

    def test_a_malformed_record_accessed_line_is_not_met(self) -> None:
        self.assertEqual(self.check(self.GOOD + ['RECORD_ACCESSED not json'])[0], 1)


if __name__ == "__main__":
    unittest.main()
