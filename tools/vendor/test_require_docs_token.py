"""The drift guard fails with one fixed line when DOCS_READ_TOKEN is missing, and reads the token in one place (GOV-22).

Run: python3 -I -m unittest discover -s tools/vendor -p 'test_*.py'
"""
import os
import re
import subprocess
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
SCRIPT = HERE / "require_docs_token.py"
WORKFLOW = ROOT / ".github/workflows/docs-drift.yml"
LINE = "DOCS_READ_TOKEN is not set. See docs/vendors/README.md."


def run(token):
    env = {k: v for k, v in os.environ.items() if k != "DOCS_READ_TOKEN"}
    if token is not None:
        env["DOCS_READ_TOKEN"] = token
    return subprocess.run([sys.executable, "-I", str(SCRIPT)], env=env, capture_output=True, text=True)


class MissingToken(unittest.TestCase):
    def test_a_missing_token_fails_with_the_one_line(self):
        for label, value in (("unset", None), ("empty", ""), ("blank", "   \n")):
            with self.subTest(label):
                done = run(value)
                self.assertEqual(done.returncode, 1)
                self.assertEqual(done.stdout, LINE + "\n")
                self.assertEqual(done.stderr, "")

    def test_a_set_token_passes_and_is_never_printed(self):
        done = run("ghp_SYNTHETIC_not_a_real_token")
        self.assertEqual((done.returncode, done.stdout, done.stderr), (0, "", ""))


class Workflow(unittest.TestCase):
    text = WORKFLOW.read_text()

    def test_the_token_is_read_only_as_the_secrets_expression(self):
        reads = re.findall(r"secrets\.DOCS_READ_TOKEN", self.text)
        exact = re.findall(r"\$\{\{ secrets\.DOCS_READ_TOKEN \}\}", self.text)
        self.assertEqual(len(reads), len(exact))
        self.assertEqual(len(exact), 2)  # the missing-token check, and the checkout of docs
        self.assertNotIn("secrets.DOCS_READ_TOKEN", "\n".join(l for l in self.text.splitlines() if l.lstrip().startswith("run:")))
        self.assertNotRegex(self.text, r"secrets\[|toJSON|secrets: inherit")

    def test_the_missing_token_check_runs_before_docs_is_checked_out(self):
        check = self.text.index("require_docs_token.py")
        checkout = self.text.index("repository: Noemai-org/docs")
        self.assertLess(check, checkout)

    def test_the_check_script_is_the_one_in_this_folder(self):
        self.assertIn("python3 -I tools/vendor/require_docs_token.py", self.text)


if __name__ == "__main__":
    unittest.main()
