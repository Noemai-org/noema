#!/usr/bin/env python3
"""The workflow-permissions check fails for the right reason and passes for the right shape (REQ-SEC-007; PLT-02).

Each case writes small workflows into a temporary directory and runs the check on them. Every refusal changes exactly one thing from a passing document, so it cannot pass because the check
refuses everything. The last class runs the check on this repository's own workflows.

Run: python3 -I tools/ci/test_check_workflow_permissions.py
"""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_workflow_permissions as check  # noqa: E402

GOOD = """name: t
on: push
permissions:
  contents: read
jobs:
  one:
    runs-on: ubuntu-24.04
    steps:
      - run: echo hello
"""


def run(files: dict[str, str]) -> list[str]:
    with tempfile.TemporaryDirectory() as tmp:
        for name, text in files.items():
            (Path(tmp) / name).write_text(text, encoding="utf-8")
        return check.violations(Path(tmp))


def only(found: list[str], *needles: str) -> None:
    """Exactly one problem was found, and it mentions every needle (explicit raises: the check must not depend on `assert` surviving `python -O`)."""
    if len(found) != 1:
        raise AssertionError(f"want exactly one problem, got {found}")
    for needle in needles:
        if needle not in found[0]:
            raise AssertionError(f"{needle!r} not in {found[0]!r}")


class Passes(unittest.TestCase):
    def test_contents_read_alone(self) -> None:
        self.assertEqual(run({"t.yml": GOOD}), [])

    def test_read_permissions_of_other_kinds(self) -> None:
        self.assertEqual(run({"t.yml": GOOD.replace("  contents: read\n", "  contents: read\n  pull-requests: read\n  checks: none\n")}), [])

    def test_an_empty_mapping_is_the_strictest_block(self) -> None:
        self.assertEqual(run({"t.yml": GOOD.replace("permissions:\n  contents: read\n", "permissions: {}\n")}), [])

    def test_a_comment_after_a_value_and_a_comment_line_do_not_matter(self) -> None:
        text = GOOD.replace("  contents: read\n", "  # nothing may write\n  contents: read  # the token reads the repository only\n")
        self.assertEqual(run({"t.yml": text}), [])

    def test_a_comment_that_names_the_forbidden_words_passes(self) -> None:
        text = "# not pull_request_target, not write-all, no id-token\n" + GOOD
        self.assertEqual(run({"t.yml": text}), [])

    def test_a_reviewed_write_in_its_own_workflow_passes(self) -> None:
        text = GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n")
        with unittest_allow({("t.yml", "issues"): "test"}):
            self.assertEqual(run({"t.yml": text}), [])

    def test_a_reviewed_write_at_job_level_passes(self) -> None:
        text = GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions:\n      issues: write\n")
        with unittest_allow({("t.yml", "issues"): "test"}):
            self.assertEqual(run({"t.yml": text}), [])


