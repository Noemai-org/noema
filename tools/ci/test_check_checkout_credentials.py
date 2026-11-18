#!/usr/bin/env python3
"""The checkout-credentials check fails for the right reason and passes for the right shape (PLT-05, REQ-SEC-005, REQ-SEC-007; A283, OD-P-95 D1).

The files are parsed, so each case is a document, not a layout: every refusal changes exactly one thing from a passing document, so it cannot pass because the check refuses everything.
The same document in the other layouts (flow mapping, quoted or escaped keys, an anchor, a quoted `#`) must get the same answer. The last class runs the check on this repository's own `.github/`.

Run: python3 -I tools/ci/test_check_checkout_credentials.py
"""
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_checkout_credentials as check  # noqa: E402

GOOD = """name: t
on: push
jobs:
  one:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@0000000000000000000000000000000000000000
        with:
          persist-credentials: false
      - run: echo hello
"""
USES = "actions/checkout@0000000000000000000000000000000000000000"


def run(files: dict[str, str]) -> list[str]:
    with tempfile.TemporaryDirectory() as tmp:
        for name, text in files.items():
            path = Path(tmp) / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text, encoding="utf-8")
        return check.violations(Path(tmp))


def wf(steps: str) -> str:
    return "name: t\non: push\njobs:\n  one:\n    runs-on: ubuntu-24.04\n    steps:\n" + steps


def only(found: list[str], *needles: str) -> None:
    """Exactly one problem was found and it mentions every needle (explicit raises: the check must not depend on `assert` surviving `python -O`)."""
    if len(found) != 1:
        raise AssertionError(f"want exactly one problem, got {found}")
    for needle in needles:
        if needle not in found[0]:
            raise AssertionError(f"{needle!r} not in {found[0]!r}")


class Passes(unittest.TestCase):
    def test_the_good_shape(self) -> None:
        self.assertEqual(run({"workflows/t.yml": GOOD}), [])

    def test_the_value_may_be_a_quoted_false_and_the_key_may_be_quoted(self) -> None:
        for text in (GOOD.replace("persist-credentials: false", "persist-credentials: 'false'"), GOOD.replace("persist-credentials: false", '"persist-credentials": false'),
                     GOOD.replace("persist-credentials: false", "persist-credentials: False"), GOOD.replace("persist-credentials: false", "Persist-Credentials: false"), GOOD.replace("with:", "With:")):
            with self.subTest(text[-60:]):
                self.assertEqual(run({"workflows/t.yml": text}), [])

    def test_a_flow_mapping_step_that_sets_it_passes(self) -> None:
        self.assertEqual(run({"workflows/t.yml": wf(f"      - {{uses: {USES}, with: {{persist-credentials: false}}}}\n")}), [])

    def test_a_json_style_step_that_sets_it_passes(self) -> None:
        self.assertEqual(run({"workflows/t.yml": wf(f'      - {{"uses": "{USES}", "with": {{"persist-credentials": false}}}}\n')}), [])

    def test_an_escaped_key_that_spells_the_same_key_passes(self) -> None:
        self.assertEqual(run({"workflows/t.yml": GOOD.replace("persist-credentials:", '"persist\\x2dcredentials":')}), [])

    def test_a_quoted_hash_in_a_name_does_not_matter(self) -> None:
        self.assertEqual(run({"workflows/t.yml": wf(f"      - {{name: '# label', uses: {USES}, with: {{persist-credentials: false}}}}\n")}), [])
        self.assertEqual(run({"workflows/t.yml": GOOD.replace("      - uses:", "      - name: 'a # b'\n        uses:")}), [])

    def test_an_anchor_that_carries_the_with_mapping_passes(self) -> None:
        text = wf(f"      - uses: {USES}\n        with: &safe\n          persist-credentials: false\n      - uses: {USES}\n        with: *safe\n")
        self.assertEqual(run({"workflows/t.yml": text}), [])

    def test_a_workflow_with_no_checkout_a_comment_about_it_and_an_empty_file(self) -> None:
        self.assertEqual(run({"workflows/t.yml": wf("      - run: echo actions/checkout@v4\n      # uses: actions/checkout@v4\n")}), [])
        self.assertEqual(run({"workflows/empty.yml": ""}), [])

    def test_another_action_that_mentions_the_name_is_not_a_checkout(self) -> None:
        self.assertEqual(run({"workflows/t.yml": wf("      - uses: actions/setup-go@0000000000000000000000000000000000000000\n")}), [])

    def test_a_composite_action_is_read_too(self) -> None:
        action = f"name: a\nruns:\n  using: composite\n  steps:\n    - uses: {USES}\n      with:\n        persist-credentials: false\n"
        self.assertEqual(run({"actions/a/action.yml": action}), [])

    def test_a_file_that_is_not_yaml_by_name_is_ignored(self) -> None:
        self.assertEqual(run({"CODEOWNERS": f"uses: {USES}\n", "workflows/README.md": f"- uses: {USES}\n"}), [])


