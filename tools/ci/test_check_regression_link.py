"""MEM-02 (REQ-MEM-002, REQ-BEN-006): a fix PR links the regression case it adds before its fix merges. Written first and seen failing."""
import os
import unittest
from unittest import mock

from tools.ci.check_regression_link import check, is_fix_pr, main

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


class Label(unittest.TestCase):
    """Codex P1 on docs#160: the acceptance criterion of REQ-MEM-002 is a PR *labelled* `fix`; branch and title only supplement it."""

    def test_the_fix_label_makes_a_fix_pr_whatever_the_branch_and_the_title(self) -> None:
        self.assertTrue(is_fix_pr("feat/EXE-09-x", "EXE-09: add the void guard", labels=["fix"]))
        self.assertTrue(is_fix_pr("feat/EXE-09-x", "EXE-09: add the void guard", labels=["P0", "Fix"]))

    def test_another_label_does_not(self) -> None:
        self.assertFalse(is_fix_pr("feat/EXE-09-x", "EXE-09: add the void guard", labels=["prefix", "bug-report", "P0"]))

    def test_a_fix_labelled_pr_without_a_link_fails_even_on_a_feature_branch(self) -> None:
        got = check("feat/EXE-09-x", "EXE-09: add the void guard", body("no link here"), TESTS, labels=["fix"])
        self.assertTrue(got and "REQ-MEM-002" in got[0], got)

    def test_a_fix_labelled_pr_with_a_case_id_passes(self) -> None:
        self.assertEqual([], check("feat/EXE-09-x", "EXE-09: add", body("Regression case: BEN-AMB-0016"), TESTS, labels=["fix"]))

    def test_the_command_reads_the_labels_from_the_environment(self) -> None:
        env = {"HEAD_REF": "feat/EXE-09-x", "PR_TITLE": "EXE-09: add", "PR_BODY": body("nothing"), "CHANGED_FILES": "", "PR_LABELS": "P0\nfix"}
        with mock.patch.dict(os.environ, env, clear=True):
            self.assertEqual(1, main())
        with mock.patch.dict(os.environ, {**env, "PR_LABELS": "P0"}, clear=True):
            self.assertEqual(0, main())


class Linking(unittest.TestCase):
    def test_a_feature_pr_needs_no_link(self) -> None:
        self.assertEqual([], check("feat/EXE-09-x", "EXE-09: add", body("nothing"), TESTS))

    def test_a_fix_pr_without_a_link_fails(self) -> None:
        got = check(FIX, "EXE-09: fix", body("no link here"), TESTS)
        self.assertTrue(any("Regression case" in p for p in got), got)

    def test_a_link_to_a_test_file_the_pr_changes_is_refused_by_default(self) -> None:
        # REQ-MEM-002 asks for "a named, permanent benchmark case" and a "linked new case ID". The test-file alternative is ruling A156 (docs#157), which is not committed until a Founder
        # merges that pull request (AGENTS.md section 2.3), so the check does not accept it unless the workflow turns it on after that merge (Codex P1 on docs#160).
        got = check(FIX, "EXE-09: fix", body("Regression case: services/core/outcome/store/grant_void_test.go"), TESTS)
        self.assertTrue(got and "A156" in got[0] and "case ID" in got[0], got)

    def test_a_link_to_a_test_file_the_pr_changes_passes_once_the_ruling_is_enabled(self) -> None:
        self.assertEqual([], check(FIX, "EXE-09: fix", body("Regression case: services/core/outcome/store/grant_void_test.go"), TESTS, allow_test_file=True))
        self.assertEqual([], check(FIX, "EXE-09: fix", body("Regression case: benchmark/scoring/test_stats.py::StatsTest::test_x"), TESTS, allow_test_file=True))

    def test_the_command_enables_the_test_file_alternative_only_by_its_flag(self) -> None:
        env = {"HEAD_REF": FIX, "PR_TITLE": "EXE-09: fix", "PR_BODY": body("Regression case: benchmark/scoring/test_stats.py"), "CHANGED_FILES": "benchmark/scoring/test_stats.py"}
        with mock.patch.dict(os.environ, env, clear=True):
            self.assertEqual(1, main())
        with mock.patch.dict(os.environ, {**env, "REGRESSION_ALLOW_TEST_FILE": "1"}, clear=True):
            self.assertEqual(0, main())

    def test_a_link_to_a_case_id_passes(self) -> None:
        self.assertEqual([], check(FIX, "EXE-09: fix", body("Regression case: BEN-AMB-0016"), TESTS))

    def test_a_link_to_a_file_the_pr_does_not_change_fails(self) -> None:
        got = check(FIX, "EXE-09: fix", body("Regression case: benchmark/scoring/test_other.py"), TESTS, allow_test_file=True)
        self.assertTrue(any("not changed by this PR" in p for p in got), got)

    def test_a_link_to_a_source_file_is_not_a_regression_case(self) -> None:
        got = check(FIX, "EXE-09: fix", body("Regression case: services/core/outcome/store/store.go"), TESTS, allow_test_file=True)
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