class Refuses(unittest.TestCase):
    def test_no_permissions_block(self) -> None:
        only(run({"t.yml": GOOD.replace("permissions:\n  contents: read\n", "")}), "t.yml", "no top-level `permissions:`")

    def test_a_block_that_exists_only_at_job_level_is_not_top_level(self) -> None:
        text = GOOD.replace("permissions:\n  contents: read\n", "").replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions:\n      contents: read\n")
        only(run({"t.yml": text}), "no top-level `permissions:`")

    def test_a_scalar_block(self) -> None:
        for scalar in ("write-all", "read-all", "read"):
            with self.subTest(scalar=scalar):
                found = run({"t.yml": GOOD.replace("permissions:\n  contents: read\n", f"permissions: {scalar}\n")})
                self.assertTrue(any("must be a mapping" in item for item in found), found)

    def test_top_level_contents_write(self) -> None:
        found = run({"t.yml": GOOD.replace("contents: read", "contents: write")})
        self.assertTrue(any("top-level `contents` must be read or none" in item for item in found), found)
        self.assertTrue(any("not in ALLOW_WRITE" in item for item in found), found)

    def test_a_write_that_is_not_listed(self) -> None:
        only(run({"t.yml": GOOD.replace("  contents: read\n", "  contents: read\n  pull-requests: write\n")}), "t.yml:5", "pull-requests: write", "ALLOW_WRITE")

    def test_a_listed_write_in_another_workflow_is_not_listed_for_this_one(self) -> None:
        text = GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n")
        only(run({"other.yml": text}), "other.yml:5", "issues: write")

    def test_a_listed_permission_with_another_name_is_not_covered(self) -> None:
        text = GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n  packages: write\n")
        with unittest_allow({("t.yml", "issues"): "test"}):
            only(run({"t.yml": text}), "packages: write")

    def test_a_job_level_write_that_is_not_listed(self) -> None:
        text = GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions:\n      actions: write\n")
        only(run({"t.yml": text}), "actions: write")

    def test_an_unknown_value(self) -> None:
        only(run({"t.yml": GOOD.replace("  contents: read\n", "  contents: read\n  packages: admin\n")}), "packages: admin", "not read, write or none")

    def test_id_token_in_any_form(self) -> None:
        for value in ("write", "read"):
            with self.subTest(value=value):
                found = run({"t.yml": GOOD.replace("  contents: read\n", f"  contents: read\n  id-token: {value}\n")})
                self.assertTrue(any("`id-token` is not allowed" in item for item in found), found)

    def test_pull_request_target(self) -> None:
        for trigger in ("on: pull_request_target\n", "on:\n  pull_request_target:\n    branches: [main]\n", "on: [push, pull_request_target]\n"):
            with self.subTest(trigger=trigger):
                found = run({"t.yml": GOOD.replace("on: push\n", trigger)})
                self.assertTrue(any("pull_request_target" in item for item in found), found)

    def test_write_all_on_a_job(self) -> None:
        text = GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions: write-all\n")
        found = run({"t.yml": text})
        self.assertTrue(any("write-all" in item for item in found), found)

    def test_each_bad_workflow_is_named(self) -> None:
        bad = GOOD.replace("permissions:\n  contents: read\n", "")
        found = run({"a.yml": bad, "b.yml": GOOD, "c.yml": bad})
        self.assertEqual([item.split(":")[0] for item in found], ["a.yml", "c.yml"])

    def test_an_allow_entry_that_is_no_longer_used_is_refused(self) -> None:
        with unittest_allow({("t.yml", "issues"): "test"}):
            only(run({"t.yml": GOOD}), "t.yml", "ALLOW_WRITE", "remove the entry")

    def test_a_file_that_cannot_be_decoded(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "t.yml").write_bytes(b"\xff\xfe\x00bad")
            only(check.violations(Path(tmp)), "cannot be read")


class unittest_allow:
    """Swap ALLOW_WRITE for one test and put it back."""

    def __init__(self, allow: dict[tuple[str, str], str]) -> None:
        self.allow = allow

    def __enter__(self) -> None:
        self.saved = dict(check.ALLOW_WRITE)
        check.ALLOW_WRITE.clear()
        check.ALLOW_WRITE.update(self.allow)

    def __exit__(self, *_: object) -> None:
        check.ALLOW_WRITE.clear()
        check.ALLOW_WRITE.update(self.saved)


class QuotedAndUnsupportedSyntax(unittest.TestCase):
    """YAML allows quoted keys, and a block this reader cannot parse must not be skipped (Codex P2 on noema#629): quoted spellings are read, and an unrecognised line inside a
    permissions block is refused (fail closed)."""

    def test_a_quoted_write_is_the_same_as_a_bare_one(self) -> None:
        for mark in ('"', "'"):
            with self.subTest(mark=mark):
                text = GOOD.replace("  contents: read" + chr(10), "  contents: read" + chr(10) + f"  {mark}packages{mark}: write" + chr(10))
                only(run({"t.yml": text}), "packages: write", "ALLOW_WRITE")

    def test_a_quoted_read_passes(self) -> None:
        text = GOOD.replace("  contents: read" + chr(10), '  "contents": read' + chr(10) + "  'pull-requests': read" + chr(10))
        self.assertEqual(run({"t.yml": text}), [])

    def test_a_quoted_top_level_block_is_found(self) -> None:
        self.assertEqual(run({"t.yml": GOOD.replace("permissions:", '"permissions":')}), [])

    def test_a_quoted_job_level_block_is_read(self) -> None:
        text = GOOD.replace("    runs-on: ubuntu-24.04" + chr(10), "    runs-on: ubuntu-24.04" + chr(10) + '    "permissions":' + chr(10) + '      "actions": write' + chr(10))
        only(run({"t.yml": text}), "actions: write")

    def test_a_quoted_top_level_block_with_contents_write_is_refused(self) -> None:
        found = run({"t.yml": GOOD.replace("permissions:", '"permissions":').replace("contents: read", '"contents": write')})
        self.assertTrue(any("top-level `contents` must be read or none" in item for item in found), found)

    def test_a_line_the_check_does_not_understand_is_refused(self) -> None:
        for line in ("  <<: *wide", "  - contents", "  {packages: write}", "  ? packages"):
            with self.subTest(line=line):
                text = GOOD.replace("  contents: read" + chr(10), "  contents: read" + chr(10) + line + chr(10))
                found = run({"t.yml": text})
                self.assertEqual(len(found), 1, found)
                self.assertIn("does not understand", found[0])

    def test_a_flow_mapping_on_the_key_line_is_still_refused(self) -> None:
        found = run({"t.yml": GOOD.replace("permissions:" + chr(10) + "  contents: read" + chr(10), "permissions: {packages: write}" + chr(10))})
        self.assertTrue(any("must be a mapping" in item for item in found), found)


