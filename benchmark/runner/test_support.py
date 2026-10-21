"""The support report: which cases this build can run, and exactly what stops the rest (no case is ever dropped from the count)."""
import unittest

from benchmark.runner.support import report
from benchmark.runner.suite import load_suite
from benchmark.runner.test_suite import suite_dir


class Support(unittest.TestCase):
    def test_every_case_is_counted_once(self) -> None:
        suite = load_suite(suite_dir())
        r = report(suite)
        self.assertEqual(r["total"], 320)
        self.assertEqual(r["runnable"] + r["blocked"], 320)
        self.assertEqual(sum(1 for lc in suite.cases if lc.plan is None and all(e["event"] == "session_start:synthetic" for e in lc.case["events_timeline"])),
                         r["without_faults_or_events"])

    def test_a_fault_no_producer_has_is_its_own_reason(self) -> None:
        r = report(load_suite(suite_dir()))
        self.assertTrue(any(k.startswith("event: needs_fault:") for k in r["blocked_by"]), r["blocked_by"])

    def test_reasons_name_their_cause(self) -> None:
        r = report(load_suite(suite_dir()))
        self.assertTrue(any(k.startswith("fault: ") for k in r["blocked_by"]))
        self.assertTrue(any(k.startswith("event: ") for k in r["blocked_by"]))


if __name__ == "__main__":
    unittest.main()
