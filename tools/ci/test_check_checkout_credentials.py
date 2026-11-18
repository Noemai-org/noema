#!/usr/bin/env python3
"""The checkout-credentials check fails for the right reason and passes for the right shape (PLT-05, REQ-SEC-005, REQ-SEC-007).

Each case writes a small workflow into a temporary `.github/` and runs the check on it. A case that only passes because the check ignores something is not a case: every refusal below
changes exactly one thing from a passing document. The last test runs the check on this repository's own `.github/`.

Run: python3 -I tools/ci/test_check_checkout_credentials.py
"""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_checkout_credentials as check  # noqa: E402

PIN = "actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4"
GOOD = f"""name: t
on: push
jobs:
  one:
    runs-on: ubuntu-24.04
    steps:
      - uses: {PIN}
        with:
          persist-credentials: false
      - run: echo hello
"""


def run(files: dict[str, str]) -> list[str]:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp) / ".github"
        for name, text in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text, encoding="utf-8")
        return check.violations(root)


class Passes(unittest.TestCase):
    def test_the_good_document_passes(self) -> None:
        self.assertEqual(run({"workflows/t.yml": GOOD}), [])

    def test_the_key_may_follow_other_keys_of_the_same_with_block(self) -> None:
        text = GOOD.replace("          persist-credentials: false\n", "          fetch-depth: 0\n          persist-credentials: false\n          ref: main\n")
        self.assertEqual(run({"workflows/t.yml": text}), [])

    def test_a_named_step_and_a_comment_after_the_value_pass(self) -> None:
        text = f"""jobs:
  j:
    steps:
      - name: Check out
        uses: {PIN}
        with:
          persist-credentials: false  # nothing later needs the token
"""
        self.assertEqual(run({"workflows/t.yml": text}), [])

    def test_a_workflow_with_no_checkout_passes(self) -> None:
        self.assertEqual(run({"workflows/t.yml": "jobs:\n  j:\n    steps:\n      - run: echo hi\n"}), [])

    def test_a_composite_action_is_read_too(self) -> None:
        action = f"runs:\n  using: composite\n  steps:\n    - uses: {PIN}\n      with:\n        persist-credentials: false\n"
        self.assertEqual(run({"actions/x/action.yml": action}), [])


class Refuses(unittest.TestCase):
    def assertRefused(self, files: dict[str, str], where: str) -> None:
        found = run(files)
        self.assertEqual(len(found), 1, found)
        self.assertIn(where, found[0])
        self.assertIn("persist-credentials", found[0])

    def test_no_with_block(self) -> None:
        self.assertRefused({"workflows/t.yml": f"jobs:\n  j:\n    steps:\n      - uses: {PIN}\n      - run: echo\n"}, "workflows/t.yml:4")

    def test_a_with_block_without_the_key(self) -> None:
        self.assertRefused({"workflows/t.yml": GOOD.replace("          persist-credentials: false\n", "          fetch-depth: 0\n")}, "workflows/t.yml:7")

    def test_true(self) -> None:
        self.assertRefused({"workflows/t.yml": GOOD.replace("persist-credentials: false", "persist-credentials: true")}, "workflows/t.yml:7")

    def test_an_expression_a_quoted_string_and_an_empty_value_are_not_the_literal_false(self) -> None:
        for value in ("${{ inputs.keep }}", '"false"', "'false'", "", "False", "no"):
            with self.subTest(value=value):
                self.assertRefused({"workflows/t.yml": GOOD.replace("persist-credentials: false", f"persist-credentials: {value}".rstrip())}, "workflows/t.yml:7")

    def test_a_comment_that_names_the_key_does_not_count(self) -> None:
        self.assertRefused({"workflows/t.yml": GOOD.replace("          persist-credentials: false\n", "          # persist-credentials: false\n          fetch-depth: 0\n")}, "workflows/t.yml:7")

    def test_the_key_of_another_step_does_not_count(self) -> None:
        text = f"""jobs:
  j:
    steps:
      - uses: {PIN}
      - uses: actions/setup-python@a26af69be951a213d495a4c3e4e4022e16d87065 # v5
        with:
          persist-credentials: false
"""
        self.assertRefused({"workflows/t.yml": text}, "workflows/t.yml:4")

    def test_the_key_in_the_wrong_block_does_not_count(self) -> None:
        text = f"""jobs:
  j:
    steps:
      - uses: {PIN}
        env:
          persist-credentials: false
"""
        self.assertRefused({"workflows/t.yml": text}, "workflows/t.yml:4")

    def test_one_good_checkout_does_not_cover_a_second_bad_one(self) -> None:
        text = GOOD + f"      - uses: {PIN}\n        with:\n          repository: other/repo\n"
        self.assertRefused({"workflows/t.yml": text}, "workflows/t.yml:11")

    def test_the_last_step_of_the_file_without_a_trailing_newline(self) -> None:
        self.assertRefused({"workflows/t.yml": f"jobs:\n  j:\n    steps:\n      - uses: {PIN}"}, "workflows/t.yml:4")

    def test_each_bad_file_is_named(self) -> None:
        bad = f"jobs:\n  j:\n    steps:\n      - uses: {PIN}\n"
        found = run({"workflows/a.yml": bad, "workflows/b.yml": GOOD, "workflows/c.yml": bad})
        self.assertEqual([item.split(":")[0] for item in found], ["workflows/a.yml", "workflows/c.yml"])

    def test_a_file_that_cannot_be_decoded_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / ".github"
            (root / "workflows").mkdir(parents=True)
            (root / "workflows" / "t.yml").write_bytes(b"\xff\xfe\x00bad")
            found = check.violations(root)
        self.assertEqual(len(found), 1)
        self.assertIn("cannot be read", found[0])


