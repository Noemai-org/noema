"""repoint_docs.py rewrites the body of each vendored copy from the docs file at the target commit, not only the header (GOV-22).

Each test builds a throwaway docs checkout and points the script's ROOT at another temp directory; nothing touches the network or this
repository. Run: python3 -I -m unittest discover -s tools/vendor -p 'test_*.py'
"""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import repoint_docs as rd  # noqa: E402
from test_drift_guard import PERD, PERD_README  # noqa: E402

COPIES = ["AGENTS.md", "CLAUDE.md", "contracts/state-registry.md", rd.EXTRACT_TARGET, rd.STATE_TARGET]


def body(text: str) -> str:
    """A copy without its first line, which is the header comment naming the docs commit."""
    return text.split("\n", 1)[1]


class Bodies(unittest.TestCase):
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
        self.assertEqual(self.run_main(), 0)

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

    def copy(self, name: str) -> str:
        return (rd.ROOT / name).read_text(encoding="utf-8")

    def edit_perd(self, old: str, new: str):
        path = self.docs / "docs/poc/perd/NOEMA_POC_PERD_v1.1.md"
        text = path.read_text(encoding="utf-8")
        self.assertIn(old, text)
        path.write_text(text.replace(old, new), encoding="utf-8")

    def test_a_changed_section_is_reflected_in_the_copy(self):
        """Every kind of source changes, one commit: each copy's body follows its source, and the result is not drift."""
        (self.docs / "AGENTS.md").write_text("agents, rewritten\n", encoding="utf-8")
        (self.docs / "docs/engineering/state-registry.md").write_text("registry, rewritten\n", encoding="utf-8")
        self.edit_perd("GRANT_ISSUED", "GRANT_MINTED")
        self.edit_perd("-> INTERPRETING.", "-> INTERPRETING or EXPIRED.")
        self.commit("change")
        self.assertEqual(self.run_main("--check", "--ignore-commit"), 1)  # drift is seen
        self.assertEqual(self.run_main(), 0)                                # and repaired
        self.assertIn("agents, rewritten", self.copy("AGENTS.md"))
        self.assertIn("registry, rewritten", self.copy("contracts/state-registry.md"))
        self.assertIn("GRANT_MINTED", self.copy(rd.EXTRACT_TARGET))
        self.assertNotIn("GRANT_ISSUED", self.copy(rd.EXTRACT_TARGET))
        self.assertIn("-> INTERPRETING or EXPIRED.", self.copy(rd.STATE_TARGET))
        self.assertEqual(self.run_main("--check"), 0)                       # the copies now match exactly

    def test_an_unchanged_section_is_left_alone(self):
        """Only the event table changes. The body of every other copy is byte-for-byte what it was."""
        before = {name: self.copy(name) for name in COPIES}
        self.edit_perd("GRANT_ISSUED", "GRANT_MINTED")
        self.commit("change one section")
        self.assertEqual(self.run_main(), 0)
        for name in COPIES:
            if name == rd.EXTRACT_TARGET:
                self.assertNotEqual(body(self.copy(name)), body(before[name]), name)
            else:
                self.assertEqual(body(self.copy(name)), body(before[name]), f"{name}: its body must not change")

    def test_a_run_with_nothing_to_change_writes_nothing(self):
        stamps = {name: (rd.ROOT / name).stat().st_mtime_ns for name in COPIES}
        self.assertEqual(self.run_main(), 0)
        for name in COPIES:
            self.assertEqual((rd.ROOT / name).stat().st_mtime_ns, stamps[name], f"{name} was rewritten with nothing to change")


if __name__ == "__main__":
    unittest.main()
