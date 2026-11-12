"""MEM-02 (REQ-MEM-002, REQ-BEN-006): a fix PR links the regression case it adds before its fix merges. Written first and seen failing."""
import unittest

from tools.ci.check_regression_link import check, is_fix_pr

FIX = "fix/EXE-09-void-guard"
TESTS = ["services/core/outcome/store/grant_void_test.go", "benchmark/scoring/test_stats.py", "services/core/outcome/store/store.go"]


def body(line: str) -> str:
    return f"Authored By: x\n\n{line}\n\nTask EXE-09 REQ-MEM-002 §38\n"


class Detect(unittest.TestCase):
    def test_a_fix_branch_is_a_fix_pr(self) -> None:
        self.assertTrue(is_fix_pr("fix/EXE-09-x", "EXE-09: something"))

    def test_a_title_that_says_fix_is_a_fix_pr_even_on_another_branch(self) -> None:
        self.assertTrue(is_fix_pr("feat/EXE-09-x", "EXE-09: fix the void guard"))
        self.assertTrue(is_fix_pr("feat/EXE-09-x", "Fixes the void guard"))

    def test_a_feature_pr_is_not_a_fix_pr(self) -> None:
        self.assertFalse(is_fix_pr("feat/EXE-09-x", "EXE-09: add the void guard"))
        self.assertFalse(is_fix_pr("docs/EXE-09-x", "prefix handling"), "'prefix' is not 'fix'")


class Linking(unittest.TestCase):
    def test_a_feature_pr_needs_no_link(self) -> None:
        self.assertEqual([], check("feat/EXE-09-x", "EXE-09: add", body("nothing"), TESTS))

    def test_a_fix_pr_without_a_link_fails(self) -> None:
        got = check(FIX, "EXE-09: fix", body("no link here"), TESTS)
        self.assertTrue(any("Regression case" in p for p in got), got)

    def test_a_link_to_a_test_file_the_pr_changes_passes(self) -> None:
        self.assertEqual([], check(FIX, "EXE-09: fix", body("Regression case: services/core/outcome/store/grant_void_test.go"), TESTS))
        self.assertEqual([], check(FIX, "EXE-09: fix", body("Regression case: benchmark/scoring/test_stats.py::StatsTest::test_x"), TESTS))

    def test_a_link_to_a_case_id_passes(self) -> None:
        self.assertEqual([], check(FIX, "EXE-09: fix", body("Regression case: BEN-AMB-0016"), TESTS))

    def test_a_link_to_a_file_the_pr_does_not_change_fails(self) -> None:
        got = check(FIX, "EXE-09: fix", body("Regression case: benchmark/scoring/test_other.py"), TESTS)
        self.assertTrue(any("not changed by this PR" in p for p in got), got)

    def test_a_link_to_a_source_file_is_not_a_regression_case(self) -> None:
        got = check(FIX, "EXE-09: fix", body("Regression case: services/core/outcome/store/store.go"), TESTS)
        self.assertTrue(any("test file" in p for p in got), got)

    def test_none_or_an_empty_link_is_refused_for_a_fix(self) -> None:
        for line in ("Regression case: none", "Regression case:", "Regression case: n/a", "Regression case: TBD"):
            self.assertTrue(any("names no case" in p for p in check(FIX, "EXE-09: fix", body(line), TESTS)), line)

    def test_a_link_inside_a_code_block_or_quote_does_not_count(self) -> None:
        self.assertTrue(check(FIX, "EXE-09: fix", "```\nRegression case: BEN-AMB-0016\n```\nEXE-09", TESTS))
        self.assertTrue(check(FIX, "EXE-09: fix", "> Regression case: BEN-AMB-0016\nEXE-09", TESTS))

    def test_a_link_inside_any_fence_form_does_not_count(self) -> None:
        """Codex P2 on noema#452: the closing fence must match the opening one's character and be at least as long (CommonMark), so a shorter or different fence inside a block does not end it."""
        forms = [
            "````\n```\nRegression case: BEN-AMB-0016\n````",          # a three-backtick line inside a four-backtick fence
            "~~~\nRegression case: BEN-AMB-0016\n~~~",                  # tilde fence
            "```\n~~~\nRegression case: BEN-AMB-0016\n```",            # a tilde line inside a backtick fence
            "~~~~\n~~~\nRegression case: BEN-AMB-0016\n~~~~",          # a shorter tilde line inside a longer one
            "   ```python\nRegression case: BEN-AMB-0016\n   ```",      # up to three spaces of indent and an info string
            "```\nRegression case: BEN-AMB-0016",                       # never closed: the rest of the body is code
        ]
        for form in forms:
            self.assertTrue(check(FIX, "EXE-09: fix", form + "\nEXE-09", TESTS), form)

    def test_a_link_after_a_properly_closed_fence_counts(self) -> None:
        for form in ("```\nexample\n```\nRegression case: BEN-AMB-0016", "````\n```\nx\n````\nRegression case: BEN-AMB-0016", "~~~\nx\n~~~\nRegression case: BEN-AMB-0016"):
            self.assertEqual([], check(FIX, "EXE-09: fix", form, TESTS), form)

    def test_a_closing_fence_with_text_after_it_does_not_close_the_block(self) -> None:
        self.assertTrue(check(FIX, "EXE-09: fix", "```\nx\n``` still code\nRegression case: BEN-AMB-0016\n```\nEXE-09", TESTS))

    def test_a_closing_fence_indented_four_spaces_does_not_close_the_block(self) -> None:
        self.assertTrue(check(FIX, "EXE-09: fix", "```\nx\n    ```\nRegression case: BEN-AMB-0016\nEXE-09", TESTS))

    def test_an_indented_four_spaces_fence_is_not_a_fence(self) -> None:
        self.assertEqual([], check(FIX, "EXE-09: fix", "    ```\nRegression case: BEN-AMB-0016", TESTS))

    def test_a_malformed_case_id_fails(self) -> None:
        self.assertTrue(check(FIX, "EXE-09: fix", body("Regression case: BEN-16"), TESTS))


if __name__ == "__main__":
    unittest.main()
