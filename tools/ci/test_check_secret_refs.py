"""Tests for check_secret_refs.py: what it accepts, what it rejects, and that the real repository passes."""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_secret_refs as c  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent


def run(workflow: str, env: str) -> list[str]:
    with tempfile.TemporaryDirectory() as tmp:
        d = Path(tmp)
        (d / "wf").mkdir()
        (d / "wf" / "a.yml").write_text(workflow)
        (d / "env").write_text(env)
        return c.violations(d / "wf", d / "env")


class Cases(unittest.TestCase):
    def test_listed_secret_passes(self):
        self.assertEqual(run("x: ${{ secrets.OPENAI_API_KEY }}\n", "OPENAI_API_KEY=\n"), [])

    def test_unlisted_secret_fails(self):
        self.assertEqual(len(run("x: ${{ secrets.OPENAI_API_KEY }}\n", "ANTHROPIC_API_KEY=\n")), 1)

    def test_github_token_is_built_in(self):
        self.assertEqual(run("x: ${{ secrets.GITHUB_TOKEN }}\n", ""), [])

    def test_prefix_line_covers_the_family(self):
        self.assertEqual(run("x: ${{ secrets.TUSHE_ORACLE_CLIENT_ID }}\n", "TUSHE_ORACLE_*=\n"), [])
        self.assertEqual(len(run("x: ${{ secrets.TUSHE_FIXTURE_ID }}\n", "TUSHE_ORACLE_*=\n")), 1)

    def test_bracket_form_is_checked(self):
        self.assertEqual(len(run("x: ${{ secrets['NOPE'] }}\n", "")), 1)
        self.assertEqual(run("x: ${{ secrets['OK_KEY'] }}\n", "OK_KEY=\n"), [])

    def test_dynamic_lookup_fails(self):
        self.assertEqual(len(run("x: ${{ secrets[matrix.name] }}\n", "ANY=\n")), 1)

    def test_comments_are_ignored(self):
        self.assertEqual(run("# ${{ secrets.NOPE }}\nx: 1  # secrets.NOPE\n", ""), [])

    def test_a_name_that_only_starts_like_a_listed_one_fails(self):
        self.assertEqual(len(run("x: ${{ secrets.OPENAI_API_KEY_2 }}\n", "OPENAI_API_KEY=\n")), 1)

    def test_a_value_in_the_example_does_not_count_as_a_name_line_with_a_space(self):
        self.assertEqual(len(run("x: ${{ secrets.K }}\n", "# K=\n")), 1)

    def test_the_real_repository_passes(self):
        self.assertEqual(c.violations(ROOT / ".github/workflows", ROOT / ".env.example"), [])


if __name__ == "__main__":
    unittest.main()