class LayoutsTheBlockReaderDoesNotParse(unittest.TestCase):
    """A permissions key in any layout the block reader does not parse is refused, never skipped (Codex P2 on noema#629)."""

    def refused(self, text: str) -> None:
        found = run({"t.yml": text})
        self.assertTrue(any("does not read" in item for item in found), found)

    def test_a_space_before_the_colon_at_job_level(self) -> None:
        self.refused(GOOD.replace("    runs-on: ubuntu-24.04" + chr(10), "    runs-on: ubuntu-24.04" + chr(10) + "    permissions : " + chr(10) + "      contents: write" + chr(10)))

    def test_an_inline_job_mapping(self) -> None:
        text = GOOD.split("jobs:")[0] + "jobs:" + chr(10) + "  one: {runs-on: ubuntu-24.04, permissions: {contents: write}, steps: [{run: echo}]}" + chr(10)
        self.refused(text)

    def test_a_json_style_job(self) -> None:
        text = GOOD.split("jobs:")[0] + "jobs:" + chr(10) + '  "one": {"permissions": {"contents": "write"}}' + chr(10)
        self.refused(text)

    def test_a_flow_mapping_at_the_top_of_the_file(self) -> None:
        self.refused("{name: t, permissions: {contents: write}, jobs: {}}" + chr(10))

    def test_a_top_level_block_with_a_space_before_the_colon_does_not_count_as_the_block(self) -> None:
        found = run({"t.yml": GOOD.replace("permissions:", "permissions :")})
        self.assertTrue(any("does not read" in item for item in found), found)
        self.assertTrue(any("no top-level" in item for item in found), found)

    def test_a_comment_or_another_word_is_not_a_permissions_key(self) -> None:
        text = GOOD + "      - run: echo needs permissions" + chr(10) + "      # permissions : write" + chr(10) + "      - name: no-permissions-here" + chr(10) + "        run: echo ok" + chr(10)
        self.assertEqual(run({"t.yml": text}), [])

    def test_a_quoted_hash_before_the_permissions_key_does_not_hide_it(self) -> None:
        for quote in ("'", '"'):
            with self.subTest(quote=quote):
                text = GOOD.split("jobs:")[0] + "jobs:" + chr(10) + "  one: {name: " + quote + "# x" + quote + ", permissions: {contents: write}}" + chr(10)
                self.refused(text)

    def test_a_quoted_hash_in_a_permissions_value_line_does_not_hide_a_write(self) -> None:
        text = GOOD.replace("  contents: read" + chr(10), "  contents: read" + chr(10) + "  packages: write # '# reviewed'" + chr(10))
        found = run({"t.yml": text})
        self.assertTrue(any("packages: write" in item for item in found), found)

    def test_a_hash_inside_a_word_is_not_a_comment(self) -> None:
        text = GOOD.split("jobs:")[0] + "jobs:" + chr(10) + "  one: {name: a#b, permissions: {contents: write}}" + chr(10)
        self.refused(text)


class ThisRepository(unittest.TestCase):
    workflows = Path(__file__).resolve().parents[2] / ".github" / "workflows"

    def test_every_workflow_of_this_repository_passes(self) -> None:
        self.assertGreaterEqual(len(list(self.workflows.glob("*.yml"))), 20)
        self.assertEqual(check.violations(self.workflows), [])

    def test_the_reviewed_writes_are_exactly_the_two_issue_filers(self) -> None:
        self.assertEqual(sorted(check.ALLOW_WRITE), [("bench-nightly.yml", "issues"), ("main-merge-audit.yml", "issues")])


if __name__ == "__main__":
    unittest.main()
