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

    def test_the_intron_key_and_endpoint_slot_are_named_and_empty(self):
        # A179: Intron Sahara v2.5 is the speech provider. The vendor register left its key name open; the name is INTRON_API_KEY (the pattern of AETHEX_API_KEY). The provider's
        # endpoint is a setting the deployment gives, so it has a NAME here and no value: no key and no hostname in the repository.
        values = {}
        for line in (ROOT / ".env.example").read_text().splitlines():
            m = c.LINE.match(line.strip())
            if m:
                values[m.group(1)] = line.split("=", 1)[1].strip()
        for name in ("INTRON_API_KEY", "INTRON_API_BASE_URL"):
            self.assertIn(name, values, f"{name} is not named in .env.example")
            self.assertEqual(values[name], "", f"{name} must have no value in the repository")

    def test_no_file_of_the_repository_names_an_intron_endpoint(self):
        import os
        import re

        endpoint = re.compile(r"(?i)https?://[^\s\"'<>)]*intron|\bintron[a-z0-9-]*\.(?:com|ai|io|net|org|co|africa|ng)\b")
        skip = {".git", "node_modules", "__pycache__", ".venv"}
        found = []
        for base, dirs, files in os.walk(ROOT):
            dirs[:] = [d for d in dirs if d not in skip]
            for f in files:
                path = Path(base) / f
                if path.suffix in {".png", ".jpg", ".gif", ".pdf", ".ico", ".woff", ".woff2", ".gz", ".zip", ".pyc", ".exe"} or path.stat().st_size > 2_000_000:
                    continue
                try:
                    text = path.read_text(encoding="utf-8")
                except (UnicodeDecodeError, OSError):
                    continue
                if path.name == "test_check_secret_refs.py":
                    continue
                for number, line in enumerate(text.splitlines(), 1):
                    if endpoint.search(line):
                        found.append(f"{path.relative_to(ROOT)}:{number}")
        self.assertEqual(found, [], "a hostname or URL of the speech provider is in the repository: it belongs in the deployment's settings")

    def test_the_real_repository_passes(self):
        self.assertEqual(c.violations(ROOT / ".github/workflows", ROOT / ".env.example"), [])


if __name__ == "__main__":
    unittest.main()
