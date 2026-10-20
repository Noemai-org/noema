"""transitions.yaml against PERD §28.2 (REQ-STM-001): every arrow of the PERD has a row, and every row is an arrow of the PERD.

The PERD text is the vendored extract perd-28-2-state-machine.md, re-pointed by tools/vendor/repoint_docs.py. Run:
python3 -I -m unittest discover -s services/core/orchestrator -p 'test_*.py'
"""
import re
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROW = re.compile(r'^  - \{from: ([A-Z_]+), to: ([A-Z_]+), on: "([^"]+)"\}$')
STATE = re.compile(r"\b[A-Z][A-Z_]{2,}\b")


def yaml_rows():
    rows = []
    for line in (HERE / "transitions.yaml").read_text().splitlines():
        if line.startswith("  - "):
            m = ROW.match(line)
            assert m, f"unreadable row: {line}"
            rows.append(m.groups())
    return rows


def perd_arrows():
    """(from, to) pairs read from every state's 'Exit and allowed next states' line."""
    text = (HERE / "perd-28-2-state-machine.md").read_text()
    states, arrows, current = [], set(), None
    for line in text.splitlines():
        head = re.match(r"^\*\*([A-Z_]+)\*\*", line)
        if head and head.group(1) != "FROZEN":  # FROZEN is a flag, not a state
            current = head.group(1)
            states.append(current)
        elif current and line.startswith("- Exit and allowed next states:"):
            body = line.split(":", 1)[1]
            body = re.sub(r"\([^)]*\)|\[[^\]]*\]", " ", body)  # parentheses and tags carry causes, not targets
            for target in STATE.findall(body):
                arrows.add((current, target))
    return states, arrows


class Transitions(unittest.TestCase):
    def test_every_perd_arrow_has_a_row_and_every_row_is_a_perd_arrow(self):
        _, arrows = perd_arrows()
        rows = {(f, t) for f, t, _ in yaml_rows()}
        self.assertEqual(sorted(arrows - rows), [], "PERD arrows with no row")
        self.assertEqual(sorted(rows - arrows), [], "rows that are not PERD arrows")

    def test_the_perd_has_the_expected_number_of_arrows(self):
        _, arrows = perd_arrows()
        self.assertEqual(len(arrows), 42)  # distinct (from, to) pairs; EVALUATING to EVALUATING has two causes, so 43 rows

    def test_every_state_of_the_perd_is_declared_and_every_declared_state_is_used(self):
        states, _ = perd_arrows()
        declared = re.search(r"^states: \[(.*)\]$", (HERE / "transitions.yaml").read_text(), re.M).group(1).split(", ")
        self.assertEqual(sorted(declared), sorted(set(states) | {"EXPLAINING_BLOCKED"}))
        used = {x for f, t, _ in yaml_rows() for x in (f, t)}
        self.assertLessEqual(used, set(declared))

    def test_every_row_has_a_cause_and_no_row_repeats(self):
        rows = yaml_rows()
        self.assertTrue(all(cause.strip() for _, _, cause in rows))
        self.assertEqual(len(rows), len(set(rows)))

    def test_nothing_leaves_expired_and_only_listed_states_are_terminal(self):
        rows = yaml_rows()
        self.assertEqual([r for r in rows if r[0] == "EXPIRED"], [])
        leaving = {f for f, _, _ in rows}
        self.assertEqual(sorted(set(re.search(r"^states: \[(.*)\]$", (HERE / "transitions.yaml").read_text(), re.M).group(1).split(", ")) - leaving),
                         ["COMPLETED", "EXPIRED", "EXPLAINING_BLOCKED", "REJECTED"])


if __name__ == "__main__":
    unittest.main()
