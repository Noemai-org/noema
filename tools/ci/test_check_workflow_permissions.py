#!/usr/bin/env python3
"""The workflow-permissions check fails for the right reason and passes for the right shape (REQ-SEC-007; PLT-02), reading the PARSED workflow (OD-P-95, ruling A283).

Each case writes small workflows into a temporary directory and runs the check on them. Every refusal changes exactly one thing from a passing document, so it cannot pass because the check
refuses everything. The spelling classes that a line scanner kept missing (noema#629: quoted keys, flow mappings, a space before the colon, a quoted `#`, YAML escapes in a key, JSON-style
jobs) are each a case here, and each is judged as what it IS. The last class runs the check on this repository's own workflows.

Run: python3 -I tools/ci/test_check_workflow_permissions.py   (needs PyYAML: pip install --require-hashes -r tools/ci/requirements.lock)
"""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_workflow_permissions as check  # noqa: E402
import yamlload  # noqa: E402

BS = chr(92)
NL = chr(10)
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


def one(text: str, name: str = "t.yml") -> list[str]:
    return run({name: text})


def only(found: list[str], *needles: str) -> None:
    """Exactly one problem was found, and it mentions every needle (explicit raises: the check must not depend on `assert` surviving `python -O`)."""
    if len(found) != 1:
        raise AssertionError(f"want exactly one problem, got {found}")
    for needle in needles:
        if needle not in found[0]:
            raise AssertionError(f"{needle!r} not in {found[0]!r}")


def job_level(block: str) -> str:
    return GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n" + block)


