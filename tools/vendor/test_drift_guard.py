"""The drift guard's mode of repoint_docs.py: --check --ignore-commit (GOV-22).

Content drift is reported; a moved docs commit alone is not. Each test builds a throwaway docs checkout in a temp directory
and points the script's ROOT at another temp directory; nothing touches the network or this repository.
Run: python3 -I -m unittest discover -s tools/vendor -p 'test_*.py'
"""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import repoint_docs as rd  # noqa: E402

PERD_README = """| File | What it is |
|---|---|
| [`NOEMA_POC_PERD_v1.1.md`](NOEMA_POC_PERD_v1.1.md) | **Current.** The PERD, v1.1. |
"""
PERD = """# PERD v1.1

### 28.2 Investigation state machine [ADOPTED]

**RECEIVED**

- Exit and allowed next states: -> INTERPRETING.

### 28.3 State requirements

### 32.2 Event types [ADOPTED]

| Group | Event types |
|---|---|
| Trust | DECISION_MADE · GRANT_ISSUED |

Text after the table.
"""


class DriftCheck(unittest.TestCase):
    def setUp(self):
        self.docs_tmp, self.root_tmp = tempfile.TemporaryDirectory(), tempfile.TemporaryDirectory()
        self.addCleanup(self.docs_tmp.cleanup)
        self.addCleanup(self.root_tmp.cleanup)
        self.docs = Path(self.docs_tmp.name)
        (self.docs / "docs/poc/perd").mkdir(parents=True)
        (self.docs / "docs/engineering").mkdir(parents=True)
        for name, text in (("AGENTS.md", "agents\n"), ("CLAUDE.md", "claude\n"), ("docs/engineering/state-registry.md", "registry\n"),
                           ("docs/poc/perd/README.md", PERD_README), ("docs/poc/perd/NOEMA_POC_PERD_v1.1.md", PERD)):
            (self.docs / name).write_text(text, encoding="utf-8")
        subprocess.check_call(["git", "init", "-q", str(self.docs)])
        self.before = rd.ROOT
        rd.ROOT = Path(self.root_tmp.name)
        self.addCleanup(setattr, rd, "ROOT", self.before)
        self.commit("first")
        self.assertEqual(self.run_main(), 0)  # vendor the copies at the first commit

    def commit(self, message: str):
        subprocess.check_call(["git", "-C", str(self.docs), "add", "-A"])
        subprocess.check_call(["git", "-C", str(self.docs), "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", message])

    def run_main(self, *flags: str) -> int:
        old = sys.argv
        sys.argv = ["repoint_docs.py", "--docs", str(self.docs), *flags]
        try:
            return rd.main()
        finally:
            sys.argv = old

    def test_a_new_docs_commit_with_the_same_content_is_not_drift(self):
        (self.docs / "unrelated.txt").write_text("x\n", encoding="utf-8")
        self.commit("unrelated")
        self.assertEqual(self.run_main("--check"), 1)  # the header names another commit
        self.assertEqual(self.run_main("--check", "--ignore-commit"), 0)

    def test_a_changed_original_is_drift(self):
        (self.docs / "AGENTS.md").write_text("agents changed\n", encoding="utf-8")
        self.commit("change")
        self.assertEqual(self.run_main("--check", "--ignore-commit"), 1)

    def test_a_changed_event_table_is_drift(self):
        path = self.docs / "docs/poc/perd/NOEMA_POC_PERD_v1.1.md"
        path.write_text(path.read_text(encoding="utf-8").replace("GRANT_ISSUED", "GRANT_MINTED"), encoding="utf-8")
        self.commit("change")
        self.assertEqual(self.run_main("--check", "--ignore-commit"), 1)

    def test_a_missing_copy_is_drift(self):
        (rd.ROOT / "AGENTS.md").unlink()
        self.assertEqual(self.run_main("--check", "--ignore-commit"), 1)

    def test_ignore_commit_without_check_is_refused(self):
        self.assertEqual(self.run_main("--ignore-commit"), 2)


if __name__ == "__main__":
    unittest.main()