class Refuses(unittest.TestCase):
    def test_no_with_mapping(self) -> None:
        only(run({"workflows/t.yml": wf(f"      - uses: {USES}\n")}), "workflows/t.yml", "jobs.one.steps[0]", "persist-credentials")

    def test_a_with_mapping_without_the_key(self) -> None:
        only(run({"workflows/t.yml": GOOD.replace("          persist-credentials: false\n", "          fetch-depth: 0\n")}), "jobs.one.steps[0]")

    def test_each_value_that_is_not_false(self) -> None:
        for value in ("true", "'true'", "\"${{ inputs.keep }}\"", "''", "~", "no", "off", "0", "FaLsE", "[false]", "{a: false}", "'False'", "' false'"):
            with self.subTest(value):
                only(run({"workflows/t.yml": GOOD.replace("persist-credentials: false", f"persist-credentials: {value}")}), "jobs.one.steps[0]")

    def test_the_key_in_the_wrong_place_does_not_count(self) -> None:
        for text in (wf(f"      - uses: {USES}\n        persist-credentials: false\n"), wf(f"      - uses: {USES}\n        env:\n          persist-credentials: false\n"),
                     wf(f"      - uses: {USES}\n        with:\n          nested:\n            persist-credentials: false\n")):
            with self.subTest(text[-70:]):
                only(run({"workflows/t.yml": text}), "jobs.one.steps[0]")

    def test_the_key_of_another_step_does_not_count(self) -> None:
        text = wf(f"      - uses: {USES}\n        with:\n          persist-credentials: false\n      - uses: {USES}\n")
        only(run({"workflows/t.yml": text}), "jobs.one.steps[1]")

    def test_a_comment_that_names_the_key_does_not_count(self) -> None:
        only(run({"workflows/t.yml": GOOD.replace("          persist-credentials: false\n", "          # persist-credentials: false\n          fetch-depth: 0\n")}), "jobs.one.steps[0]")
        only(run({"workflows/t.yml": GOOD.replace("          persist-credentials: false\n", '          # "persist-credentials": false\n          fetch-depth: 0\n')}), "jobs.one.steps[0]")

    def test_a_with_that_is_not_a_mapping(self) -> None:
        for value in ("persist-credentials", "[persist-credentials]", "~", "false"):
            with self.subTest(value):
                only(run({"workflows/t.yml": wf(f"      - uses: {USES}\n        with: {value}\n")}), "jobs.one.steps[0]")

    def test_the_other_layouts_of_a_checkout_that_does_not_set_it(self) -> None:
        for label, steps in {
            "flow mapping": f"      - {{uses: {USES}}}\n", "flow mapping with a with that lacks it": f"      - {{uses: {USES}, with: {{fetch-depth: 0}}}}\n",
            "json style": f'      - {{"uses": "{USES}"}}\n', "quoted uses key": f'      - "uses": {USES}\n', "single-quoted uses key": f"      - 'uses': {USES}\n",
            "space before the colon": f"      - uses : {USES}\n", "escaped uses key": f'      - "u\\x73es": {USES}\n', "upper-case key": f"      - Uses: {USES}\n",
            "upper-case action": "      - uses: Actions/Checkout@v4\n", "no version": "      - uses: actions/checkout\n", "a hash after the name": f"      - name: 'x # y'\n        uses: {USES}\n",
        }.items():
            with self.subTest(label):
                only(run({"workflows/t.yml": wf(steps)}), "workflows/t.yml")

    def test_an_escaped_key_that_spells_a_different_key_does_not_count(self) -> None:
        only(run({"workflows/t.yml": GOOD.replace("persist-credentials:", '"persist\\x2dcredential":')}), "jobs.one.steps[0]")

    def test_one_good_checkout_does_not_cover_a_second_bad_one(self) -> None:
        found = run({"workflows/t.yml": GOOD + f"      - uses: {USES}\n"})
        only(found, "jobs.one.steps[2]")

    def test_a_checkout_in_another_job_and_another_file_is_named_with_its_place(self) -> None:
        text = GOOD + f"  two:\n    runs-on: ubuntu-24.04\n    steps:\n      - name: fetch\n        uses: {USES}\n"
        found = run({"workflows/a.yml": GOOD, "workflows/b.yml": text})
        only(found, "workflows/b.yml", "jobs.two.steps[0]", "(fetch)")

    def test_a_checkout_in_a_composite_action_is_checked(self) -> None:
        only(run({"actions/a/action.yml": f"name: a\nruns:\n  using: composite\n  steps:\n    - uses: {USES}\n"}), "actions/a/action.yml", "runs.steps[0]")


