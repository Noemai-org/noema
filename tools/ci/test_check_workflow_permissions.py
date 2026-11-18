#!/usr/bin/env python3
"""The workflow-permissions check fails for the right reason and passes for the right shape (REQ-SEC-007; PLT-02; A283, OD-P-95 D1).

The files are parsed, so each case is a document, not a layout: every refusal changes exactly one thing from a passing document, so it cannot pass because the check refuses everything.
The same grant in the other layouts (flow mapping, quoted or escaped keys, a space before the colon, an anchor, a quoted `#`) must get the same answer. The last class runs the check on this
repository's own workflows.

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
    """Exactly one problem was found and it mentions every needle (explicit raises: the check must not depend on `assert` surviving `python -O`)."""
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

    def test_the_same_document_in_other_layouts_passes(self) -> None:
        for label, text in {
            "flow mapping": GOOD.replace("permissions:\n  contents: read\n", "permissions: {contents: read}\n"), "space before the colon": GOOD.replace("permissions:", "permissions :"),
            "quoted keys": GOOD.replace("permissions:\n  contents: read\n", '"permissions":\n  "contents": read\n'), "quoted value": GOOD.replace("contents: read", "contents: 'read'"),
            "json style": '{"name": "t", "on": "push", "permissions": {"contents": "read"}, "jobs": {"one": {"runs-on": "x", "steps": [{"run": "echo"}]}}}\n',
            "comment after the value": GOOD.replace("contents: read", "contents: read # least privilege"),
            "anchor": GOOD.replace("permissions:\n  contents: read\n", "x-ro: &ro\n  contents: read\npermissions: *ro\n"),
            "a comment that names the forbidden words": GOOD + "# write-all, pull_request_target and id-token are not used\n",
            "a script that names them": GOOD.replace("echo hello", "echo pull_request_target write-all id-token"),
        }.items():
            with self.subTest(label):
                self.assertEqual(run({"t.yml": text}), [], label)

    def test_a_reviewed_write_in_its_own_workflow_passes(self) -> None:
        text = GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n")
        self.assertEqual(run({"bench-nightly.yml": text, "main-merge-audit.yml": text}), [])

    def test_a_reviewed_write_at_job_level_passes(self) -> None:
        text = GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions:\n      issues: write\n")
        self.assertEqual(run({"bench-nightly.yml": text.replace("permissions:\n  contents: read\n", "permissions:\n  contents: read\n  issues: read\n"), "main-merge-audit.yml": text}), [])

    def test_a_trigger_that_only_resembles_the_forbidden_one_passes(self) -> None:
        self.assertEqual(run({"t.yml": GOOD.replace("on: push", "on:\n  pull_request:\n  workflow_dispatch:\n")}), [])


class Refuses(unittest.TestCase):
    def test_no_permissions_block(self) -> None:
        only(run({"t.yml": GOOD.replace("permissions:\n  contents: read\n", "")}), "t.yml", "no top-level")

    def test_a_block_that_exists_only_at_job_level_is_not_top_level(self) -> None:
        text = GOOD.replace("permissions:\n  contents: read\n", "").replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions:\n      contents: read\n")
        only(run({"t.yml": text}), "no top-level")

    def test_a_scalar_block(self) -> None:
        for scalar in ("write-all", "read-all", "read", "'write-all'", "~", "[contents]"):
            with self.subTest(scalar):
                only(run({"t.yml": GOOD.replace("permissions:\n  contents: read\n", f"permissions: {scalar}\n")}), "must be a mapping")

    def test_a_scalar_block_at_job_level(self) -> None:
        text = GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions: write-all\n")
        only(run({"t.yml": text}), "job `one`", "must be a mapping")

    def test_top_level_contents_write_and_an_unlisted_write(self) -> None:
        found = run({"t.yml": GOOD.replace("contents: read", "contents: write")})
        self.assertEqual(len(found), 2, found)
        self.assertTrue(any("must be read or none" in f for f in found) and any("ALLOW_WRITE" in f for f in found))

    def test_a_write_that_is_not_listed(self) -> None:
        only(run({"t.yml": GOOD.replace("  contents: read\n", "  contents: read\n  packages: write\n")}), "`packages: write`", "ALLOW_WRITE")

    def test_a_listed_write_in_another_workflow_is_not_listed_for_this_one(self) -> None:
        only(run({"other.yml": GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n")}), "other.yml", "ALLOW_WRITE")

    def test_a_listed_permission_with_another_name_is_not_covered(self) -> None:
        only(run({"bench-nightly.yml": GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n  pull-requests: write\n"),
                  "main-merge-audit.yml": GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n")}), "pull-requests: write")

    def test_a_job_level_write_that_is_not_listed(self) -> None:
        only(run({"t.yml": GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions:\n      contents: write\n")}), "job `one`", "ALLOW_WRITE")

    def test_an_unknown_value(self) -> None:
        for value in ("admin", "true", "READ", "'write '", "1"):
            with self.subTest(value):
                only(run({"t.yml": GOOD.replace("contents: read", f"contents: {value}")}), "is not read, write or none")

    def test_id_token_in_any_form(self) -> None:
        for line in ("id-token: write", "id-token: read", "id-token: none", "ID-Token: write", '"id-token": write'):
            with self.subTest(line):
                only(run({"t.yml": GOOD.replace("  contents: read\n", f"  contents: read\n  {line}\n")}), "id-token")

    def test_pull_request_target_in_each_shape_of_on(self) -> None:
        for trigger in ("on: pull_request_target", "on: [push, pull_request_target]", "on:\n  pull_request_target:\n    types: [opened]", "'on': pull_request_target", '"on":\n  pull_request_target: {}',
                        "on: {pull_request_target: {}}", "on: Pull_Request_Target"):
            with self.subTest(trigger):
                only(run({"t.yml": GOOD.replace("on: push", trigger)}), "pull_request_target")

    def test_the_grant_hidden_in_another_layout_is_still_read(self) -> None:
        for label, text in {
            "flow mapping": GOOD.replace("permissions:\n  contents: read\n", "permissions: {contents: read, packages: write}\n"),
            "space before the colon": GOOD.replace("  contents: read\n", "  contents: read\n  packages : write\n"),
            "quoted key and value": GOOD.replace("  contents: read\n", '  contents: read\n  "packages": "write"\n'),
            "json style": '{"name": "t", "on": "push", "permissions": {"contents": "read", "packages": "write"}, "jobs": {"one": {"runs-on": "x"}}}\n',
            "inline job": GOOD.replace("  one:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: echo hello\n", "  one: {runs-on: x, permissions: {packages: write}}\n"),
            "anchor and merge key": GOOD.replace("permissions:\n  contents: read\n", "x-w: &w\n  packages: write\npermissions:\n  <<: *w\n  contents: read\n"),
            "escaped key": GOOD.replace("  contents: read\n", '  contents: read\n  "pack\\x61ges": write\n'),
            "a quoted hash before the key": GOOD.replace("  contents: read\n", "  contents: read\n  x: '# not a comment'\n  packages: write\n"),
            "a hash after a value": GOOD.replace("  contents: read\n", "  contents: read\n  packages: write # a real comment\n"),
            "a hash inside a word": GOOD.replace("  contents: read\n", "  contents: read\n  packages: write#notacomment\n"),
        }.items():
            with self.subTest(label):
                found = run({"t.yml": text})
                self.assertTrue(found, label)
                self.assertTrue(any("packages" in f or "write#notacomment" in f for f in found), found)

    def test_each_bad_workflow_is_named(self) -> None:
        found = run({"a.yml": GOOD, "b.yml": GOOD.replace("contents: read", "contents: write"), "c.yml": GOOD.replace("permissions:\n  contents: read\n", "")})
        self.assertTrue(any(f.startswith("b.yml") for f in found) and any(f.startswith("c.yml") for f in found))
        self.assertFalse(any(f.startswith("a.yml") for f in found))

    def test_an_allow_entry_that_is_no_longer_used_is_refused(self) -> None:
        only(run({"bench-nightly.yml": GOOD, "main-merge-audit.yml": GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n")}), "bench-nightly.yml", "no longer asks")


class Unreadable(unittest.TestCase):
    def test_a_repeated_key_is_an_error_not_last_wins(self) -> None:
        # Last-key-wins would read the second block and pass a document GitHub refuses; either order is refused.
        for text in (GOOD.replace("permissions:\n  contents: read\n", "permissions:\n  contents: write\npermissions:\n  contents: read\n"),
                     GOOD.replace("permissions:\n  contents: read\n", "permissions:\n  contents: read\n  contents: write\n")):
            with self.subTest(text[:80]):
                only(run({"t.yml": text}), "t.yml", "repeated")

    def test_two_permissions_keys_that_differ_only_in_case_are_not_one_block(self) -> None:
        found = run({"t.yml": GOOD.replace("permissions:\n  contents: read\n", "permissions:\n  contents: read\nPermissions:\n  contents: write\n")})
        self.assertTrue(any("no top-level" in f for f in found), found)
        self.assertTrue(any("`contents: write`" in f for f in found), found)

    def test_invalid_yaml_and_several_documents_and_a_non_mapping(self) -> None:
        only(run({"t.yml": "jobs: [unclosed\n"}), "cannot be parsed")
        only(run({"t.yml": GOOD + "---\n" + GOOD}), "exactly one YAML mapping")
        only(run({"t.yml": "- a\n- b\n"}), "exactly one YAML mapping")
        only(run({"t.yml": ""}), "exactly one YAML mapping")

    def test_a_file_that_cannot_be_decoded(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "t.yml").write_bytes(b"\xff\xfe\x00bad")
            only(check.violations(Path(tmp)), "t.yml", "cannot be read")

    def test_a_tag_that_would_build_an_object_is_refused(self) -> None:
        only(run({"t.yml": GOOD + "x: !!python/object/apply:os.system ['echo hi']\n"}), "cannot be parsed")

    def test_without_pyyaml_the_check_fails_closed(self) -> None:
        saved = check.yaml
        check.yaml = None
        try:
            only(check.violations(Path(".")), "PyYAML is not installed")
        finally:
            check.yaml = saved


class TheLoader(unittest.TestCase):
    def test_booleans_are_read_as_yaml_one_two_and_the_loader_is_safe(self) -> None:
        self.assertTrue(issubclass(check.make_loader(), check.yaml.SafeLoader))
        self.assertEqual(check.documents("on: push\na: [no, off, true, False]\n"), [{"on": "push", "a": ["no", "off", True, False]}])


class ThisRepository(unittest.TestCase):
    def test_every_workflow_of_this_repository_passes(self) -> None:
        self.assertEqual(check.violations(Path(__file__).resolve().parents[2] / ".github" / "workflows"), [])

    def test_the_reviewed_writes_are_exactly_the_two_issue_filers(self) -> None:
        self.assertEqual(sorted(check.ALLOW_WRITE), [("bench-nightly.yml", "issues"), ("main-merge-audit.yml", "issues")])


if __name__ == "__main__":
    unittest.main()
