"""Tests for tools/vendor/repoint_docs.py: the current PERD is resolved from the docs README, not hardcoded (GOV-22).

Run: python3 -I -m unittest discover -s tools/vendor -p 'test_*.py'
Each test builds a throwaway docs checkout in a temp directory; nothing touches the network or this repository.
"""
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import repoint_docs as rd  # noqa: E402

TABLE = """| File | What it is |
|---|---|
{rows}
| [`requirements-index.csv`](requirements-index.csv) | Generated index. |
"""
EVENTS = """### 32.2 Event types [ADOPTED]

| Group | Event types |
|---|---|
| Trust | DECISION_MADE · GRANT_ISSUED |
| Security | SECURITY_EVENT{{kind: CHAIN_BREAK}} |

Text after the table.
"""
STATES = """### 28.2 Investigation state machine [ADOPTED]

**RECEIVED**

- Exit and allowed next states: → INTERPRETING.

### 28.3 State requirements
"""


def row(version: str, current: bool) -> str:
    name = f"NOEMA_POC_PERD_v{version}.md"
    label = "**Current.** The PERD" if current else "The PERD"
    return f"| [`{name}`]({name}) | {label}, v{version}. |"


class DocsCheckout(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.docs = Path(self._tmp.name)
        (self.docs / "docs/poc/perd").mkdir(parents=True)
        (self.docs / "docs/engineering").mkdir(parents=True)
        for name, text in (("AGENTS.md", "agents\n"), ("CLAUDE.md", "claude\n"), ("docs/engineering/state-registry.md", "registry\n")):
            (self.docs / name).write_text(text, encoding="utf-8")
        subprocess.check_call(["git", "init", "-q", str(self.docs)])

    def perd(self, version: str, events: str = EVENTS):
        (self.docs / f"docs/poc/perd/NOEMA_POC_PERD_v{version}.md").write_text(f"# PERD v{version}\n\n{events}\n{STATES}", encoding="utf-8")

    def readme(self, *rows: str):
        (self.docs / "docs/poc/perd/README.md").write_text(TABLE.format(rows="\n".join(rows)), encoding="utf-8")


class ResolvePerd(DocsCheckout):
    def test_resolves_the_row_marked_current(self):
        self.perd("1.9"); self.perd("1.10")
        self.readme(row("1.10", True), row("1.9", False))
        self.assertEqual(rd.resolve_perd(self.docs), "docs/poc/perd/NOEMA_POC_PERD_v1.10.md")

    def test_follows_a_version_change_without_any_code_change(self):
        """The same code, two READMEs: a bump moves the answer. 1.10 > 1.9 and 1.11 > 1.10 do not depend on sorting."""
        for version in ("1.9", "1.10", "1.11", "2.0"):
            self.perd(version)
        for current in ("1.9", "1.10", "1.11", "2.0"):
            rows = [row(v, v == current) for v in ("2.0", "1.11", "1.10", "1.9")]
            self.readme(*rows)
            self.assertEqual(rd.resolve_perd(self.docs), f"docs/poc/perd/NOEMA_POC_PERD_v{current}.md", current)

    def test_the_row_order_does_not_matter(self):
        self.perd("1.9"); self.perd("1.10")
        self.readme(row("1.9", False), row("1.10", True))
        self.assertEqual(rd.resolve_perd(self.docs), "docs/poc/perd/NOEMA_POC_PERD_v1.10.md")

    def test_no_current_row_stops(self):
        self.perd("1.9"); self.readme(row("1.9", False))
        with self.assertRaises(SystemExit) as raised:
            rd.resolve_perd(self.docs)
        self.assertIn("exactly one", str(raised.exception))

    def test_two_current_rows_stop(self):
        self.perd("1.9"); self.perd("1.10"); self.readme(row("1.10", True), row("1.9", True))
        with self.assertRaises(SystemExit):
            rd.resolve_perd(self.docs)

    def test_a_current_row_whose_file_is_missing_stops(self):
        self.readme(row("1.10", True))
        with self.assertRaises(SystemExit) as raised:
            rd.resolve_perd(self.docs)
        self.assertIn("does not exist", str(raised.exception))

    def test_the_script_names_no_perd_version(self):
        source = Path(rd.__file__).read_text(encoding="utf-8")
        self.assertNotRegex(source, r"NOEMA_POC_PERD_v\d", "a hardcoded PERD file name would need a code change per version")


class ExtractFollowsTheResolvedFile(DocsCheckout):
    def test_the_extract_comes_from_the_current_file_and_names_it(self):
        self.perd("1.9", events=EVENTS.replace("GRANT_ISSUED", "OLD_ONLY"))
        self.perd("1.10")
        self.readme(row("1.10", True), row("1.9", False))
        out = rd.expected(self.docs, "c" * 40)["contracts/perd-32-2-event-types.md"]
        self.assertIn("NOEMA_POC_PERD_v1.10.md section 32.2", out)
        self.assertIn("GRANT_ISSUED", out)
        self.assertNotIn("OLD_ONLY", out)
        self.assertNotIn("Text after the table", out)

    def test_after_a_version_bump_the_extract_follows(self):
        self.perd("1.10"); self.perd("1.11", events=EVENTS.replace("GRANT_ISSUED", "ADDED_IN_1_11"))
        self.readme(row("1.10", True), row("1.11", False))
        self.assertIn("GRANT_ISSUED", rd.expected(self.docs, "d" * 40)["contracts/perd-32-2-event-types.md"])
        self.readme(row("1.11", True), row("1.10", False))
        after = rd.expected(self.docs, "d" * 40)["contracts/perd-32-2-event-types.md"]
        self.assertIn("NOEMA_POC_PERD_v1.11.md section 32.2", after)
        self.assertIn("ADDED_IN_1_11", after)

    def test_the_whole_file_copies_name_the_commit(self):
        self.perd("1.10"); self.readme(row("1.10", True))
        out = rd.expected(self.docs, "e" * 40)
        self.assertEqual(sorted(out), ["AGENTS.md", "CLAUDE.md", "contracts/perd-32-2-event-types.md", "contracts/state-registry.md",
                                    "services/core/orchestrator/perd-28-2-state-machine.md"])
        for target in ("AGENTS.md", "CLAUDE.md", "contracts/state-registry.md"):
            self.assertIn("docs commit " + "e" * 40, out[target].splitlines()[0])


class StateExtract(DocsCheckout):
    def test_section_28_2_is_copied_verbatim_and_stops_at_the_next_heading(self):
        self.perd("1.1")
        self.readme(row("1.1", True))
        out = rd.expected(self.docs, "e" * 40)["services/core/orchestrator/perd-28-2-state-machine.md"]
        self.assertIn("### 28.2 Investigation state machine [ADOPTED]", out)
        self.assertIn("→ INTERPRETING", out)
        self.assertNotIn("28.3 State requirements", out)
        self.assertIn("docs commit " + "e" * 40, out)


if __name__ == "__main__":
    unittest.main()