class Unreadable(unittest.TestCase):
    def test_a_repeated_key_is_an_error_not_last_wins(self) -> None:
        # A last-key-wins read would see `false` here and pass a document GitHub refuses; the first `with` hides nothing, the second wins: refuse both orders.
        for text in (GOOD.replace("        with:\n          persist-credentials: false\n", "        with:\n          persist-credentials: true\n        with:\n          persist-credentials: false\n"),
                     GOOD.replace("          persist-credentials: false\n", "          persist-credentials: true\n          persist-credentials: false\n")):
            with self.subTest(text[-90:]):
                only(run({"workflows/t.yml": text}), "workflows/t.yml", "repeated")

    def test_invalid_yaml_is_refused(self) -> None:
        only(run({"workflows/t.yml": "jobs: [unclosed\n"}), "workflows/t.yml", "cannot be parsed")

    def test_a_file_that_cannot_be_decoded_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "t.yml").write_bytes(b"\xff\xfe\x00bad")
            only(check.violations(Path(tmp)), "t.yml", "cannot be read")

    def test_a_tag_that_would_build_an_object_is_refused(self) -> None:
        only(run({"workflows/t.yml": GOOD + "x: !!python/object/apply:os.system ['echo hi']\n"}), "cannot be parsed")

    def test_several_documents_are_each_walked(self) -> None:
        found = run({"workflows/t.yml": GOOD + "---\n" + wf(f"      - uses: {USES}\n")})
        only(found, "document 1")

    def test_a_very_deep_document_is_refused(self) -> None:
        deep = "x: " + "[" * 100 + "]" * 100 + "\n"
        only(run({"workflows/t.yml": deep}), "deeper than")

    def test_without_pyyaml_the_check_fails_closed(self) -> None:
        saved = check.yaml
        check.yaml = None
        try:
            only(check.violations(Path(".")), "PyYAML is not installed")
        finally:
            check.yaml = saved


class TheLoader(unittest.TestCase):
    def test_it_is_a_safe_loader_and_reads_booleans_as_yaml_one_two(self) -> None:
        loader = check.make_loader()
        self.assertTrue(issubclass(loader, check.yaml.SafeLoader))
        self.assertEqual(check.documents("a: [true, True, TRUE, false, no, off, yes, on]\n"), [{"a": [True, True, True, False, "no", "off", "yes", "on"]}])
        self.assertEqual(check.documents("on: push\n"), [{"on": "push"}])  # `on` is a string, not True

    def test_the_source_never_uses_an_unsafe_loader(self) -> None:
        for name in ("check_checkout_credentials.py", "check_workflow_permissions.py"):
            source = (Path(__file__).resolve().parent / name).read_text(encoding="utf-8")
            body = "\n".join(line for line in source.splitlines() if not line.lstrip().startswith("#") and '"""' not in line)
            for forbidden in ("yaml.unsafe_load", "FullLoader", "UnsafeLoader", "yaml.load(", "Loader=yaml.Loader"):
                if forbidden == "yaml.load(":
                    continue  # load_all with make_loader() is the one use; checked below
                self.assertNotIn(forbidden, body, f"{name}: {forbidden}")
            for line in body.splitlines():
                if "yaml.load" in line:
                    self.assertIn("Loader=make_loader()", line, f"{name}: {line.strip()}")


class ThisRepository(unittest.TestCase):
    def test_every_workflow_and_action_of_this_repository_passes(self) -> None:
        self.assertEqual(check.violations(Path(__file__).resolve().parents[2] / ".github"), [])


class ThePinnedParser(unittest.TestCase):
    """OD-P-95 D1: PyYAML is hash-pinned for tools/ci, installed in the safeguards job before the checks that need it, and nothing else is in the lock."""

    root = Path(__file__).resolve().parents[2]

    def test_the_lock_holds_pyyaml_only_with_hashes(self) -> None:
        text = (self.root / "tools" / "ci" / "requirements.lock").read_text(encoding="utf-8")
        names = [line.split("==")[0] for line in text.splitlines() if "==" in line and not line.startswith("#")]
        self.assertEqual(names, ["pyyaml"])
        self.assertGreaterEqual(text.count("--hash=sha256:"), 10)
        if os.environ.get("GITHUB_ACTIONS"):  # in CI the installed parser is the pinned one; a developer machine may have another
            self.assertEqual(check.yaml.__version__, "6.0.3")

    def test_the_safeguards_job_installs_it_with_hashes_and_wheels_only_before_the_checks(self) -> None:
        text = (self.root / ".github" / "workflows" / "safeguards.yml").read_text(encoding="utf-8")
        install = text.index("requirements.lock")
        self.assertIn("--require-hashes", text[install - 200:install + 50])
        self.assertIn("--only-binary", text[install - 200:install + 50])
        for later in ("check_workflow_permissions.py", "check_checkout_credentials.py", "run_canaries.py"):
            self.assertLess(install, text.index(later), later)

    def test_no_other_workflow_installs_it(self) -> None:
        for path in sorted((self.root / ".github").rglob("*.y*ml")):
            if path.name != "safeguards.yml":
                self.assertNotIn("tools/ci/requirements.lock", path.read_text(encoding="utf-8"), path.name)


if __name__ == "__main__":
    unittest.main()