KEY_LINE = "          persist-credentials: false\n"
WITH_BLOCK = "        with:\n" + KEY_LINE


class QuotedKeys(unittest.TestCase):
    """YAML allows a quoted key: `"persist-credentials": false` is the same document as the bare one (Codex P2 on noema#622). A check that reads only bare keys would call a quoted
    `true` safe, so every key the check reads is read in both spellings, and the refusals below are the cases that matter."""

    @staticmethod
    def quote(text: str, *names: str, mark: str = '"') -> str:
        for name in names:
            text = text.replace(f"{name}:", f"{mark}{name}{mark}:")
        return text

    def test_a_quoted_key_with_false_passes(self) -> None:
        for mark in ('"', "'"):
            with self.subTest(mark=mark):
                self.assertEqual(run({"workflows/t.yml": self.quote(GOOD, "persist-credentials", mark=mark)}), [])

    def test_a_quoted_with_key_passes(self) -> None:
        self.assertEqual(run({"workflows/t.yml": self.quote(GOOD, "with")}), [])

    def test_a_quoted_uses_key_is_still_a_checkout(self) -> None:
        bad = GOOD.replace("      - uses:", '      - "uses":').replace(WITH_BLOCK, "")
        found = run({"workflows/t.yml": bad})
        self.assertEqual(len(found), 1, found)
        self.assertIn("workflows/t.yml:7", found[0])

    def test_a_quoted_key_with_true_is_refused(self) -> None:
        for mark in ('"', "'"):
            with self.subTest(mark=mark):
                text = self.quote(GOOD, "persist-credentials", mark=mark).replace("false", "true")
                found = run({"workflows/t.yml": text})
                self.assertEqual(len(found), 1, found)
                self.assertIn("persist-credentials", found[0])

    def test_a_quoted_key_that_is_a_comment_does_not_count(self) -> None:
        text = GOOD.replace(KEY_LINE, '          # "persist-credentials": false\n          fetch-depth: 0\n')
        self.assertEqual(len(run({"workflows/t.yml": text})), 1)

    def test_a_quoted_key_in_another_step_does_not_count(self) -> None:
        text = f"""jobs:
  j:
    steps:
      - uses: {PIN}
      - uses: actions/setup-python@a26af69be951a213d495a4c3e4e4022e16d87065 # v5
        with:
          "persist-credentials": false
"""
        found = run({"workflows/t.yml": text})
        self.assertEqual(len(found), 1, found)
        self.assertIn("workflows/t.yml:4", found[0])


class UnsupportedForms(unittest.TestCase):
    """A checkout the check cannot read is refused, not skipped (Codex P2 on noema#622)."""

    def refused(self, step: str) -> None:
        text = chr(10).join(["jobs:", "  j:", "    steps:", step, ""])
        found = run({"workflows/t.yml": text})
        self.assertEqual(len(found), 1, found)
        self.assertIn("form this check does not read", found[0])

    def test_a_flow_mapping_step_is_refused(self) -> None:
        self.refused("      - {uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262}")

    def test_a_flow_mapping_step_that_sets_the_key_is_refused_too(self) -> None:
        self.refused("      - {uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262, with: {persist-credentials: false}}")

    def test_a_space_before_the_colon_is_refused(self) -> None:
        self.refused("      - uses : actions/checkout@11d5960a326750d5838078e36cf38b85af677262")

    def test_a_json_style_step_is_refused(self) -> None:
        self.refused('      - {"uses": "actions/checkout@11d5960a326750d5838078e36cf38b85af677262"}')

    def test_a_comment_that_names_a_checkout_is_not_a_checkout(self) -> None:
        comment = "      # was: uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262" + chr(10)
        self.assertEqual(run({"workflows/t.yml": GOOD + comment}), [])


    def test_a_quoted_hash_before_the_checkout_key_does_not_hide_it(self) -> None:
        for quote in ("'", '"'):
            self.refused(f"      - {{name: {quote}# label{quote}, uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262}}")

    def test_a_quoted_hash_followed_by_a_real_comment_is_still_read(self) -> None:
        self.refused("      - {name: '# a', uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262} # and a comment")

    def test_a_hash_inside_a_word_is_not_a_comment(self) -> None:
        self.refused("      - {name: a#b, uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262}")

    def test_a_hash_in_a_block_style_name_does_not_hide_a_missing_key(self) -> None:
        text = chr(10).join(["jobs:", "  j:", "    steps:", "      - name: '# label'", "        uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262", ""])
        found = run({"workflows/t.yml": text})
        self.assertEqual(len(found), 1, found)
        self.assertIn("workflows/t.yml:5", found[0])


class ThisRepository(unittest.TestCase):
    def test_every_checkout_of_this_repository_sets_it(self) -> None:
        github = Path(__file__).resolve().parents[2] / ".github"
        self.assertTrue((github / "workflows").is_dir())
        self.assertGreaterEqual(sum(p.read_text(encoding="utf-8").count("actions/checkout@") for p in (github / "workflows").glob("*.yml")), 20)
        self.assertEqual(check.violations(github), [])


if __name__ == "__main__":
    unittest.main()
