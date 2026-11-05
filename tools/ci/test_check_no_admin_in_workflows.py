"""Tests for check_no_admin_in_workflows.py: what it rejects, what it accepts, and that the real repository passes."""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_no_admin_in_workflows as c  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent


def run(files: dict[str, str]) -> list[str]:
    with tempfile.TemporaryDirectory() as tmp:
        d = Path(tmp)
        for name, text in files.items():
            (d / name).parent.mkdir(parents=True, exist_ok=True)
            (d / name).write_text(text)
        return c.violations(d)


class Cases(unittest.TestCase):
    def test_the_secret_is_rejected(self):
        self.assertEqual(len(run({"workflows/a.yml": "x: ${{ secrets.TUSHE_ADMIN_TOKEN }}\n"})), 1)

    def test_the_environment_is_rejected(self):
        self.assertEqual(len(run({"workflows/a.yml": "jobs:\n  j:\n    environment: tushe-admin\n"})), 1)

    def test_any_admin_prefixed_name_is_rejected(self):
        self.assertEqual(len(run({"workflows/a.yml": "x: ${{ secrets['TUSHE_ADMIN_KEY'] }}\n"})), 1)

    def test_case_and_separator_variants_are_rejected(self):
        for spelling in ("Tushe-Admin", "tushe_admin", "TUSHE ADMIN", "tushe--admin", "tusheadmin"):
            self.assertEqual(len(run({"workflows/a.yml": f"environment: {spelling}\n"})), 1, spelling)

    def test_a_comment_is_rejected(self):
        self.assertEqual(len(run({"workflows/a.yml": "# later: use TUSHE_ADMIN_TOKEN here\n"})), 1)

    def test_a_composite_action_is_checked(self):
        self.assertEqual(len(run({"actions/x/action.yaml": "env:\n  T: ${{ secrets.TUSHE_ADMIN_TOKEN }}\n"})), 1)

    def test_the_fixture_and_oracle_credentials_are_not_this_check(self):
        self.assertEqual(run({"workflows/a.yml": "x: ${{ secrets.TUSHE_FIXTURE_TOKEN }}\n"}), [])

    def test_a_file_that_cannot_be_decoded_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "a.yml").write_bytes(b"\xff\xfe\x00bad")
            self.assertEqual(len(c.violations(Path(tmp))), 1)

    def test_the_real_repository_passes(self):
        self.assertEqual(c.violations(ROOT / ".github"), [])


if __name__ == "__main__":
    unittest.main()
