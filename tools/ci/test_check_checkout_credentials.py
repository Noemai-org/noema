#!/usr/bin/env python3
"""The checkout-credentials check fails for the right reason and passes for the right shape (PLT-05, REQ-SEC-005, REQ-SEC-007), reading the PARSED workflow (OD-P-95, ruling A283).

Each case writes a small workflow into a temporary `.github/` and runs the check on it. A case that only passes because the check ignores something is not a case: every refusal below
changes exactly one thing from a passing document. The spelling classes a line scanner kept missing (noema#622: a quoted `uses` key, a flow mapping, a space before the colon, a quoted `#`,
a JSON-style step) are each a case, judged as what they ARE. The last test runs the check on this repository's own `.github/`.

Run: python3 -I tools/ci/test_check_checkout_credentials.py   (needs PyYAML: pip install --require-hashes -r tools/ci/requirements.lock)
"""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_checkout_credentials as check  # noqa: E402
import yamlload  # noqa: E402

REF = "actions/checkout@11d5960a326750d5838078e36cf38b85af677262"
GOOD = f"""name: t
on: push
jobs:
  one:
    runs-on: ubuntu-24.04
    steps:
      - uses: {REF} # v4
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


def wf(text: str) -> list[str]:
    return run({"workflows/t.yml": text})


class Passes(unittest.TestCase):
    def test_the_good_document_passes(self) -> None:
        self.assertEqual(wf(GOOD), [])

    def test_the_key_may_follow_and_precede_other_keys_of_the_same_with_block(self) -> None:
        self.assertEqual(wf(GOOD.replace("          persist-credentials: false\n", "          fetch-depth: 0\n          persist-credentials: false\n          ref: main\n")), [])

    def test_a_named_step_and_a_comment_after_the_value_pass(self) -> None:
        self.assertEqual(wf(GOOD.replace("persist-credentials: false", "persist-credentials: false  # nothing later needs the token")), [])

    def test_a_workflow_with_no_checkout_passes(self) -> None:
        self.assertEqual(wf("on: push\njobs:\n  j:\n    steps:\n      - run: echo actions/checkout@v4 is only text\n"), [])

    def test_another_action_with_a_similar_name_is_not_a_checkout(self) -> None:
        self.assertEqual(wf(GOOD.replace("actions/checkout@", "actions/checkout-helper@").replace("persist-credentials: false", "x: y")), [])

    def test_the_string_false_is_the_same_input(self) -> None:
        for spelling in ("'false'", '"false"', "False", "FALSE", "false"):
            self.assertEqual(wf(GOOD.replace("persist-credentials: false", f"persist-credentials: {spelling}")), [], spelling)

    def test_a_quoted_key_and_the_other_case_of_the_input_name_are_the_same_input(self) -> None:
        for key in ('"persist-credentials"', "'persist-credentials'", "Persist-Credentials"):
            self.assertEqual(wf(GOOD.replace("persist-credentials: false", f"{key}: false")), [], key)

    def test_a_composite_action_and_a_json_style_workflow_are_read_too(self) -> None:
        composite = f"name: c\nruns:\n  using: composite\n  steps:\n    - uses: {REF}\n      with:\n        persist-credentials: false\n"
        self.assertEqual(run({"actions/c/action.yml": composite}), [])
        self.assertEqual(wf('{"on": "push", "jobs": {"j": {"steps": [{"uses": "' + REF + '", "with": {"persist-credentials": false}}]}}}'), [])


class Refuses(unittest.TestCase):
    def one(self, text: str, *needles: str) -> None:
        found = wf(text)
        self.assertEqual(len(found), 1, found)
        for needle in needles:
            self.assertIn(needle, found[0])

    def test_no_with_block(self) -> None:
        self.one(GOOD.replace("        with:\n          persist-credentials: false\n", ""), "workflows/t.yml", "jobs.one.steps[0]", "without a `with:` block")

    def test_a_with_block_without_the_key(self) -> None:
        self.one(GOOD.replace("persist-credentials: false", "fetch-depth: 0"), "without `persist-credentials: false`")

    def test_true_an_expression_and_anything_else(self) -> None:
        for value in ("true", "'true'", "${{ inputs.keep }}", "yes", "0", "null", "[false]", "{a: false}", "'false x'"):
            found = wf(GOOD.replace("persist-credentials: false", f"persist-credentials: {value}"))
            self.assertEqual(len(found), 1, (value, found))
            self.assertIn("something other than false", found[0])

    def test_a_comment_that_only_mentions_the_key_does_not_count(self) -> None:
        self.one(GOOD.replace("        with:\n          persist-credentials: false\n", "        with:\n          # persist-credentials: false\n          fetch-depth: 0\n"), "without `persist-credentials: false`")

    def test_the_key_in_another_steps_block_does_not_count(self) -> None:
        text = GOOD.replace("        with:\n          persist-credentials: false\n", "") + "      - uses: actions/setup-python@a26af69be951a213d495a4c3e4e4022e16d87065\n        with:\n          persist-credentials: false\n"
        self.one(text, "jobs.one.steps[0]")

    def test_the_key_in_the_with_block_of_the_step_after_does_not_count(self) -> None:
        text = f"on: push\njobs:\n  j:\n    steps:\n      - uses: {REF}\n      - run: echo\n        with:\n          persist-credentials: false\n"
        self.one(text, "jobs.j.steps[0]")

    def test_a_padded_input_name_is_a_different_input_and_does_not_count(self) -> None:
        # Codex P2 on noema#729: the runner does not trim input names, so the real `persist-credentials` keeps checkout's default of true.
        for key in ('" persist-credentials"', '"persist-credentials "', '"\\tpersist-credentials"', '" persist-credentials "'):
            found = wf(GOOD.replace("persist-credentials: false", f"{key}: false"))
            self.assertEqual(len(found), 1, (key, found))
            self.assertIn("without `persist-credentials: false`", found[0])

    def test_a_padded_value_is_not_the_boolean_false(self) -> None:
        for value in ("' false'", "'false '", "'false\\n'"):
            found = wf(GOOD.replace("persist-credentials: false", f"persist-credentials: {value}"))
            self.assertEqual(len(found), 1, (value, found))
            self.assertIn("something other than false", found[0])

    def test_a_padded_key_beside_the_real_one_leaves_the_real_one_in_charge(self) -> None:
        self.one(GOOD.replace("persist-credentials: false", "' persist-credentials': false\n          persist-credentials: true"), "something other than false")
        self.assertEqual(wf(GOOD.replace("persist-credentials: false", "' persist-credentials': true\n          persist-credentials: false")), [])

    def test_a_key_that_is_not_a_string_is_not_the_input(self) -> None:
        self.one(GOOD.replace("persist-credentials: false", "1: false"), "without `persist-credentials: false`")

    def test_set_twice_with_one_of_them_true(self) -> None:
        self.one(GOOD.replace("persist-credentials: false", "persist-credentials: false\n          Persist-Credentials: true"), "something other than false")

    def test_an_environment_variable_that_overrides_the_input(self) -> None:
        self.one(GOOD.replace("        with:\n", "        env:\n          INPUT_PERSIST-CREDENTIALS: true\n        with:\n"), "INPUT_PERSIST-CREDENTIALS")

    def test_each_of_two_checkouts_is_judged_on_its_own(self) -> None:
        text = GOOD + f"      - uses: {REF}\n        with:\n          fetch-depth: 0\n"
        self.one(text, "jobs.one.steps[2]")

    def test_the_step_is_named_when_it_has_a_name(self) -> None:
        self.one(GOOD.replace(f"      - uses: {REF} # v4\n", f"      - name: Check out\n        uses: {REF}\n").replace("persist-credentials: false", "fetch-depth: 0"), "(Check out)")

    def test_a_composite_action_is_checked_too(self) -> None:
        composite = f"name: c\nruns:\n  using: composite\n  steps:\n    - uses: {REF}\n"
        found = run({"actions/c/action.yml": composite})
        self.assertEqual(len(found), 1, found)
        self.assertIn("actions/c/action.yml", found[0])

    def test_the_action_name_in_any_case_and_with_padding(self) -> None:
        for uses in ("Actions/Checkout@v4", "  actions/checkout@v4", "actions/checkout"):
            text = f'on: push\njobs:\n  j:\n    steps:\n      - uses: "{uses}"\n'
            self.assertEqual(len(wf(text)), 1, uses)


class EverySpellingIsJudgedAsWhatItIs(unittest.TestCase):
    """The classes a line scanner missed one at a time: each is the same document as the plain spelling, so each is refused exactly as the plain spelling is."""

    def refused(self, text: str) -> None:
        found = wf(text)
        self.assertEqual(len(found), 1, found)
        self.assertIn("actions/checkout", found[0])

    def test_a_quoted_uses_key(self) -> None:
        self.refused(f'on: push\njobs:\n  j:\n    steps:\n      - "uses": {REF}\n')
        self.refused(f"on: push\njobs:\n  j:\n    steps:\n      - 'uses': {REF}\n")

    def test_a_space_before_the_colon(self) -> None:
        self.refused(f"on: push\njobs:\n  j:\n    steps:\n      - uses : {REF}\n")

    def test_a_flow_mapping_step(self) -> None:
        self.refused(f"on: push\njobs:\n  j:\n    steps:\n      - {{uses: {REF}}}\n")
        self.refused(f"on: push\njobs:\n  j:\n    steps:\n      - {{name: x, uses: {REF}, with: {{fetch-depth: 0}}}}\n")

    def test_a_flow_mapping_step_that_sets_it_is_fine(self) -> None:
        self.assertEqual(wf(f"on: push\njobs:\n  j:\n    steps:\n      - {{uses: {REF}, with: {{persist-credentials: false}}}}\n"), [])

    def test_a_json_style_workflow(self) -> None:
        self.refused('{"on": "push", "jobs": {"j": {"steps": [{"uses": "' + REF + '"}]}}}')

    def test_a_flow_sequence_of_steps(self) -> None:
        self.refused(f"on: push\njobs:\n  j:\n    steps: [{{uses: {REF}}}, {{run: echo}}]\n")

    def test_a_quoted_hash_before_the_key_does_not_hide_a_missing_key(self) -> None:
        self.refused(f"on: push\njobs:\n  j:\n    steps:\n      - name: '# label'\n        uses: {REF}\n")
        self.refused(f"on: push\njobs:\n  j:\n    steps:\n      - {{name: '# a', uses: {REF}}} # and a comment\n")

    def test_a_quoted_hash_in_the_value_line_does_not_make_it_false(self) -> None:
        self.refused(GOOD.replace("persist-credentials: false", "persist-credentials: '#false'"))

    def test_a_yaml_escape_in_the_key_is_the_key(self) -> None:
        bs = chr(92)
        self.refused('on: push\njobs:\n  j:\n    steps:\n      - "us' + bs + "x65s\": " + REF + "\n")
        self.assertEqual(wf(GOOD.replace("persist-credentials:", '"persist-cred' + bs + 'u0065ntials":')), [])

    def test_a_block_scalar_that_only_mentions_checkout_is_text(self) -> None:
        self.assertEqual(wf(f"on: push\njobs:\n  j:\n    steps:\n      - run: |\n          uses: {REF}\n"), [])


class FilesThatAreNotPlainYaml(unittest.TestCase):
    def one(self, text: str, needle: str) -> None:
        found = wf(text)
        self.assertEqual(len(found), 1, found)
        self.assertIn(needle, found[0])

    def test_a_repeated_key_is_refused_whichever_wins(self) -> None:
        self.one(GOOD.replace("persist-credentials: false", "persist-credentials: false\n          persist-credentials: false"), "repeats a key")
        self.one(GOOD.replace("persist-credentials: false", "persist-credentials: true\n          persist-credentials: false"), "repeats a key")

    def test_an_anchor_an_alias_and_a_second_document(self) -> None:
        self.one(GOOD.replace("with:\n", "with: &w\n"), "anchor or an alias")
        self.one(GOOD + "---\n" + GOOD, "more than one YAML document")

    def test_a_file_that_is_not_yaml_and_a_python_tag(self) -> None:
        self.one("a: [unclosed\n", "is not plain YAML")
        self.one("!!python/object/apply:os.system ['echo no']\n", "is not plain YAML")

    def test_a_file_that_cannot_be_decoded(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / ".github"
            (root / "workflows").mkdir(parents=True)
            (root / "workflows" / "t.yml").write_bytes(b"\xff\xfe\x00bad")
            found = check.violations(root)
            self.assertEqual(len(found), 1, found)
            self.assertIn("cannot be read", found[0])

    def test_a_missing_parser_refuses_instead_of_passing(self) -> None:
        saved = yamlload.yaml
        yamlload.yaml = None
        try:
            self.one(GOOD, "PyYAML is not installed")
        finally:
            yamlload.yaml = saved

    def test_a_file_that_is_not_a_workflow_extension_is_not_read(self) -> None:
        self.assertEqual(run({"workflows/notes.txt": "a: [unclosed\n"}), [])


class ThisRepository(unittest.TestCase):
    def test_every_checkout_of_this_repository_sets_it(self) -> None:
        github = Path(__file__).resolve().parents[2] / ".github"
        self.assertTrue((github / "workflows").is_dir())
        self.assertGreaterEqual(sum(p.read_text(encoding="utf-8").count("actions/checkout@") for p in (github / "workflows").glob("*.yml")), 20)
        self.assertEqual(check.violations(github), [])


if __name__ == "__main__":
    unittest.main()
