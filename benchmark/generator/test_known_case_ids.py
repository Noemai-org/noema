"""A652 (MEM-02; REQ-MEM-002 "a linked new case ID"): the regression-link check reads the case IDs of the generated suite. This proves it reads the real suite: the same IDs `generate` writes, nothing more,
and it refuses a well-formed ID that no case carries. The check's own logic is tested without the generator in `tools/ci/test_check_regression_link.py`."""
import unittest

from benchmark.generator.generate import DEFAULT_SEED, generate
from tools.ci.check_regression_link import check, load_known_case_ids


class KnownCaseIds(unittest.TestCase):
    def test_the_loader_returns_exactly_the_case_ids_of_the_generated_suite(self) -> None:
        ids = load_known_case_ids()
        self.assertEqual(ids, frozenset(generate(DEFAULT_SEED).cases))
        self.assertGreaterEqual(len(ids), 300)
        self.assertIn("BEN-CLEAN-0001", ids)
        self.assertNotIn("BEN-CLEAN-9999", ids)

    def test_a_real_case_passes_and_an_invented_one_fails_through_the_real_suite(self) -> None:
        ids = load_known_case_ids()
        body = "Regression case: {}\n"
        self.assertEqual([], check("fix/EXE-09-x", "EXE-09: fix", body.format("BEN-CLEAN-0001"), [], known_case_ids=ids))
        got = check("fix/EXE-09-x", "EXE-09: fix", body.format("BEN-CLEAN-9999"), [], known_case_ids=ids)
        self.assertTrue(got and "BEN-CLEAN-9999" in got[0], got)


if __name__ == "__main__":
    unittest.main()
