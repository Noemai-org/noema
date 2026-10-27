#!/usr/bin/env python3
"""Tests for gitleaks_range.py against the real, pinned gitleaks (PLT-02).

Run with GITLEAKS=<path to gitleaks>. Each case builds a throwaway git repository; the fake secret is built at
run time, so no secret-shaped string is committed.
"""
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "gitleaks_range.py"
GITLEAKS = os.environ.get("GITLEAKS", "gitleaks")
TOKEN = "ghp_" + "aB3dE5fG7hI9jK1lM3nO5pQ7rS9tU1vW3xY5"  # synthetic, assembled at run time


def git(repo: Path, *args: str) -> str:
    env = {**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.invalid",
           "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.invalid"}
    return subprocess.run(["git", "-C", str(repo), *args], capture_output=True, text=True, check=True, env=env).stdout.strip()


def commit(repo: Path, name: str, text: str) -> str:
    (repo / name).write_text(text)
    git(repo, "add", name)
    git(repo, "commit", "-q", "-m", f"add {name}")
    return git(repo, "rev-parse", "HEAD")


def scan(repo: Path, base: str, head: str) -> tuple[int, str]:
    r = subprocess.run([sys.executable, "-I", str(SCRIPT), GITLEAKS, base, head], cwd=repo, capture_output=True, text=True)
    return r.returncode, r.stdout + r.stderr


class RangeScan(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name)
        git(self.repo, "init", "-q", "-b", "main")
        self.base = commit(self.repo, "a.txt", "clean\n")

    def test_a_secret_in_the_range_is_found(self):
        head = commit(self.repo, "leak.env", f"GITHUB_TOKEN={TOKEN}\n")
        code, out = scan(self.repo, self.base, head)
        self.assertNotEqual(code, 0, out)
        self.assertIn("leaks found", out)
        self.assertIn(f"gitleaks range: {self.base}..{head}", out)

    def test_a_clean_range_passes_even_when_another_branch_holds_a_secret(self):
        git(self.repo, "checkout", "-q", "-b", "abandoned")
        commit(self.repo, "leak.env", f"GITHUB_TOKEN={TOKEN}\n")
        git(self.repo, "checkout", "-q", "main")
        head = commit(self.repo, "b.txt", "also clean\n")
        code, out = scan(self.repo, self.base, head)
        self.assertEqual(code, 0, out)
        self.assertIn("no leaks found", out)

    def test_the_default_scan_would_have_failed_on_the_other_branch(self):
        # the behaviour this script exists to avoid: gitleaks's own default covers every ref
        git(self.repo, "checkout", "-q", "-b", "abandoned")
        commit(self.repo, "leak.env", f"GITHUB_TOKEN={TOKEN}\n")
        git(self.repo, "checkout", "-q", "main")
        r = subprocess.run([GITLEAKS, "git", ".", "--no-banner", "--redact"], cwd=self.repo, capture_output=True, text=True)
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)

    def test_a_secret_below_the_base_is_not_this_changes_business(self):
        old = commit(self.repo, "leak.env", f"GITHUB_TOKEN={TOKEN}\n")
        head = commit(self.repo, "b.txt", "clean\n")
        code, out = scan(self.repo, old, head)
        self.assertEqual(code, 0, out)

    def test_no_usable_base_scans_the_head_ancestry_and_nothing_else(self):
        git(self.repo, "checkout", "-q", "-b", "abandoned")
        commit(self.repo, "leak.env", f"GITHUB_TOKEN={TOKEN}\n")
        git(self.repo, "checkout", "-q", "main")
        head = commit(self.repo, "b.txt", "clean\n")
        for base in ("", "0" * 40):
            code, out = scan(self.repo, base, head)
            self.assertEqual(code, 0, f"base {base!r}: {out}")
            self.assertIn(f"gitleaks range: {head}", out)
        head_leak = commit(self.repo, "c.env", f"GITHUB_TOKEN={TOKEN}\n")
        code, out = scan(self.repo, "0" * 40, head_leak)
        self.assertNotEqual(code, 0, out)

    def test_no_head_is_refused(self):
        code, _ = scan(self.repo, self.base, "")
        self.assertNotEqual(code, 0)


if __name__ == "__main__":
    unittest.main()