class Passes(unittest.TestCase):
    def test_contents_read_alone(self) -> None:
        self.assertEqual(one(GOOD), [])

    def test_read_permissions_of_other_kinds(self) -> None:
        self.assertEqual(one(GOOD.replace("  contents: read\n", "  contents: read\n  pull-requests: read\n  issues: none\n")), [])

    def test_an_empty_mapping_is_the_strictest_block(self) -> None:
        self.assertEqual(one(GOOD.replace("permissions:\n  contents: read\n", "permissions: {}\n")), [])

    def test_comments_do_not_matter_and_naming_the_forbidden_words_in_one_passes(self) -> None:
        text = GOOD.replace("  contents: read\n", "  contents: read  # never write-all, never id-token, never pull_request_target\n")
        self.assertEqual(one("# permissions: write-all\n" + text), [])

    def test_a_reviewed_write_in_its_own_workflow_passes(self) -> None:
        self.assertEqual(one(GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n"), "bench-nightly.yml"), [])

    def test_a_reviewed_write_at_job_level_passes(self) -> None:
        self.assertEqual(one(job_level("    permissions:\n      issues: write\n"), "main-merge-audit.yml"), [])

    def test_the_run_script_may_mention_the_words(self) -> None:
        text = GOOD.replace("echo hello", "echo write-all id-token pull_request_target permissions")
        self.assertEqual(one(text), [])

    def test_a_quoted_read_and_a_quoted_key_are_the_same_as_the_plain_ones(self) -> None:
        self.assertEqual(one(GOOD.replace("  contents: read\n", "  'contents': 'read'\n")), [])

    def test_a_json_style_workflow_that_is_correct_passes(self) -> None:
        self.assertEqual(one('{"name": "t", "on": "push", "permissions": {"contents": "read"}, "jobs": {"one": {"runs-on": "x", "steps": [{"run": "echo"}]}}}'), [])


class Refuses(unittest.TestCase):
    def test_no_permissions_block(self) -> None:
        only(one(GOOD.replace("permissions:\n  contents: read\n", "")), "no top-level")

    def test_a_block_that_exists_only_at_job_level_is_not_top_level(self) -> None:
        text = GOOD.replace("permissions:\n  contents: read\n", "").replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions:\n      contents: read\n")
        only(one(text), "no top-level")

    def test_a_scalar_block(self) -> None:
        for scalar in ("write-all", "read-all"):
            only(one(GOOD.replace("permissions:\n  contents: read\n", f"permissions: {scalar}\n")), scalar, "must be a mapping")

    def test_an_empty_block(self) -> None:
        only(one(GOOD.replace("permissions:\n  contents: read\n", "permissions:\n")), "must be a mapping")

    def test_top_level_contents_write(self) -> None:
        found = one(GOOD.replace("contents: read", "contents: write"))
        self.assertTrue(any("top-level `contents` must be read or none" in item for item in found), found)

    def test_a_write_that_is_not_listed(self) -> None:
        only(one(GOOD.replace("  contents: read\n", "  contents: read\n  pull-requests: write\n")), "pull-requests: write", "ALLOW_WRITE")

    def test_a_listed_write_in_another_workflow_is_not_listed_for_this_one(self) -> None:
        only(one(GOOD.replace("  contents: read\n", "  contents: read\n  issues: write\n"), "other.yml"), "issues: write", "ALLOW_WRITE")

    def test_a_listed_permission_with_another_name_is_not_covered(self) -> None:
        found = one(GOOD.replace("  contents: read\n", "  contents: read\n  packages: write\n"), "bench-nightly.yml")
        self.assertTrue(any("`packages: write` is not in ALLOW_WRITE" in item for item in found), found)

    def test_a_job_level_write_that_is_not_listed(self) -> None:
        only(one(job_level("    permissions:\n      packages: write\n")), "job `one`", "packages: write")

    def test_an_unknown_value(self) -> None:
        only(one(GOOD.replace("  contents: read\n", "  contents: read\n  packages: admin\n")), "packages", "not read, write or none")

    def test_a_value_that_is_not_a_string(self) -> None:
        for value in ("true", "1", "[read]", "{a: b}", "null"):
            found = one(GOOD.replace("  contents: read\n", f"  contents: read\n  packages: {value}\n"))
            self.assertTrue(any("`packages` is not read, write or none" in item for item in found), (value, found))

    def test_id_token_in_any_form(self) -> None:
        for value in ("write", "read", "none"):
            found = one(GOOD.replace("  contents: read\n", f"  contents: read\n  id-token: {value}\n"))
            self.assertTrue(any("`id-token` is not allowed" in item for item in found), (value, found))
        found = one(job_level("    permissions:\n      id-token: write\n"))
        self.assertTrue(any("`id-token` is not allowed" in item for item in found), found)

    def test_pull_request_target_in_every_form_of_on(self) -> None:
        for on in ("pull_request_target", "[push, pull_request_target]", "{pull_request_target: {types: [opened]}}", "{push: {}, 'pull_request_target': {}}"):
            only(one(GOOD.replace("on: push", f"on: {on}")), "pull_request_target")

    def test_write_all_on_a_job(self) -> None:
        only(one(job_level("    permissions: write-all\n")), "job `one`", "write-all")

    def test_each_bad_workflow_is_named(self) -> None:
        found = run({"good.yml": GOOD, "bad.yml": GOOD.replace("contents: read", "contents: write"), "worse.yml": "name: x\n"})
        self.assertTrue(any(item.startswith("bad.yml") for item in found) and any(item.startswith("worse.yml") for item in found), found)
        self.assertFalse(any(item.startswith("good.yml") for item in found), found)

    def test_an_allow_entry_that_is_no_longer_used_is_refused(self) -> None:
        only(run({"bench-nightly.yml": GOOD}), "bench-nightly.yml", "no longer asks for", "remove the entry")

    def test_a_file_that_cannot_be_decoded(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "t.yml").write_bytes(b"\xff\xfe\x00bad")
            only(check.violations(Path(tmp)), "t.yml", "cannot be read")

    def test_a_workflow_that_is_not_a_mapping(self) -> None:
        for text in ("- a\n- b\n", "just a string\n", ""):
            only(one(text), "not a mapping")


class EverySpellingIsJudgedAsWhatItIs(unittest.TestCase):
    """The classes a line scanner missed one at a time. Each is the SAME document as the plain spelling, so each is refused (or passes) exactly as the plain spelling does."""

    def test_a_quoted_write_is_the_same_as_a_bare_one(self) -> None:
        for spelling in ("  \"packages\": write\n", "  'packages': 'write'\n", "  packages: \"write\"\n"):
            only(one(GOOD.replace("  contents: read\n", "  contents: read\n" + spelling)), "packages: write")

    def test_a_quoted_top_level_key_is_the_top_level_block(self) -> None:
        self.assertEqual(one(GOOD.replace("permissions:", "\"permissions\":")), [])
        found = one(GOOD.replace("permissions:", "'permissions':").replace("contents: read", "contents: write"))
        self.assertTrue(any("top-level `contents`" in item for item in found), found)

    def test_a_quoted_job_level_block_is_read(self) -> None:
        only(one(job_level("    \"permissions\":\n      packages: write\n")), "job `one`", "packages: write")

    def test_a_flow_mapping_is_read(self) -> None:
        only(one(GOOD.replace("permissions:\n  contents: read\n", "permissions: {contents: read, packages: write}\n")), "packages: write")
        only(one(job_level("    permissions: {contents: write}\n")), "job `one`", "contents: write")

    def test_a_space_before_the_colon_is_read(self) -> None:
        only(one(job_level("    permissions :\n      packages: write\n")), "job `one`", "packages: write")
        found = one(GOOD.replace("permissions:", "permissions :").replace("contents: read", "contents: write"))
        self.assertTrue(any("top-level `contents`" in item for item in found), found)

    def test_an_inline_job_mapping_is_read(self) -> None:
        text = GOOD.split("jobs:")[0] + "jobs:" + NL + "  one: {runs-on: x, permissions: {packages: write}, steps: [{run: echo}]}" + NL
        only(one(text), "job `one`", "packages: write")

    def test_a_json_style_job_is_read(self) -> None:
        text = '{"name": "t", "on": "push", "permissions": {"contents": "read"}, "jobs": {"one": {"runs-on": "x", "permissions": {"packages": "write"}, "steps": []}}}'
        only(one(text), "job `one`", "packages: write")

    def test_a_flow_mapping_at_the_top_of_the_file(self) -> None:
        only(one('{"name": "t", "on": "push", "permissions": {"contents": "read", "issues": "write"}, "jobs": {}}'), "issues: write")

    def test_a_quoted_hash_before_the_key_does_not_hide_it(self) -> None:
        text = job_level("    name: '# not a comment'\n    permissions: {packages: write}  # a real comment\n")
        only(one(text), "job `one`", "packages: write")

    def test_a_quoted_hash_in_a_value_does_not_hide_a_write(self) -> None:
        only(one(GOOD.replace("  contents: read\n", "  contents: read\n  packages: 'write' # why\n")), "packages: write")
        only(one(GOOD.replace("  contents: read\n", "  contents: read\n  'x#y': write\n")), "x#y: write")

    def test_a_hash_inside_a_word_is_not_a_comment(self) -> None:
        only(one(GOOD.replace("  contents: read\n", "  contents: read\n  packages: write#not-a-comment\n")), "packages", "not read, write or none")

    def test_a_yaml_escape_in_a_key_is_read_as_the_key(self) -> None:
        escaped_block = "\"permiss" + BS + "u0069ons\":"
        text = GOOD.replace("permissions:", escaped_block)
        self.assertEqual(one(text), [])  # it IS the top-level block, written with an escape
        text = job_level("    \"permiss" + BS + "u0069ons\":\n      packages: write\n")
        only(one(text), "job `one`", "packages: write")
        text = GOOD.replace("  contents: read\n", "  contents: read\n  \"pack" + BS + "x61ges\": write\n")
        only(one(text), "packages: write")

    def test_an_escape_in_a_value_or_a_text_is_not_a_key(self) -> None:
        text = GOOD.replace("      - run: echo hello\n", "      - run: |\n          echo '{" + BS + '"a' + BS + '": 1}' + "'\n")
        self.assertEqual(one(text), [])

    def test_the_bare_on_key_is_found_though_yaml_reads_it_as_true(self) -> None:
        only(one(GOOD.replace("on: push", "on:\n  pull_request_target:\n")), "pull_request_target")


class FilesThatAreNotPlainYaml(unittest.TestCase):
    def test_a_repeated_key_is_refused_whichever_wins(self) -> None:
        text = GOOD.replace("permissions:\n  contents: read\n", "permissions:\n  contents: write\n  contents: read\n")
        only(one(text), "repeats a key")
        only(one(GOOD.replace("permissions:\n  contents: read\n", "permissions:\n  contents: read\npermissions:\n  contents: write\n")), "repeats a key")

    def test_an_anchor_or_an_alias_is_refused(self) -> None:
        only(one(GOOD.replace("permissions:\n  contents: read\n", "permissions: &p\n  contents: read\n")), "anchor or an alias")
        text = GOOD.replace("    runs-on: ubuntu-24.04\n", "    runs-on: ubuntu-24.04\n    permissions: *p\n").replace("permissions:\n  contents: read\n", "permissions: &p\n  contents: read\n")
        found = one(text)
        self.assertEqual(len(found), 1, found)
        self.assertIn("anchor or an alias", found[0])

    def test_a_second_document_is_refused(self) -> None:
        only(one(GOOD + "---\n" + GOOD), "more than one YAML document")

    def test_a_file_that_is_not_yaml(self) -> None:
        only(one("a: [unclosed\n"), "is not plain YAML")

    def test_a_python_tag_is_not_built(self) -> None:
        only(one("!!python/object/apply:os.system ['echo no']\n"), "is not plain YAML")

    def test_a_file_over_the_size_bound(self) -> None:
        only(one(GOOD + "# " + "x" * yamlload.MAX_BYTES + "\n"), "larger than")

    def test_a_missing_parser_refuses_instead_of_passing(self) -> None:
        saved = yamlload.yaml
        yamlload.yaml = None
        try:
            only(one(GOOD), "PyYAML is not installed")
            self.assertFalse(yamlload.available())
        finally:
            yamlload.yaml = saved


class ThisRepository(unittest.TestCase):
    workflows = Path(__file__).resolve().parents[2] / ".github" / "workflows"

    def test_every_workflow_of_this_repository_passes(self) -> None:
        self.assertGreaterEqual(len(list(self.workflows.glob("*.yml"))), 20)
        self.assertEqual(check.violations(self.workflows), [])

    def test_the_reviewed_writes_are_exactly_the_two_issue_filers(self) -> None:
        self.assertEqual(sorted(check.ALLOW_WRITE), [("bench-nightly.yml", "issues"), ("main-merge-audit.yml", "issues")])


if __name__ == "__main__":
    unittest.main()
